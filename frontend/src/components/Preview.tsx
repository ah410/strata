import { useEffect, useState } from "react";
import { api, type Entry } from "../lib/api";
import { bytes } from "../lib/format";
import { setScopeTheme, themeVars, useScopeTheme } from "../lib/themes";
import { ThemeSelect } from "./Appearance";

/** Full-screen preview for the file kinds a browser can render natively. */
export function Preview({ entry, onClose }: { entry: Entry; onClose: () => void }) {
  const [text, setText] = useState<{ content: string; truncated: boolean } | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && onClose();
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onClose]);

  useEffect(() => {
    if (entry.kind !== "text" && entry.kind !== "csv") return;
    setText(null);
    setError(null);
    api.text(entry.path).then(setText, (e: Error) => setError(e.message));
  }, [entry.path, entry.kind]);

  const url = api.rawUrl(entry.path);
  const scope = entry.kind === "text" || entry.kind === "csv" ? "text" : "media";
  const theme = useScopeTheme(scope);

  return (
    <div
      className="overlay"
      role="dialog"
      aria-modal="true"
      aria-label={entry.name}
      style={themeVars(theme) as React.CSSProperties}
    >
      <div className="overlay-bar">
        <button className="btn btn-icon" onClick={onClose} aria-label="Close preview">
          ✕
        </button>
        <div className="overlay-title">
          <strong>{entry.name}</strong>
          <span>{bytes(entry.size)}</span>
        </div>
        <ThemeSelect compact label="Theme for this file type" value={theme}
          onChange={(id) => setScopeTheme(scope, id)} />
        <a className="btn" href={api.downloadUrl(entry.path)} download>
          Download
        </a>
      </div>

      <div className={`overlay-body${isCentered(entry.kind) ? " center" : ""}`}>
        {error && (
          <div className="state error">
            <h2>Preview unavailable</h2>
            <p>{error}</p>
          </div>
        )}

        {!error && entry.kind === "image" && <img src={url} alt={entry.name} />}
        {!error && entry.kind === "pdf" && <iframe src={url} title={entry.name} />}
        {!error && entry.kind === "video" && <video src={url} controls autoPlay />}
        {!error && entry.kind === "audio" && <audio src={url} controls autoPlay />}

        {!error && (entry.kind === "text" || entry.kind === "csv") && (
          <pre className="code">
            {text ? text.content : "Loading…"}
            {text?.truncated && "\n\n— truncated at 2 MB —"}
          </pre>
        )}

        {!error && !PREVIEWABLE.has(entry.kind) && (
          <div className="state">
            <h2>No preview for this file type</h2>
            <p>Download it to open in an app that understands {entry.name.split(".").pop()}.</p>
          </div>
        )}
      </div>
    </div>
  );
}

const PREVIEWABLE = new Set(["image", "pdf", "video", "audio", "text", "csv"]);

export function canPreview(kind: string): boolean {
  return PREVIEWABLE.has(kind);
}

function isCentered(kind: string): boolean {
  return kind !== "text" && kind !== "csv" && kind !== "pdf";
}
