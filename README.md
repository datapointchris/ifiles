# ifiles

A Go CLI for [FileBrowser Quantum](https://github.com/gtsteffaniak/filebrowser), so files move in and
out of `files.ichrisbirch.com` from a terminal instead of through the browser UI.

```bash
ifiles ls /photos                          # what is in a remote directory
ifiles get /photos/raw.cr2                 # download
ifiles put video.mkv /media                # upload, chunked
ifiles search invoice                      # find a file without opening the browser
ifiles shares create /docs/contract.pdf    # hand it to someone with no account
```

## Why it exists

Quantum ships no client CLI, and the one built into its own binary operates on the local Bolt
database with the server stopped — it cannot address a remote instance at all.

More to the point, **nothing else can upload a large file through the tunnel.** Cloudflare's free
tier caps a request body at 100 MB, and Quantum's WebDAV has no chunking
([upstream #2404](https://github.com/gtsteffaniak/filebrowser/issues/2404)), so `rclone copy` of
anything big fails with a 413 that no retry clears. The web UI works only because it chunks over the
REST API. `ifiles` does the same thing from a terminal.

## Install

```bash
task install     # dev build to ~/.local/bin
```

## Shell completion

Remote paths tab-complete against the server, so a long filename is a Tab rather than a copy out of
the web UI:

```bash
ifiles get /photos/2026-07-<TAB>
```

`get`, `cat`, `rm`, `mv`, `cp`, and `put`'s remote argument complete any entry; `ls` and `mkdir`
complete only directories, since a file in either position can only produce a failed command. A
directory arrives with a trailing slash and no trailing space, so a second Tab descends into it. Local
positions — `put`'s first argument, `get`'s second — fall through to the shell's own file completion,
and `--source` completes from the server's source list.

Install the script the way every other completion in `~/dotfiles` is installed:

```bash
ifiles completion zsh > "$XDG_CACHE_HOME/zsh/completions/ifiles.zsh"
```

Each Tab is a real request: the shell runs `ifiles __complete get /photos/` as a fresh process, so
nothing can be held in memory between them. Listings are cached for ten seconds at
`$XDG_CACHE_HOME/ifiles/listings.json` — long enough that tabbing through one directory hits the
server once, short enough that a file uploaded by the previous command shows up in the next
completion. A completion that fails for any reason — no token, tunnel down, expired token — offers
nothing and says nothing, because its stdout *is* the candidate list and anything written there
arrives as a suggestion.

## Authentication

The instance is OIDC-only, so there is no password to send and no headless login. Access is a
long-lived API token:

1. In the web UI, go to **Settings → API tokens** and create one.
2. `ifiles auth login https://files.ichrisbirch.com` and paste it at the prompt.

The token goes in the OS keyring, keyed by server URL — never into the config file. Login verifies it
and records the source name, which every resource call needs and nobody should have to type.

The account needs the **`api` permission** for step 1 to work. Without it the server answers 403,
because minting a token is itself an API-permissioned action.

For a script, `IFILES_TOKEN` overrides both stores, or pipe it in:

```bash
pass show ifiles | ifiles auth login -
```

**On a host with no keyring**, the token goes to `$XDG_STATE_HOME/ifiles/token.json` at `0600`
instead. On Linux the keyring is the Secret Service over D-Bus, which WSL, containers, and headless
servers do not have — there the keyring call fails before any request is made, with
`exec: "dbus-launch": executable file not found in $PATH`. Refusing to store a token would leave the
tool unusable on exactly the machines whose only other route to the server is a browser, so it stores
one and says where: `auth login` reports the fallback, and `ifiles auth status` names the backend a
token came from. The keyring still wins whenever it holds a token, so a host that gains a Secret
Service later moves onto it without a re-login.

## Configuration

TOML at `$XDG_CONFIG_HOME/ifiles/config.toml`, written by `auth login`:

```toml
url = "https://files.ichrisbirch.com"
source = "files"
remote_dir = "/inbox"   # where `put` sends things by default
chunk_size = "32MiB"    # must stay under the 100 MB request cap
```

`IFILES_URL`, `IFILES_SOURCE`, `IFILES_TOKEN`, and `IFILES_CHUNK_SIZE` override the file.

## Transfers

**Uploads are always chunked**, small ones included — one code path rather than two, and the
unchunked path could not clear the 100 MB cap anyway.

**Interrupting is safe.** On Ctrl-C the server is told to keep its partial file *before* the transfer
is aborted; that ordering is the whole mechanism, because the server only consults its pause register
when a chunk request fails. The offset is recorded at
`$XDG_STATE_HOME/ifiles/transfers.json` — it has to be kept locally, since no endpoint reports how
much of a partial upload arrived. Re-running the same command resumes.

A local file that changed between attempts invalidates the resume. The server seeks to whatever
offset it is given without checking the partial file is really that long, so resuming a modified file
would write new bytes into the middle of an old upload and produce a corrupt result that passes every
check the server makes.

**Downloads resume too**, via an HTTP range request against the partial `.ifilespart` file. A
directory download does not: the server generates the archive as it streams, so there is nothing to
seek to.

## Share links

A share link hands a file or directory to someone with no account here:

```bash
ifiles shares create /docs/contract.pdf --expires 7d --password
ifiles shares list
ifiles shares rm https://files.ichrisbirch.com/public/share/T7bQ3xkLm2
```

The URL is the only thing on stdout, so it pipes straight into `pbcopy` or a message. `--expires`
takes a window rather than a date — `7d`, `12h`, `90m` — and without it the link never expires.
`--password` reads from a hidden prompt or from stdin, never from argv. A directory shares the whole
subtree, with the server building the archive when the recipient downloads it.

`shares list` prints the URL rather than the hash, because that column is both the thing you came for
and the handle: it pastes back into `shares rm`, which also takes a bare hash or a download URL. An
expired link still appears — the server keeps it — marked `expired`, and a link whose file has since
been deleted is marked `gone`, since it still resolves and 404s for whoever holds it.

This needs the **`share` permission**, which is separate from `api` and from `admin`. Without it the
server refuses with a bare 403 carrying no message at all, so `ifiles` supplies the explanation.

## What this deliberately does not do

- **No `sync` or `mirror`.** Syncthing already does two-way sync, correctly.
- **No FUSE mount.** WebDAV covers it with no code here:

  ```ini
  [ifiles]
  type = webdav
  url = https://files.ichrisbirch.com/dav/files
  vendor = other
  user = anything          # ignored
  pass = <API token>       # obscured via `rclone obscure`
  ```

  Good for browsing and small files. It cannot upload over 100 MB — that is what `ifiles put` is for.
- **No user administration.** That is the server binary's job, and a once-a-year task.
- **No token management.** `POST`/`DELETE /api/auth/token` exist and an API token may call them, but
  minting and revoking happen about once a year, and the *first* token has to come from the web UI
  regardless — so a `tokens` namespace would duplicate a page nobody can avoid visiting.

  The one question that does recur is whether this machine's token is about to expire silently, and
  that needs no API at all: the token is a JWT sitting in the keyring, so `ifiles auth status` reads
  its `exp` claim locally and prints the date, warning when under a fortnight remains. The server's
  own `X-Renew-Token` header fires thirty minutes out, which on a year-long token is no warning.
- **No TUI.** A browser UI already exists for browsing.

## Notes on the API

The route table and every wire shape were read from the server's source at tag `v1.5.0-stable`, which
is the image the deployment pins. The published documentation is wrong in places that matter:

| Documented | Actually |
| --- | --- |
| `/api/search` | `/api/tools/search` |
| Upload is multipart form data | Upload is the raw request body, with `X-File-Chunk-Offset` and `X-File-Total-Size` headers |
| — | `GET /api/resources/download` takes repeated `?file=`, not the `?path=` every other route uses |
| — | `GET /api/settings/sources` exists in the router but carries no swagger annotation, so it is absent from the spec |
| — | `POST /api/share` takes `expires` as a *string* number plus a separate `unit`, and falls through to hours for any unit it does not recognize — so a typo lengthens the link rather than erroring |
| — | `DELETE /api/share` answers 400, not 404, for a hash that does not exist |
| — | A share's permission failure is a 403 with **no body**, while every other 403 on those routes carries the handler's own message — which is what makes the two tellable apart |
