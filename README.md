# Strata

A small file manager for a TrueNAS share. Browse, upload, preview, and edit
`.xlsx` workbooks in the browser — formulas intact.

Single Go binary with the UI embedded. ~25 MB image, no database, no state
of its own.

---

## ⚠️ Read this first

**Strata has no authentication.** It is built to sit behind Tailscale, where
the tailnet is the boundary. That is a deliberate trade — it is why there are
no accounts to manage and no session store.

Do not:
- map it to `0.0.0.0` on a LAN you share with other people
- point a public DNS record at it
- expose it through a Cloudflare tunnel without an Access policy in front

If any of those are your plan, put HTTP Basic auth on the Nginx Proxy Manager
proxy host first.

---

## Deploy on TrueNAS with Dockge

**1. Get the source onto the NAS.**

```bash
# in a TrueNAS shell, inside your Dockge stacks directory
cd /mnt/tank/apps/dockge/stacks
mkdir strata && cd strata
# copy the contents of this folder here (scp, rsync, or git clone)
```

**2. Point it at the dataset you want to browse.**

Edit `docker-compose.yml` and change the left side of the volume line:

```yaml
volumes:
  - /mnt/tank/documents:/data    # ← your dataset goes here
```

The right side is always `/data`. Mount one dataset, not the pool root —
Strata serves everything beneath whatever you give it.

**3. Deploy in Dockge.**

The stack appears in Dockge automatically. Hit **Deploy**. First build takes
2–3 minutes; after that it starts in under a second.

**4. Proxy it with Nginx Proxy Manager.**

Add a proxy host:

| Field | Value |
|---|---|
| Domain | `files.yourtailnet.ts.net` (or your internal domain) |
| Scheme | `http` |
| Forward host | `strata` |
| Forward port | `8080` |
| Websockets | off (not used) |

If NPM lives in a different Dockge stack, the two containers are not on the
same Docker network by default. Either add Strata to NPM's network:

```yaml
networks:
  default:
    name: npm_default
    external: true
```

…or forward to the host IP and port instead of the container name.

**5. Raise the upload limit in NPM.**

NPM defaults to 1 MB, which will reject anything real. In the proxy host's
**Advanced** tab:

```nginx
client_max_body_size 0;
proxy_request_buffering off;
proxy_read_timeout 3600s;
proxy_send_timeout 3600s;
```

`client_max_body_size 0` removes the cap; the timeouts stop long uploads
from being cut off mid-transfer.

**6. Reach it over Tailscale.**

Once NPM is serving it, any device on your tailnet can open it. Nothing
further to configure.

---

## Using it

**Files.** Click a folder to open it, a file to preview it. Drag anything
onto the listing to upload. Tick the checkboxes to rename or delete. Sort by
clicking a column header.

**Previews.** Images, PDFs, video, audio, and text render in the browser.
Video seeks properly — the server honours range requests.

**Workbooks.** Click any `.xlsx` to open the editor.

- Click a cell to select, type to replace, `Enter` to commit
- The formula bar shows the real formula, not the computed value
- A cell with a small triangle in its corner is **calculated** — it holds a
  formula. Typing into one replaces that formula with a literal, so do it
  deliberately.
- `Esc` closes, arrow keys navigate, `Tab` moves right
- Saves write straight to the file on the NAS

Edits recalculate server-side, so changing one input cell updates everything
downstream of it — exactly as Excel would.

---

## Does editing here conflict with editing over SMB?

Yes, if you do both at once. Strata writes the whole file on save, so if the
workbook is open in Excel over an SMB mount at the same time, whichever saves
last wins and the other's changes are gone.

In practice: use one or the other for a given editing session. Strata is the
convenient path from a phone or a borrowed laptop; SMB plus real Excel is
better for heavy work.

---

## Configuration

| Variable | Default | What it does |
|---|---|---|
| `STRATA_ROOT` | `/data` | Directory to serve. Leave it alone; change the volume instead. |
| `STRATA_ADDR` | `:8080` | Listen address. |
| `STRATA_NAME` | `Strata` | Name shown in the sidebar. |
| `TZ` | `America/Phoenix` | Zone for displayed timestamps. |

### Running as a non-root user

By default the container runs as root so it can read files owned by any user.
To restrict it, find who owns your dataset:

```bash
ls -n /mnt/tank/documents
```

then uncomment and set `user: "1000:1000"` in the compose file. If uploads
start failing afterwards, that uid lacks write permission on the dataset.

---

## Development

```bash
# terminal 1 — API
cd backend && go run . -root ~/some-test-folder -addr :8080

# terminal 2 — UI with hot reload, proxying /api to :8080
cd frontend && npm install && npm run dev
```

Build a production binary (embeds the UI):

```bash
cd frontend && npm run build     # writes ../backend/dist
cd ../backend && go build -o strata .
```

Run the tests:

```bash
cd backend && go test ./...
```

The spreadsheet tests need a real workbook with formulas in it, and skip
without one. Point them at any `.xlsx` you have:

```bash
STRATA_TEST_XLSX=/path/to/a-workbook.xlsx go test ./internal/sheet/ -v
```

`internal/safepath` is the security boundary — every request path is resolved
through it, including symlinks. If you change that package, run its tests.

---

## How it is put together

```
backend/
  main.go              HTTP handlers, embeds dist/
  internal/safepath/   path containment — the security boundary
  internal/fileops/    listing, move, copy, delete, atomic save
  internal/sheet/      xlsx read/write via excelize, formula calculation
frontend/
  src/App.tsx          shell, listing, dialogs
  src/components/      Capacity, Glyph, Preview, SheetEditor
  src/lib/             api client, formatters
```

**Design note.** Colour appears only where it carries information: what kind
of file something is, how full the pool is, what is selected. Everything else
is graphite and type. The capacity meter is drawn as a segmented strip
because that is the readout a drive array gives you, and on a NAS it is the
number you open the thing to check.

---

## Known limits

- Sheets render the first 2,000 rows and 64 columns. Bigger workbooks open,
  but the editor shows a truncation notice.
- Charts and images inside a workbook are preserved on save but not
  displayed in the editor.
- The formula engine covers common functions. An unsupported one keeps its
  formula on save but may show a blank value in the browser.
- No search. If you want it, `fileops.List` is where it would go.
- No thumbnails — a grid of image previews would need a cache, and this
  deliberately has no state.
