import { useCallback, useEffect, useState } from "react";
import { api, type Entry } from "../lib/api";
import { bytes } from "../lib/format";
import { setScopeTheme, themeVars, useScopeTheme } from "../lib/themes";
import { ThemeSelect } from "./Appearance";
import { History } from "./History";

/** Full-screen preview for media a browser renders natively (text opens in TextEditor). */
export function Preview({ entry, onClose }: { entry: Entry; onClose: () => void }) {
  const [showHistory, setShowHistory] = useState(false);
  const [rev, setRev] = useState(0); // bumps after a restore to reload
  const closeHistory = useCallback(() => setShowHistory(false), []);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && onClose();
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onClose]);

  const url = api.rawUrl(entry.path) + (rev ? `&r=${rev}` : "");
  const scope = "media";
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
        <button className="btn" onClick={() => setShowHistory(true)}>
          History
        </button>
        <a className="btn" href={api.downloadUrl(entry.path)} download>
          Download
        </a>
      </div>

      {showHistory && <History path={entry.path} onClose={closeHistory} onRestored={() => setRev((r) => r + 1)} />}
      <div className={`overlay-body${isCentered(entry.kind) ? " center" : ""}`}>
        {entry.kind === "image" && <img src={url} alt={entry.name} />}
        {entry.kind === "pdf" && <iframe src={url} title={entry.name} />}
        {entry.kind === "video" && <video src={url} controls autoPlay />}
        {entry.kind === "audio" && <audio src={url} controls autoPlay />}

        {!PREVIEWABLE.has(entry.kind) && (
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
  return kind !== "pdf";
}
