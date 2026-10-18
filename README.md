# ifiles

A command-line client for a [FileBrowser Quantum](https://github.com/gtsteffaniak/filebrowser)
server. It moves files in and out of the server from a terminal instead of through the browser UI.

Quantum is the `gtsteffaniak/filebrowser` fork, not the original `filebrowser/filebrowser`. The fork
changed the API enough that no client for the original works against it. ifiles speaks only the
fork's API.

Quantum ships no remote client. The CLI built into its binary edits the server's Bolt database on
local disk, so it needs the server stopped and cannot address a remote instance at all. WebDAV,
which rclone speaks, covers browsing and small files. It has no chunking
([upstream #2404](https://github.com/gtsteffaniak/filebrowser/issues/2404)). Cloudflare's free tier
caps a tunneled request body at 100 MB, so there `rclone copy` of anything big fails with a 413 that
no retry clears. The web UI gets large files through only because it chunks over the REST API.
ifiles does the same from a terminal.

## Install

```bash
go install github.com/datapointchris/ifiles@latest   # or a Linux or macOS archive from the GitHub releases
ifiles update                                         # later: the latest release, verified against its checksums
task install                                          # from a checkout: a dev build into ~/.local/bin
```

## Starting from scratch

```bash
go install github.com/datapointchris/ifiles@latest   # the binary; nothing else to install
# web UI → Settings → API tokens: create one; the account needs the `api` permission
ifiles auth login https://files.example.com          # paste it at a hidden prompt; stored only once the server accepts it
ifiles auth status                                   # reachable, authenticated, and the date the token expires
ifiles sources                                       # what --source accepts; the configured one is marked
ifiles list /                                        # proves a resource call works: the source root
```

The first token has to come from the web UI. That page needs the account to hold the **`api`
permission**, which is independent of `admin`. Without it the server answers 403, because minting
a token is itself an API-permissioned action. An account holds `api` from creation only if the
server's `userDefaults` grant it. Those defaults apply when an account is created, so changing them
later grants nothing to an existing account.

`auth login` refuses a token the server rejects, and says to mint a replacement. It verifies the
token and discovers the source name in one call. Then it stores the token and records the source in
the config. Every resource call needs the source, and nobody should have to type it. A server with
several sources makes `auth login` stop and name them, and `--source` picks one.

**When something does not work, run `ifiles auth status` first.** It checks reachability and
authentication separately, because one 401 cannot say whether the server is down or the token
expired. It reads the token's expiry before any network call, so the date shows even when the
server is down.

## The grammar

Commands are `ifiles VERB PATH`, and the verb says what happens in plain words. The remote path
comes first for a download and second for an upload. So the argument nearest the verb is always the
thing being read: `ifiles download REMOTE [LOCAL]`, `ifiles upload LOCAL [REMOTE]`.

Remote paths are absolute from the source root. The server, the source and the chunk size come from
config, so a command never has to name them. `--source` and `--chunk-size` override one run. Every
listing takes `--json`. `--no-input` never prompts, and fails naming the flag that would have
answered. Any command run with none of the arguments it needs prints its help, and `auth` or
`shares` run bare prints its subcommands. A wrong argument count exits 2, which marks the command
line rather than the run as the thing to fix.

## Which surface answers which question

| Ask | Surface | Reads |
| --- | --- | --- |
| What is in a directory | `list` | one directory listing from the server |
| Where is a file | `search` | the index the server already keeps, without walking the tree |
| Get a file or a directory here | `download`, `read` | the file, or an archive the server builds as it streams |
| Send a file or a tree there | `upload` | the local file in chunks, and the local resume record |
| Rearrange files on the server | `move`, `copy`, `mkdir`, `delete` | nothing local: the server does the work |
| Hand a file to someone with no account | `shares` | the server's share links |
| What `--source` accepts | `sources` | the sources this account can reach |
| Is the token still good | `auth status` | the stored token's own expiry, then the server |
| What a Tab offers | `completion` | a short-lived listing cache, then the server |

## `auth` — a token minted in the browser, kept in the keyring, its expiry read locally

```bash
ifiles auth login https://files.example.com   # paste a token minted in the web UI
pass show ifiles | ifiles auth login -         # or read it from stdin, for a script
ifiles auth status                             # is the server up, and is the token still valid
ifiles auth logout                             # forget it here; revoking it is the web UI's job
```

Access is a long-lived API token. That works on an OIDC-only server, where there is no password to
send and no headless login. The token is read from a hidden prompt or from stdin, never from argv,
so it stays out of shell history and `ps`.

The token goes in the OS keyring, keyed by server URL, and never into the config file.
`IFILES_TOKEN` overrides both stores, for a script.

**On a host with no keyring**, the token goes to `$XDG_STATE_HOME/ifiles/token.json` at `0600`
instead. On Linux the keyring is the Secret Service over D-Bus, which WSL, containers and headless
servers do not have. There the keyring call fails before any request is made, with
`exec: "dbus-launch": executable file not found in $PATH`. Refusing to store a token would leave the
tool unusable on exactly the machines whose only other route to the server is a browser. So it
stores one and says where: `auth login` reports the fallback, and `auth status` names the backend a
token came from.

A host that gains a Secret Service later moves onto it at the next `auth login`. From then on the
keyring copy wins over the file. `auth logout` clears both, so a file token cannot outlive the logout
meant to remove it.

`auth status` decodes the token's `exp` claim locally and warns when under two weeks remain. The
signature is not verified, because proving the server minted the token is the server's job on every
request. The server's own `X-Renew-Token` header fires thirty minutes out, which on a token minted
for months is no warning.

## `upload`, `download`, `read` — every upload is chunked, and an interrupted transfer resumes

```bash
ifiles upload video.mkv /media             # chunked, whatever its size
ifiles upload ~/photos /backup             # a whole tree, recursively
ifiles download /photos/raw.cr2            # into the current directory; re-running resumes
ifiles download /photos --archive tar.gz   # a directory, as an archive the server builds
ifiles read /logs/app.log | rg ERROR       # the file alone on stdout
```

**Every upload is chunked**, small ones included. That keeps one code path, and behind Cloudflare's
100 MB request cap an unchunked large upload fails with a 413 anyway. The default chunk size sits
well under the cap, and `ifiles upload --help` prints it.

**Interrupting is safe.** On Ctrl-C the server is told to keep its partial file *before* the
transfer is aborted. That ordering is the whole mechanism. The server consults its pause register
only when a chunk request fails, and with no pause registered it deletes the partial file. The
offset is recorded at `$XDG_STATE_HOME/ifiles/transfers.json`. It has to be kept locally, since no
endpoint reports how much of a partial upload arrived. Re-running the same command resumes.

A local file that changed between attempts invalidates the resume. The server seeks to whatever
offset it is given without checking the partial file is really that long. Resuming a modified file
would write new bytes into the middle of an old upload. The result would be corrupt, and it would
pass every check the server makes.

**Downloads resume too.** Bytes land in a `.ifilespart` file beside the destination, renamed into
place at the end. A re-run asks for the rest with an HTTP range request. A directory download does
not resume: the server generates the archive as it streams, so there is nothing to seek to.

`read` writes the file and nothing else to stdout. Progress and diagnostics go to stderr, which is
what makes it safe on the left of a pipe.

## `list`, `search`, `move`, `copy`, `mkdir`, `delete` — the server does the work

```bash
ifiles list /photos -l                       # with sizes and modification times
ifiles search invoice /documents             # the server's own index, within one subtree
ifiles move /inbox/scan.pdf /docs/receipts   # server-side, so instant at any size
ifiles delete /tmp/scratch --force           # no confirmation, for a script
```

`move` and `copy` run on the server, so the bytes never pass through the client. A destination that
is an existing directory means "into it". `search` queries the index the server already keeps, so
it walks nothing. `mkdir` creates intermediate directories, because the server does.

`delete` names how many entries a directory takes with it, and asks on a terminal. With no terminal
it refuses unless `--force` is given, rather than prompting into a stdin that will never answer.

## `shares` — a link for someone with no account, printed alone on stdout

```bash
ifiles shares create /docs/contract.pdf --expires 7d --password
ifiles shares list
ifiles shares delete https://files.example.com/public/share/T7bQ3xkLm2
```

The URL is the only thing `shares create` writes to stdout, so it pipes straight into a clipboard or
a message. `--expires` takes a window rather than a date: `7d`, `12h`, `90m`. Without it the link
never expires. `--password` reads from a hidden prompt or from stdin, never from argv. A directory
shares the whole subtree, and the server builds the archive when the recipient downloads it.

`shares list` prints the URL rather than the hash. That column is both the thing you came for and the
handle: it pastes back into `shares delete`, which also takes a bare hash or a download URL. An
expired link still appears, because the server keeps it, and is marked `expired`. A link whose file
has since been deleted is marked `gone`, since it still resolves and 404s for whoever holds it.

This needs the **`share` permission**, which is separate from `api` and from `admin`. Without it the
server refuses with a bare 403 carrying no message at all. ifiles supplies the explanation.

## Shell completion — remote paths complete from the server, and a failure offers nothing

```bash
ifiles completion zsh > "${fpath[1]}/_ifiles"   # also bash, fish and powershell; --help gives each OS's path
ifiles download /photos/2026-07-<TAB>           # a long filename is a Tab, not a copy out of the web UI
```

Every remote position completes against the server. `list` and `mkdir` complete only directories,
since a file in either position can only produce a failed command. `shares delete` completes the
account's live share hashes, each described by its path. A directory arrives with a trailing slash
and no trailing space, so a second Tab descends into it. Local positions, `upload`'s first argument
and `download`'s second, fall through to the shell's own file completion. `--source` completes from
the server's source list.

Each Tab is a real request. The shell runs `ifiles __complete download /photos/` as a fresh process,
so nothing can be held in memory between them. Listings are cached at
`$XDG_CACHE_HOME/ifiles/listings.json` for a few seconds. That is long enough that tabbing through
one directory hits the server once. It is short enough that a file uploaded by the previous command
shows up in the next completion.

A completion that fails for any reason offers nothing and says nothing. No token, an unreachable
server and an expired token all look like "no matches". Its stdout *is* the candidate list, and
anything written there arrives as a suggestion.

## Configuration — `auth login` writes it, and the environment overrides it

TOML at `$XDG_CONFIG_HOME/ifiles/config.toml`, or `~/.config/ifiles/config.toml` when that is
unset. macOS reads the same path, not `~/Library`. `auth login` writes `url` and `source`. The
other two keys are optional.

```toml
url = "https://files.example.com"
source = "files"
remote_dir = "/inbox"   # where `upload` sends things when no remote path is given; `/` when unset
chunk_size = "32MiB"    # must stay under the 100 MB request cap
```

`IFILES_URL`, `IFILES_SOURCE` and `IFILES_CHUNK_SIZE` override the file's keys. `IFILES_TOKEN`
overrides both token stores. With those set, a script needs no file at all. `--config` reads a
different file.

## Where it writes

| Path | Holds | Losing it costs |
| --- | --- | --- |
| `$XDG_CONFIG_HOME/ifiles/config.toml` | the server URL and source, at `0600` | an `auth login` |
| the OS keyring, or `$XDG_STATE_HOME/ifiles/token.json` at `0600` | the token, keyed by server URL | an `auth login` |
| `$XDG_STATE_HOME/ifiles/transfers.json` | the offset of each interrupted upload | the whole transfer, sent again |
| `$XDG_CACHE_HOME/ifiles/listings.json` | completion listings | one request |
| `<destination>.ifilespart` | a partial download, renamed into place when complete | the bytes already fetched |

## What this deliberately does not do

- **No `sync` or `mirror`.** Two-way sync is a job for a dedicated sync tool, which does it
  correctly.
- **No FUSE mount.** WebDAV covers it with no code here. The server takes HTTP basic auth, ignores
  the user name, and treats the password as the API token:

  ```ini
  [ifiles]
  type = webdav
  url = https://files.example.com/dav/<source>
  vendor = other
  user = anything          # ignored
  pass = <API token>       # obscured via `rclone obscure`
  ```

  Good for browsing and small files. Behind a proxy that caps request bodies, such as Cloudflare's
  free tier, it cannot upload past the cap. That is what `ifiles upload` is for.
- **No user administration.** That is the server binary's job.
- **No token management.** `POST` and `DELETE /api/auth/token` exist and an API token may call them.
  Minting and revoking are rare, and the *first* token has to come from the web UI regardless. A
  `tokens` namespace would duplicate a page nobody can avoid visiting. The question that does recur
  is whether this machine's token is about to expire, and `auth status` answers it with no API call.
- **No TUI.** A browser UI already exists for browsing.

## The published API documentation is wrong in places that matter

ifiles is written against the server's Go source, at the tag `filebrowser/client.go` names, not
against the docs site. The route table and every wire shape come from that source. The docs are wrong or silent in
these places:

| Documented | Actually |
| --- | --- |
| `/api/search` | `/api/tools/search` |
| Upload is multipart form data | Upload is the raw request body, with `X-File-Chunk-Offset` and `X-File-Total-Size` headers |
| — | `GET /api/resources/download` takes repeated `?file=`, not the `?path=` every other route uses |
| — | `GET /api/settings/sources` exists in the router but carries no swagger annotation, so it is absent from the spec |
| — | `POST /api/share` takes `expires` as a *string* number plus a separate `unit`, and falls through to hours for any unit it does not recognize — so a typo lengthens the link rather than erroring |
| — | `DELETE /api/share` answers 400, not 404, for a hash that does not exist |
| — | A share's permission failure is a 403 with **no body**, while every other 403 on those routes carries the handler's own message — which is what makes the two tellable apart |

Upstream moves routes between releases, and a higher tag can be an older release.
`GET /api/settings/sources` and `GET /api/tools/search` arrived in `v1.3.0-stable`, so `auth login`
fails against an older server. An unauthenticated request tells a missing route from a protected
one. A registered route answers `401` with a JSON body, and an unregistered one answers Go's
plain-text `404 page not found`:

```bash
curl -s -o /dev/null -w '%{http_code}\n' https://files.example.com/api/settings/sources
```
