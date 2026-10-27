# Changelog

## 1.0.2 (2026-10-27)

- The sign-in screen says how to reset a forgotten passphrase.

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
