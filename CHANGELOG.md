# Changelog

## 1.1.0 (2026-10-28)

### Files
- A right-click menu for files and for empty space in a folder. Shift+right-click
  still opens the browser's own menu.
- Extract `.zip`, `.tar`, `.tar.gz` and `.tgz` archives into a new folder
  beside them, and compress a selection to a zip on the share. Both run in
  the background with progress.
- Cut, copy and paste between folders. Pasting into the same folder makes a
  copy named `name (copy)`.
- Shift-click selects a range, Ctrl/Cmd-click adds one item, and dragging
  across empty space selects everything the box touches.
- Paste files or a screenshot to upload them to the open folder.
- Uploads show how much has gone up out of the total, how many files are
  done, the speed and the time left, and can be canceled. Files appear in the
  folder as they finish rather than all at the end, uploads started meanwhile
  wait their turn, and closing the tab mid-upload asks first.
- An info panel with exact size, type, image dimensions and an on-demand
  SHA-256.
- The Strata name in the sidebar goes back to All files.

### Getting around
- Keyboard shortcuts for every action plus arrow-key navigation and
  type-to-jump, listed in Settings (press `?`).
- Ctrl+K (Cmd+K) opens a palette that runs actions and finds any file or
  folder by name.

### Storage
- Clicking the capacity meter opens a dashboard of what fills the share:
  folders and files by size, the largest files, a split by type, and the
  space the trash and version history use.
- Find duplicate files below a folder and move the extra copies to the trash.

### Previews
- An optional setting (off by default) shows the first page of PDFs, Word,
  Excel, PowerPoint and text files in grid view.

### WebDAV
- With `STRATA_WEBDAV=on`, mount the share as a network drive at `/dav/`.
  Deletes go to the trash and overwrites keep the previous version.

### Fixes
- Uploads no longer fail with "413 Request Entity Too Large" behind a reverse
  proxy. Small files used to go up in one request per upload, which a folder
  of thousands of photos turned into gigabytes; requests now stay around 8 MB
  and shrink further if the proxy still refuses them.
- A single upload can now be as large as the free space on the dataset,
  rather than 8 GB, so 10 GB and 50 GB Takeout archives fit.
- Error pages from a proxy show as a sentence instead of raw HTML.

## 1.0.2 (2026-10-27)

- The sign-in screen says how to reset a forgotten passphrase.
- Security updates: Go 1.27.3 (crypto/x509), excelize with fixes for four
  workbook parsing advisories, and DOMPurify 3.4.17 for the Markdown preview.

## 1.0.1 (2026-10-17)

- The container health check no longer reports a running server as unhealthy
  when the certificate it was given has expired. It still trusts only that
  exact certificate.

## 1.0.0 (2026-10-17)

The first release, and the first one meant for people other than its author.

### Files
- Search names below any folder as you type.
- Select many items and move, copy, download or delete them together. Move
  and copy open a folder picker; rows can also be dragged onto folders or
  the breadcrumb.
- Download folders or a selection as a zip, streamed as it's built.
- Upload whole folders, keeping their structure.
- Large uploads (16 MB and up) go in resumable chunks and pick up where they
  left off after a dropped connection or a page reload.

### Safety net
- Deleting moves items to a per-dataset Trash with Undo. Items are kept 30
  days by default.
- Every time Strata replaces a file, the previous copy is kept (the last 20,
  up to 90 days) and can be downloaded or restored.

### Viewing and editing
- Grid view with image thumbnails.
- A text editor with Markdown preview. Saves are versioned, and a save is
  refused if the file changed elsewhere since you opened it.
- Favorites and recently opened files.
- The `.xlsx` editor keeps formulas intact and refuses stale saves.
- Light, dark and well-known themes, chosen per file type.

### Security and deployment
- Passphrase sign-in (argon2id) with a first-run setup code printed in the
  logs, so whoever reaches the port first can't claim the instance.
- HTTPS by default, with your certificate or a self-signed one.
- Distroless, non-root image for amd64 and arm64, signed with cosign, with
  SBOM and provenance attestations.
