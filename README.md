# ifiles

A Go CLI for [FileBrowser Quantum](https://github.com/gtsteffaniak/filebrowser), so files move in and
out of `files.ichrisbirch.com` from a terminal instead of through the browser UI.

```bash
ifiles ls /photos              # what is in a remote directory
ifiles get /photos/raw.cr2     # download
ifiles put video.mkv /media    # upload, chunked
ifiles search invoice          # find a file without opening the browser
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

## Authentication

The instance is OIDC-only, so there is no password to send and no headless login. Access is a
long-lived API token:

1. In the web UI, go to **Settings → API tokens** and create one.
2. `ifiles auth login https://files.ichrisbirch.com` and paste it at the prompt.

The token goes in the OS keyring, keyed by server URL — never into the config file. Login verifies it
and records the source name, which every resource call needs and nobody should have to type.

The account needs the **`api` permission** for step 1 to work. Without it the server answers 403,
because minting a token is itself an API-permissioned action.

For a script, `IFILES_TOKEN` overrides the keyring, or pipe it in:

```bash
pass show ifiles | ifiles auth login -
```

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
`$XDG_STATE_HOME/ifiles/uploads.json` — it has to be kept locally, since no endpoint reports how much
of a partial upload arrived. Re-running the same command resumes.

A local file that changed between attempts invalidates the resume. The server seeks to whatever
offset it is given without checking the partial file is really that long, so resuming a modified file
would write new bytes into the middle of an old upload and produce a corrupt result that passes every
check the server makes.

**Downloads resume too**, via an HTTP range request against the partial `.ifilespart` file. A
directory download does not: the server generates the archive as it streams, so there is nothing to
seek to.

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
- **No TUI.** A browser UI already exists for browsing.

## Notes on the API

The route table and every wire shape were read from the server's source at tag `v1.5.0-stable`, which
is what the deployed `gtstef/filebrowser:stable` image resolves to. The published documentation is
wrong in places that matter:

| Documented | Actually |
| --- | --- |
| `/api/search` | `/api/tools/search` |
| Upload is multipart form data | Upload is the raw request body, with `X-File-Chunk-Offset` and `X-File-Total-Size` headers |
| — | `GET /api/resources/download` takes repeated `?file=`, not the `?path=` every other route uses |
| — | `GET /api/settings/sources` exists in the router but carries no swagger annotation, so it is absent from the spec |
