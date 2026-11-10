# ifiles — FileBrowser Quantum CLI

## What this talks to

**FileBrowser Quantum** (`gtsteffaniak/filebrowser`), not the original
`filebrowser/filebrowser`. The fork changed the API enough that no existing client
library works, and almost every example online is for the original.

**Read the server's Go source, not the docs site.** The published documentation is
wrong about the upload mechanism and about at least one route path. The router is
`backend/http/httpRouter.go`; wire shapes are the structs in
`backend/indexing/iteminfo/fileinfo.go` and `backend/indexing/indexingFiles.go`.
`backend/swagger/docs/swagger.json` covers only annotated handlers, so a route
missing from it may still exist — `GET /settings/sources` does.

**Read the source at the version the server actually runs — the image tag its
deployment pins.** Not what `gh release list` calls latest: upstream's lineage is not
chronological, so `v1.5.2-beta` is *older* by date than `v1.5.0-stable`, and `main` is
a third different API again.

A pin records what *should* be deployed, and under `restart: unless-stopped` a tag moves
only on a *pull*. Fingerprint the live router whenever the answer matters.

It answers unauthenticated, which is what makes that checkable. A registered route
replies `401` with a JSON body; an unregistered one replies Go's plain-text
`404 page not found`, which is how to tell "needs a token" from "does not exist":

```bash
curl -s -o /dev/null -w '%{http_code}\n' https://files.example.com/api/settings/sources
```

Version-fingerprint it with a route that moved. `GET /settings/sources` and
`GET /tools/search` arrived in `v1.3.0-stable`; before that they were `GET /search`
with no sources route, and `DELETE /resources/bulk` replaced
`POST /resources/bulk/delete` in `v1.4.0-stable`. Which of those answer 401 pins the
deployed version to a range without shell access to the container.

## Every external effect has one door

Anything that leaves the process goes through one named function. That function is where
the token, a header or a test stub attaches, once.

- **HTTP.** `filebrowser.Client.newRequest` builds every request, and it is the only place
  the token is set. `Client.do` issues every JSON call: it encodes the body, reads the
  response, and turns a 4xx or 5xx into an `APIError`. Three methods call `c.http.Do`
  themselves, each on a request `newRequest` built:
  - `putChunk` sends a raw byte body with the chunk headers, and runs on a detached
    context so a cancellation can pause before it aborts.
  - `Download` hands the open response body to the caller to stream, which `do` would
    read whole.
  - `Health` reads only the status.

  No other code calls `c.http.Do`, and nothing outside `filebrowser` makes a request.
- **The OS keyring** is reached only through `auth.osKeyring`, behind the
  `keyringBackend` interface `auth.TokenStore` holds.
- **Files ifiles owns** are each written by one type: the config by `config.Config.Save`,
  the fallback token file by `auth.fileStore.write`, the resume offsets by
  `resume.Store.write`, and the completion listings by `completion.Cache.write`. The last
  three write a temp file and rename it into place, so a crash never leaves half a file.
- **The user's files** are read by `upload` and written by `download`, which streams into
  `<target>.ifilespart` and renames it when the transfer completes.

ifiles' own code runs no subprocess. The keyring library does: `/usr/bin/security` on
macOS, and `dbus-launch` on Linux when no session bus is running.

## Protocol details that will not be guessed correctly

- **Upload is the raw request body**, not multipart. The handler does
  `io.Copy(outFile, r.Body)` and takes the destination from the query string.
- **Chunking is header-driven**: `X-File-Chunk-Offset` and `X-File-Total-Size`. The
  *presence* of the offset header is what selects the chunked code path.
- **Pause before abort, or the partial upload is deleted.** On a chunk write failure
  the server keeps the temp file only if `POST /resources/pause` was registered
  first; otherwise it removes it. `putChunk` therefore runs the request on a
  detached context so cancellation can be sequenced — pause on a *fresh* context,
  then abort. Handing the user's canceled context to the pause request cancels the
  pause too, which is the exact bug it exists to prevent.
- **A graceful pause answers 499**, not a 4xx with a body.
- **No endpoint reports how much of a partial upload arrived.** The temp file sits
  beside the destination (`<realPath>.<md5>.uploading.tmp`) and nothing exposes it;
  a stat of the destination 404s until the final chunk lands. That is why the
  `resume` package exists. The server also **seeks to whatever offset it is given
  without verifying the temp file is that long**, so a wrong offset yields a
  silently corrupt file — hence the size-and-mtime guard in `resume.Store.Lookup`.
- **Chunk bodies are buffered before sending, deliberately.** A reader that ends
  early breaks the connection mid-body, the server reads that as a failed chunk, and
  with no pause registered it deletes the whole partial upload. Streaming straight
  from the file is cheaper but can destroy progress, so `Upload` reads each chunk
  with `io.ReadFull` first and reports `ErrSourceTooShort`.
- **`GET /resources/download` takes repeated `?file=`**, not `?path=`. It is the one
  route that breaks the convention. It also builds and streams an archive itself for
  a directory or several paths, so `POST /resources/archive` is not needed.
- **Every resource route requires `source`.** Quantum is multi-source, and a server
  may define several.
- **`PATCH /resources` reports failures inside a 200.** The body carries
  `succeeded`/`failed` arrays, so checking the status alone reports success on a
  move that did not happen.
- **The token goes in the `Authorization` header only.** The server also accepts
  `?auth=<token>`, which would put the secret in a reverse proxy's access log.
- **A 403 on the share routes means three unrelated things**, and the status
  alone cannot say which. A missing `share` permission comes from
  `withPermShareHelper`, which returns the status with a *nil error* — so
  `wrapHandler` writes no body at all. Every other 403 there (path not found,
  private source) comes from the handler, which returns an error and therefore a
  message. An empty message on a 403 is that exact discriminator, not a
  heuristic, and `IsMissingPermission` is the only thing standing between the
  user and a bare `403 Forbidden` with nothing to act on.
- **`POST /share` takes `expires` as a string number beside a separate `unit`**,
  and the switch falls through to hours for anything it does not recognize. A
  misspelled unit therefore does not error — it silently multiplies the lifetime
  by 24. `CreateShare` always sends seconds so there is one spelling to get
  right, and clamps to 1 so a sub-second duration cannot round to 0, which the
  server would accept as a link that expired before it existed.
- **`DELETE /share` answers 400 for a hash it cannot find**, not 404: the lookup
  fails before the handler decides anything is missing. `IsNotFound` never fires.
- **A share's path comes back scoped and slash-suffixed.** The server joins the
  account's scope on and calls `AddTrailingSlashIfNotExists` — even for a file —
  so a share created for `/report.pdf` lists as `/report.pdf/`. Round-tripping
  that path into a resource call is the mistake it invites.

## Token lifetime is read locally, and there is no `tokens` command

`GET /api/auth/token/list` returns **every token's full JWT**, and its permission
claim is never enforced: each check in the router reads `d.user.Permissions` off
the user record, so a token minted with a narrow set still acts with the whole
account's rights. Neither fact is worth a CLI surface — minting and revoking are
annual, and the first token has to come from the web UI regardless.

What does recur is a token expiring silently. `auth.ExpiresAt` decodes the stored
JWT's `exp` claim with no network call and `auth status` prints it, warning under
a fortnight. The signature is deliberately not verified: proving the server minted
it is the server's job on every request, and a client rejecting its own token would
only hide the date. Note `base64.RawURLEncoding` — a JWT drops the padding, so
`URLEncoding` fails on most real tokens and succeeds on the occasional one whose
payload length happens to be a multiple of three.

## Shell completion

Remote paths complete over the API, so `ifiles __complete download /pho` is a real request made
by a fresh process on every Tab. Three things about that are not guessable:

- **A candidate must keep the directory prefix exactly as typed.** Bash filters the
  returned list through `compgen -W ... -- "$cur"` and zsh through `_describe`, so
  normalizing a typed `photos/` to `/photos/x` matches nothing and reads as broken
  completion rather than as a bug. `completionCandidates` splits with `path.Split` and
  re-attaches the literal half; `TestCompletionKeepsTheDirectoryExactlyAsTyped` pins it.
- **Stdout is the candidate list.** A diagnostic printed there is offered as a suggestion
  and a returned error lands in the user's command line, so every failure — no token,
  server unreachable, expired token — returns `ShellCompDirectiveError` with no output. Nothing on
  the completion path may write to stdout.
- **Flag completions register in the file that defines the flag**, not in `complete.go`.
  Init functions run in filename order, so `complete.go` runs before `download.go`, `root.go`,
  `shares_create.go` and `upload.go` have created the flags to attach to. `mustCompleteFlag` panics rather than
  discarding cobra's error, so a renamed flag fails on the first run instead of silently
  completing nothing. Assigning `ValidArgsFunction` centrally *is* safe — command variables
  are constructed before any init runs.

Listings are cached for `completion.DefaultTTL` under `XDG_CACHE_HOME`. Cache, not state:
losing it costs one request, which is what distinguishes it from the resume offsets.

## Authentication constraint

A server can be OIDC-only (`auth.methods.password.enabled: false`), and then there is
no password grant to script against. The identity provider's own token is not what the
API accepts either — the JWT it wants is minted by FileBrowser after its own OIDC
callback. ifiles therefore authenticates with an API token alone, which works whatever
login methods a server enables.

So the first API token must be created in the browser UI, and **that requires the
account to hold the `api` permission**, which is independent of `admin`. A new user
gets it only if the server's `userDefaults` grant it. `ApplyUserDefaults` runs only
when a user is auto-created, so changing the config does not grant it retroactively;
on an existing OIDC user, login syncs `Admin` and nothing else.

**There is not always an OS keyring to put the token in.** On Linux `go-keyring` is
the Secret Service over D-Bus, and WSL, containers, and headless servers have no
provider — the call fails with `exec: "dbus-launch": executable file not found in
$PATH` before any request is made, which names nothing a user can act on. So the
store falls back to a 0600 file and `Save` returns the `Backend` it used rather than
swallowing it: a downgrade from keychain to plaintext has to be visible at the moment
it happens. `Load` prefers the keyring whenever it holds a token. Nothing copies a
file token across, so a host that gains a provider later moves onto it at the next
`auth login`, whose `Save` lands in the keyring. The file copy stays behind until
`Delete`, which clears both — otherwise a file token outlives the logout that was
meant to remove it. The keyringless path is reproducible without WSL:

```bash
GOOS=linux go build -o /tmp/ifiles . && docker run --rm -v /tmp:/w ubuntu:24.04 /w/ifiles list /
```

## `go-keyring` is accepted, and it reaches one entry

`github.com/zalando/go-keyring` is the one dependency that reaches past this repo's own
files. It reads and writes the OS credential store: Keychain on macOS, the Secret Service
over D-Bus on Linux, Credential Manager on Windows.

- **What it reaches.** ifiles asks it for one service, `ifiles`, and one account per
  server URL. Each entry holds one FileBrowser API token. ifiles never asks it for
  another service's entries.
- **Why it is accepted.** It is the maintained Go route to all three stores behind one
  API, with no cgo, which is why it runs `/usr/bin/security` on macOS. The alternative is a 0600 plaintext file, which ifiles already falls
  back to where no store answers. Using the store first keeps the token encrypted at
  rest wherever a store exists.
- **Its blast radius.** A defect in it can lose or expose that one token. A lost token
  costs an `auth login` with a token re-minted in the web UI. An exposed one carries
  the account's full rights until it is revoked there, which is the same exposure the
  fallback file has.

Calls reach it only through `auth.osKeyring`, so replacing it is one file.

## The request-body cap the design turns on

A proxy in front of the server can cap request bodies, and a Cloudflare tunnel does:
**the free tier stops at 100 MB**. Quantum's WebDAV has no chunking
([upstream #2404](https://github.com/gtsteffaniak/filebrowser/issues/2404)), so behind
that cap a large file gets through only as chunks over the REST API. Keep
`config.DefaultChunkSize` well under the cap; `TestChunksStaysUnderTheTunnelCap`
guards it.

## Testing

`httptest` servers with response shapes copied from the upstream structs, not
invented. `filebrowser/upload_test.go` reassembles chunks the way the server does —
seek to offset, write, complete at `offset+len >= total` — so it asserts the file
that would land on disk rather than just the requests made. Two cases are worth
keeping whatever else changes: `TestUploadPausesBeforeAbortingOnCancellation` pins
the pause ordering, and `TestUploadResumeProducesAByteIdenticalFile` pins that a
short read does not destroy already-uploaded bytes.

## Never write the breaking-change trailer in a commit message

Those two words anywhere in a message cut a major here, and a major on a Go module
with no `/vN` path is an outage: `go install …@latest` stops seeing the tag, every
installed binary is stranded, and recovery is a reinstall on each machine. The
analyzer matches unanchored and ORs past `.semrelrc`, so nothing switches it off.
A commit that merely *discusses* the trailer cuts one too — say "that marker".

Deliberate majors are `chore(release-major)`, which `.semrelrc` maps to a major. The
release that carries one also adds the `/vN` suffix to the module path, so
`go install <module>/vN@latest` resolves the new tag.

Recovering from an accidental major is not a history rewrite. The offending commit
re-cuts the major on every push until a tag above it takes it out of range. In this
order:

1. Delete the vN GitHub releases and their tags, remote and local.
2. Tag a baseline on `main` above the offending commit, and push it. Find the commit
   by searching `git log <highest-v1-tag>..origin/main --format='%H %s%n%b'` for the
   trailer, singular or plural, anywhere in a line. An anchored search under-reports.
3. Confirm `go list -m github.com/datapointchris/ifiles@latest` answers the v1 tag.
4. Reinstall on every machine. Self-update refuses a lower version, so it cannot
   recover a stranded binary.
