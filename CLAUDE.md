# ifiles — FileBrowser Quantum CLI

Universal rules live in `~/.claude/CLAUDE.md`; fleet standards in `~/dev/standards/`
(`go.md`, `cli-design.md`, `release.md`, `repo-structure.md`, `testing.md`). This file
holds only what is specific to ifiles.

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

**Ask the running server what it has; do not infer it from the tag.** The container
is declared as `gtstef/filebrowser:stable` with `restart: unless-stopped`, so it runs
whatever `stable` meant when the image was last *pulled* — which is not what `stable`
means upstream today, and the gap is measured in stable releases, not days. Reading
the source at whatever `gh release list` calls latest describes a server nobody is
running. Upstream's lineage compounds it: `v1.5.2-beta` is *older* by date than
`v1.5.0-stable`, so `main` is a third different API.

The live router is the only authority, and it answers unauthenticated. A registered
route replies `401` with a JSON body; an unregistered one replies Go's plain-text
`404 page not found`, which is how to tell "needs a token" from "does not exist":

```bash
curl -s -o /dev/null -w '%{http_code}\n' https://files.ichrisbirch.com/api/settings/sources
```

Version-fingerprint it with a route that moved. `GET /settings/sources` and
`GET /tools/search` arrived in `v1.3.0-stable`; before that they were `GET /search`
with no sources route, and `DELETE /resources/bulk` replaced
`POST /resources/bulk/delete` in `v1.4.0-stable`. Which of those answer 401 pins the
deployed version to a range without shell access to the container.

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
  silently corrupt file — hence the size-and-mtime guard in `resume.Lookup`.
- **Chunk bodies are buffered before sending, deliberately.** A reader that ends
  early breaks the connection mid-body, the server reads that as a failed chunk, and
  with no pause registered it deletes the whole partial upload. Streaming straight
  from the file is cheaper but can destroy progress, so `Upload` reads each chunk
  with `io.ReadFull` first and reports `ErrSourceTooShort`.
- **`GET /resources/download` takes repeated `?file=`**, not `?path=`. It is the one
  route that breaks the convention. It also builds and streams an archive itself for
  a directory or several paths, so `POST /resources/archive` is not needed.
- **Every resource route requires `source`.** Quantum is multi-source; this instance
  defines one rooted at `/files`.
- **`PATCH /resources` reports failures inside a 200.** The body carries
  `succeeded`/`failed` arrays, so checking the status alone reports success on a
  move that did not happen.
- **The token goes in the `Authorization` header only.** The server also accepts
  `?auth=<token>`, which would put the secret in Traefik's access log.

## Authentication constraint

The instance is OIDC-only (`auth.methods.password.enabled: false`), so there is no
password grant to script against, and an Authelia token is not what the API accepts —
the JWT it wants is minted by FileBrowser after its own OIDC callback.

So the first API token must be created in the browser UI, and **that requires the
account to hold the `api` permission**, which is independent of `admin` and not
granted by the deployed `userDefaults`. `ApplyUserDefaults` runs only when a user is
auto-created, so changing the config does not grant it retroactively; on an existing
OIDC user, login syncs `Admin` and nothing else.

**There is not always an OS keyring to put the token in.** On Linux `go-keyring` is
the Secret Service over D-Bus, and WSL, containers, and headless servers have no
provider — the call fails with `exec: "dbus-launch": executable file not found in
$PATH` before any request is made, which names nothing a user can act on. So the
store falls back to a 0600 file and `Save` returns the `Backend` it used rather than
swallowing it: a downgrade from keychain to plaintext has to be visible at the moment
it happens. `Load` prefers the keyring whenever it holds a token, so a host that
gains a provider later moves onto it with no re-login, and `Delete` clears both —
otherwise a file token outlives the logout that was meant to remove it. The
keyringless path is reproducible without WSL:

```bash
GOOS=linux go build -o /tmp/ifiles . && docker run --rm -v /tmp:/w ubuntu:24.04 /w/ifiles ls /
```

## Where it runs

Any personal machine, plus the work WSL box, which is listed in the
`wsl-work-workstation` manifest in `~/dotfiles` and installed from the release
tarball by the offline bundler. It is a git-only node — not an SSH or Syncthing peer
— so nothing here can be installed or tested on it from the personal desk.

The server is reachable at `files.ichrisbirch.com` through the
Cloudflare tunnel, which is the constraint the whole design turns on: **the free tier
caps a request body at 100 MB**, and Quantum's WebDAV has no chunking
([#2404](https://github.com/gtsteffaniak/filebrowser/issues/2404)). Keep
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
