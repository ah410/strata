import { useEffect } from "react";
import {
  SCOPES, THEMES, THEME_IDS, setAppearance, setScopeTheme, useAppearance,
  type Scope, type ThemeId,
} from "../lib/themes";
import { setPref, usePrefs } from "../lib/prefs";

const LIGHT = THEME_IDS.filter((id) => !THEMES[id].dark);
const DARK = THEME_IDS.filter((id) => THEMES[id].dark);

export function AppearanceDialog({ onClose }: { onClose: () => void }) {
  const { appearance } = useAppearance();
  const prefs = usePrefs();

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && onClose();
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onClose]);

  return (
    <div className="scrim" onMouseDown={(e) => e.target === e.currentTarget && onClose()}>
      <div className="dialog wide" role="dialog" aria-modal="true" aria-label="Settings">
        <h3>Settings</h3>

        <h4>Grid view</h4>
        <label className="field-row toggle">
          <span>
            Show document previews
            <small>PDFs, Word, Excel, PowerPoint and text files show their first page instead of an icon.</small>
          </span>
          <input type="checkbox" role="switch" checked={prefs.previews} onChange={(e) => setPref("previews", e.target.checked)} />
        </label>

        <h4>Theme</h4>
        <div className="theme-grid">
          <button
            className="theme-card"
            aria-pressed={appearance.app === "system"}
            onClick={() => setAppearance({ app: "system" })}
          >
            <span style={{ display: "flex", gap: 3 }}>
              <span style={{ flex: 1 }}><Swatch id={appearance.systemLight} /></span>
              <span style={{ flex: 1 }}><Swatch id={appearance.systemDark} /></span>
            </span>
            Follow system
          </button>
          {THEME_IDS.map((id) => (
            <button
              key={id}
              className="theme-card"
              aria-pressed={appearance.app === id}
              onClick={() => setAppearance({ app: id })}
            >
              <Swatch id={id} />
              {THEMES[id].label}
            </button>
          ))}
        </div>

        {appearance.app === "system" && (
          <>
            <div className="field-row" style={{ marginTop: 8 }}>
              <span>When the system is light</span>
              <ThemeSelect value={appearance.systemLight} ids={LIGHT}
                onChange={(id) => id && setAppearance({ systemLight: id })} />
            </div>
            <div className="field-row">
              <span>When the system is dark</span>
              <ThemeSelect value={appearance.systemDark} ids={DARK}
                onChange={(id) => id && setAppearance({ systemDark: id })} />
            </div>
          </>
        )}

        <h4>By file type</h4>
        {(Object.keys(SCOPES) as Scope[]).map((scope) => (
          <div className="field-row" key={scope}>
            <span>{SCOPES[scope]}</span>
            <ThemeSelect
              value={appearance.scopes[scope] ?? null}
              onChange={(id) => setScopeTheme(scope, id)}
              allowInherit
            />
          </div>
        ))}
        <p className="hint">
          Settings are saved in this browser. PDFs use the browser's own viewer, so only the frame around them changes.
        </p>

        <div className="dialog-actions" style={{ marginTop: 14 }}>
          <button className="btn primary" onClick={onClose}>Done</button>
        </div>
      </div>
    </div>
  );
}

/** Theme dropdown; with allowInherit, null means "same as app". */
export function ThemeSelect({
  value, onChange, ids = THEME_IDS, allowInherit, compact, label,
}: {
  value: ThemeId | null;
  onChange: (id: ThemeId | null) => void;
  ids?: ThemeId[];
  allowInherit?: boolean;
  compact?: boolean;
  label?: string;
}) {
  return (
    <select
      className={`select${compact ? " compact" : ""}`}
      value={value ?? ""}
      aria-label={label}
      onChange={(e) => onChange((e.target.value || null) as ThemeId | null)}
    >
      {allowInherit && <option value="">Same as app</option>}
      {ids.map((id) => (
        <option key={id} value={id}>{THEMES[id].label}</option>
      ))}
    </select>
  );
}

function Swatch({ id }: { id: ThemeId }) {
  const t = THEMES[id];
  return (
    <span className="swatch" style={{ background: t.bg }} aria-hidden="true">
      <i style={{ background: t.ink, width: "70%" }} />
      <i style={{ background: t.yellow, width: "45%" }} />
      <i style={{ background: t.green, width: "60%" }} />
      <i style={{ background: t.select, width: "35%" }} />
    </span>
  );
}
