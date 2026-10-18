import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { marked } from "marked";
import DOMPurify from "dompurify";
import { api, ApiError, type Entry } from "../lib/api";
import { bytes, joinPath, parentOf } from "../lib/format";
import { setScopeTheme, themeVars, useScopeTheme } from "../lib/themes";
import { ThemeSelect } from "./Appearance";
import { History } from "./History";

type Status = { text: string; kind?: "saving" | "saved" | "failed" } | null;

/**
 * Plain-text editor with a Markdown preview. Saves are atomic and versioned
 * on the server; a save against a file changed elsewhere is refused.
 */
export function TextEditor({ entry, onClose }: { entry: Entry; onClose: () => void }) {
  const [content, setContent] = useState<string | null>(null);
  const [saved, setSaved] = useState("");
  const [version, setVersion] = useState("");
  const [readOnly, setReadOnly] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [status, setStatus] = useState<Status>(null);
  const [showHistory, setShowHistory] = useState(false);
  const [rev, setRev] = useState(0);
  const isMarkdown = /\.(md|markdown)$/i.test(entry.name);
  const [mode, setMode] = useState<"edit" | "preview">(isMarkdown ? "preview" : "edit");
  const closeHistory = useCallback(() => setShowHistory(false), []);
  const area = useRef<HTMLTextAreaElement>(null);

  const scope = "text";
  const theme = useScopeTheme(scope);
  const dirty = content !== null && content !== saved;

  useEffect(() => {
    setContent(null);
    setError(null);
    api.text(entry.path).then(
      (t) => {
        setContent(t.content);
        setSaved(t.content);
        setVersion(t.version);
        setReadOnly(t.truncated);
      },
      (e: Error) => setError(e.message)
    );
  }, [entry.path, rev]);

  const save = useCallback(async () => {
    if (content === null || readOnly || !dirty) return;
    setStatus({ text: "Saving…", kind: "saving" });
    try {
      const v = await api.saveText(entry.path, version, content);
      setVersion(v);
      setSaved(content);
      setStatus({ text: "Saved", kind: "saved" });
    } catch (e) {
      const msg = e instanceof ApiError && e.status === 409 ? "Changed elsewhere since you opened it. Copy your edits, then reload." : (e as Error).message;
      setStatus({ text: msg, kind: "failed" });
    }
  }, [content, dirty, entry.path, readOnly, version]);

  const close = useCallback(() => {
    if (dirty && !window.confirm("Discard unsaved changes?")) return;
    onClose();
  }, [dirty, onClose]);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === "s") {
        e.preventDefault();
        save();
      } else if (e.key === "Escape" && !showHistory) close();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [save, close, showHistory]);

  // Closing the tab with unsaved edits asks first.
  useEffect(() => {
    if (!dirty) return;
    const warn = (e: BeforeUnloadEvent) => e.preventDefault();
    window.addEventListener("beforeunload", warn);
    return () => window.removeEventListener("beforeunload", warn);
  }, [dirty]);

  const html = useMemo(
    () => (isMarkdown && mode === "preview" && content !== null ? renderMarkdown(content, parentOf(entry.path)) : ""),
    [isMarkdown, mode, content, entry.path]
  );

  return (
    <div className="overlay" role="dialog" aria-modal="true" aria-label={entry.name} style={themeVars(theme) as React.CSSProperties}>
      <div className="overlay-bar">
        <button className="btn btn-icon" onClick={close} aria-label="Close editor">✕</button>
        <div className="overlay-title">
          <strong>{entry.name}{dirty ? " •" : ""}</strong>
          <span>{readOnly ? `read-only: larger than 2 MB (${bytes(entry.size)})` : status?.text ?? bytes(content?.length ?? entry.size)}</span>
        </div>
        {isMarkdown && (
          <div className="tabs" role="tablist" style={{ border: 0, padding: 0 }}>
            <button className="tab" role="tab" aria-selected={mode === "edit"} onClick={() => { setMode("edit"); setTimeout(() => area.current?.focus()); }}>Edit</button>
            <button className="tab" role="tab" aria-selected={mode === "preview"} onClick={() => setMode("preview")}>Preview</button>
          </div>
        )}
        <ThemeSelect compact label="Theme for this file type" value={theme} onChange={(id) => setScopeTheme(scope, id)} />
        <button className="btn" onClick={() => setShowHistory(true)}>History</button>
        <a className="btn" href={api.downloadUrl(entry.path)} download>Download</a>
        {!readOnly && (
          <button className="btn primary" disabled={!dirty || status?.kind === "saving"} onClick={save}>Save</button>
        )}
      </div>

      {showHistory && (
        <History
          path={entry.path}
          onClose={closeHistory}
          onRestored={() => { setStatus({ text: "Version restored" }); setRev((r) => r + 1); }}
        />
      )}

      <div className="overlay-body">
        {error && (
          <div className="state error">
            <h2>Can't open this file</h2>
            <p>{error}</p>
          </div>
        )}
        {!error && content === null && <div className="state">Loading…</div>}
        {!error && content !== null && mode === "edit" && (
          <textarea
            ref={area}
            className="code editor"
            value={content}
            readOnly={readOnly}
            spellCheck={isMarkdown}
            aria-label={`Contents of ${entry.name}`}
            onChange={(e) => { setContent(e.target.value); if (status?.kind !== "saving") setStatus(null); }}
          />
        )}
        {!error && content !== null && mode === "preview" && (
          // Sanitized by DOMPurify; the app CSP also forbids inline script.
          <article className="markdown" dangerouslySetInnerHTML={{ __html: html }} />
        )}
      </div>
    </div>
  );
}

/** Renders Markdown to sanitized HTML. Relative images load from the share. */
export function renderMarkdown(src: string, dir: string): string {
  const raw = marked.parse(src, { async: false, gfm: true }) as string;
  const hook = (node: Element) => {
    if (node.tagName === "IMG") {
      const s = node.getAttribute("src") ?? "";
      if (s && !/^[a-z][a-z0-9+.-]*:/i.test(s) && !s.startsWith("/") && !s.startsWith("//")) {
        node.setAttribute("src", api.rawUrl(joinPath(dir, decodeURIComponent(s.split("#")[0].split("?")[0]))));
      }
    }
    if (node.tagName === "A" && /^https?:/i.test(node.getAttribute("href") ?? "")) {
      node.setAttribute("target", "_blank");
      node.setAttribute("rel", "noopener noreferrer");
    }
  };
  DOMPurify.addHook("afterSanitizeAttributes", hook);
  try {
    return DOMPurify.sanitize(raw, { ADD_ATTR: ["target"], FORBID_TAGS: ["style", "form", "input", "button"] });
  } finally {
    DOMPurify.removeHook("afterSanitizeAttributes");
  }
}
