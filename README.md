# Strata

A self-hosted file manager for a TrueNAS share. Browse, search, upload and
preview files from any browser, and edit `.xlsx` workbooks and text files in
place without losing formulas. Deleted files go to a trash and replaced files
keep their history, so a slip in the browser is never final.

It's one Go binary with the UI embedded, shipped as a distroless image of
about 20 MB. There's no database: files stay where they are on your dataset,
and the little Strata remembers (the passphrase hash, a TLS certificate,
favorites, a thumbnail cache) lives in one config directory.

Created with Claude as a POC.

![The grid view with image thumbnails](docs/screenshots/2-grid.png)

## Features

- **Files:** browse, search names below any folder, upload files or whole
  folders, and download a selection as a zip. Rename, move or copy with a
  folder picker or by dragging. Large uploads resume after a dropped
  connection.
- **Previews:** images, PDF, video and audio play in the browser, and a grid
  view shows image thumbnails.
- **Editing:** a spreadsheet editor for `.xlsx` that recalculates on the
  server and keeps formulas intact, and a text editor with Markdown preview.
  Both refuse to overwrite a file that changed since you opened it.
- **Safety net:** a trash with Undo, and version history for every file
  Strata replaces.
- **Favorites, recent files, and themes:** light, dark and well-known themes,
  chosen per file type.
- **Hardened by default:** passphrase sign-in, HTTPS, a non-root
  distroless image, and a read-only root filesystem. See
  [SECURITY.md](SECURITY.md).

## Install

### TrueNAS SCALE

Strata is [submitted to the TrueNAS community catalog](https://github.com/truenas/apps/pull/6142)
and pending review. Once it's listed, install **Strata** from Apps, pick the
dataset to browse, and give the app's user (568 `apps` by default) a
**Modify** ACL entry on it. When it's running, open the app's logs, copy the
setup code, and open the Web UI to choose a passphrase. Until then, use the
compose file below.

### Docker Compose (or a Dockge stack)

Copy [docker-compose.yml](docker-compose.yml), set the dataset path and a
`user:` that can write to it, and run `docker compose up -d`. Then:

```bash
docker logs strata 2>&1 | grep "SETUP CODE"
```

Open `https://<your-nas>:8443`, accept the self-signed certificate (the logs
print its fingerprint so you can check it), enter the code, and choose a
passphrase.

Images are published as `ghcr.io/ah410/strata` for amd64 and arm64, signed
with cosign. Pin a version (`:1.0.0`) or a digest in production.

## Sign-in and HTTPS

Strata asks for a passphrase before showing anything. There are no user
accounts: one passphrase, hashed with argon2id, unlocks it.

**First run.** With a config volume at `/config`, Strata prints a one-time
setup code in its logs on every start until setup is done:

```
FIRST-RUN SETUP CODE: 34EB-WUZC-7XXO-XQJV-BLKK
```

Open Strata, enter the code, and choose a passphrase of 12 characters or
more. The code proves you can read the app's logs, so someone who merely
reaches the port first can't claim your instance. Only the hash is stored, in
`/config/auth.hash`.

**Forgot the passphrase?** Delete `/config/auth.hash` and restart. A new
setup code is printed.

**HTTPS.** Strata serves HTTPS itself unless told otherwise:

- With `STRATA_TLS_CERT` and `STRATA_TLS_KEY`, it uses that certificate.
- Without them, it creates a self-signed certificate once, keeps it in
  `/config/tls/`, and logs its SHA-256 fingerprint. Your browser warns the
  first time; compare the fingerprint before accepting.
- Behind a reverse proxy that terminates TLS, set `STRATA_TRUSTED_PROXIES`
  to the proxy's address or Docker network. Strata then serves plain HTTP on
  that internal network and believes the proxy's `X-Forwarded-Proto` and
  `X-Forwarded-For`. `STRATA_TLS=off` or `on` overrides the choice.

The session cookie is `Secure`, so sign-in only works over HTTPS (or on
localhost).

**Without a config volume** you can supply the hash yourself: run
`docker run --rm -it ghcr.io/ah410/strata:1 hash-password > strata_password`
and point `STRATA_PASSWORD_FILE` at it, for example as a compose secret. That
file takes precedence over `/config/auth.hash`.

Sessions are kept in memory, so restarting the container signs everyone out,
which is also how a new passphrase takes effect. Five wrong guesses lock a
client out with growing delays (up to 15 minutes), and a global cap stops
rotating addresses from helping. The setup code is throttled the same way.

If Strata is only reachable on a network you fully trust, `STRATA_AUTH=none`
turns sign-in off. It logs a warning on every start.

## Using it

Click a folder to open it and a file to preview or edit it. Drop files or
folders anywhere on the listing to upload them. Tick checkboxes to act on
several items at once; dragging rows onto a folder or a breadcrumb moves them
(hold Ctrl or Alt to copy). The search box looks at names below the folder
you're in. The button next to it switches between list and grid.

**Workbooks.** Click any `.xlsx` to open the editor. Click a cell to select
it, type to replace, and press Enter to commit. The formula bar shows the real
formula rather than the computed value. A cell with a small triangle in its
corner holds a formula, and typing into it replaces that formula with a
literal, so do it deliberately. Edits recalculate on the server, so
everything downstream updates the way it would in Excel.

**Text and Markdown.** Text, CSV and Markdown files open in an editor; Ctrl+S
saves. Markdown opens in a rendered preview first. Relative images in
Markdown load from the share, and remote images are blocked.

## Trash and version history

Deleting moves things to the **Trash** in the sidebar, and the toast offers
**Undo**. Items stay restorable for 30 days (`STRATA_TRASH_DAYS`), then
they're removed for good. Restoring never overwrites: if the old name is
taken, the item comes back as `name (restored).ext`.

Every time Strata replaces a file (saving a workbook or text file, uploading
over an existing file, restoring an old version), the previous copy is kept.
**History** lists the last 20 copies from up to 90 days back, with Download
and Restore. Restoring is itself kept as a version, so it can be undone too.

Both live in a `.strata` folder at the top of each dataset, so moving
something into them is an instant rename rather than a copy. SMB users will
see that folder; leave it alone, or delete it to discard all trash and history
for that dataset. Partial uploads wait in the same folder and are cleaned up
after a day. None of this replaces ZFS snapshots, which also cover changes
made outside Strata.

## Editing here and over SMB

Strata won't overwrite someone else's save. Each save is checked against the
version the editor loaded; if the file changed since (Excel over SMB, another
tab), the save is refused and your edits stay on screen. Saves are atomic, so
a crash mid-save never leaves a half-written file.

Excel has no such check, though: if Strata saves while the workbook is open in
Excel, Excel's next save wins. Use one or the other for a given editing
session. Strata is the convenient path from a phone or a borrowed laptop, and
SMB plus real Excel is better for heavy work.

## Configuration

| Variable | Default | What it does |
|---|---|---|
| `STRATA_ROOT` | `/data` | Directory to serve. Change the volume rather than this. |
| `STRATA_ADDR` | `:8080` | Listen address. |
| `STRATA_NAME` | `Strata` | Name shown in the sidebar. |
| `STRATA_CONFIG` | `/config` if mounted | Passphrase hash, TLS certificate, thumbnails, favorites and recent files. Optional, but setup, favorites and recents need it. |
| `STRATA_ALLOWED_HOSTS` | *(empty)* | Comma-separated hostnames you open Strata by, e.g. `strata.example.com`. IP addresses and `localhost` always work; any other `Host` is refused, which blocks DNS rebinding. |
| `STRATA_TRUSTED_PROXIES` | *(empty)* | Comma-separated IPs or CIDRs of your reverse proxy. Only these may set `X-Forwarded-For` and `X-Forwarded-Proto`. |
| `STRATA_TLS` | `auto` | `auto` means HTTPS, unless behind a trusted proxy with no certificate given. `on` and `off` force it. |
| `STRATA_TLS_CERT`, `STRATA_TLS_KEY` | *(none)* | PEM certificate and key to serve. Without them, a self-signed pair is used. |
| `STRATA_PASSWORD_FILE` | *(none)* | A hash from `strata hash-password`, used instead of first-run setup. |
| `STRATA_AUTH` | `passphrase` | `none` disables sign-in. Only for networks you fully trust. |
| `STRATA_TRASH_DAYS` | `30` | Days a deleted item stays restorable. |
| `TZ` | `Etc/UTC` | Zone for log timestamps. The UI shows times in the browser's zone. |

### Which user it runs as

The image never runs as root. Its default user (uid 65532) usually has no
access to your dataset, so run it as an account that does. On TrueNAS the
cleanest setup is a dedicated user with no login access, given a **Modify**
entry with **Inherit** on the dataset's ACL, then `user: "<uid>:<gid>"` in
compose (or the app's user setting in the catalog). If uploads fail with
"permission denied", that uid can't write there; the container logs show the
exact path.

Don't `chmod` files on a dataset with `aclmode=restricted`: it fails on any
file with an ACL. Strata never needs to.

### Behind Nginx Proxy Manager

Put Strata on NPM's Docker network, set `STRATA_TRUSTED_PROXIES` to that
network's subnet and `STRATA_ALLOWED_HOSTS` to your hostname, and forward to
`strata:8080` over `http`. Drop the `ports:` mapping so the only way in is
through the proxy. NPM's default 1 MB body limit rejects real uploads, so add
this under the proxy host's **Advanced** tab:

```nginx
client_max_body_size 0;
proxy_request_buffering off;
proxy_read_timeout 3600s;
proxy_send_timeout 3600s;
```

## Development

```bash
# terminal 1: API
cd backend && STRATA_AUTH=none STRATA_TLS=off go run . -root ~/some-test-folder

# terminal 2: UI with hot reload, proxying /api to :8080
cd frontend && npm install && npm run dev
```

Build a production binary (it embeds the UI):

```bash
cd frontend && npm run build     # writes ../backend/dist
cd ../backend && go build -o strata .
```

Run the tests with `cd backend && go test -race ./...`. The workbook tests
generate their own files; `STRATA_TEST_XLSX=/path/to/a.xlsx` adds a round trip
through a real workbook of yours.

`internal/safepath` is the security boundary. Every request path goes
through it, so run its tests whenever it changes.

```
backend/
  main.go              HTTP handlers and routes; embeds dist/
  internal/safepath/   path containment (the security boundary)
  internal/fileops/    listing, move, copy, search, zip, uploads, trash, versions
  internal/sheet/      xlsx read and write via excelize
  internal/thumb/      image thumbnails and their cache
  internal/state/      favorites and recent files
  internal/auth/       passphrase, sessions, throttling, first-run setup
  internal/tlsutil/    HTTPS with a provided or self-signed certificate
  internal/web/        Host, CSRF and security-header guards
frontend/src/
  App.tsx              shell, listing, dialogs
  components/          editors, previews, pickers, trash and history
  lib/                 API client, uploads, themes, formatters
```

Colour appears only where it carries information: what kind of file
something is, how full the pool is, what's selected. Everything else is
graphite and type. The capacity meter is a segmented strip because that's the
readout a drive array gives you, and on a NAS it's the number you open the
thing to check.

## Known limits

- Sheets render the first 2,000 rows and 64 columns. Bigger workbooks open
  with a truncation notice.
- Charts and images inside a workbook are kept on save but not shown.
- The formula engine covers common functions. An unsupported one keeps its
  formula on save but may show a blank value in the browser.
- One passphrase, no separate user accounts, and no share links yet.
- Video files have no thumbnails.

## License

[MIT](LICENSE)
