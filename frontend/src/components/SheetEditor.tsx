import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { api, type Cell, type Entry, type Grid } from "../lib/api";
import { colName } from "../lib/format";

type SaveState = "idle" | "saving" | "saved" | "failed";

/**
 * Spreadsheet editor.
 *
 * The contract that matters: a formula cell is a derived value. We display
 * what the server calculated, mark the cell, and only overwrite it if the
 * user deliberately types into it. Everything else in the workbook — other
 * sheets, styles, charts — is left untouched by a save.
 */
export function SheetEditor({ entry, onClose }: { entry: Entry; onClose: () => void }) {
  const [grid, setGrid] = useState<Grid | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [sel, setSel] = useState<{ row: number; col: number }>({ row: 1, col: 1 });
  const [draft, setDraft] = useState<string | null>(null);
  const [save, setSave] = useState<SaveState>("idle");

  const cellInput = useRef<HTMLInputElement>(null);
  const barInput = useRef<HTMLInputElement>(null);

  const load = useCallback(
    (sheet?: string) => {
      setError(null);
      api.sheet(entry.path, sheet).then(setGrid, (e: Error) => setError(e.message));
    },
    [entry.path]
  );

  useEffect(() => load(), [load]);

  // Index cells by reference, and work out which cells a merge hides.
  const { byRef, anchors, covered } = useMemo(() => {
    const byRef = new Map<string, Cell>();
    const anchors = new Map<string, { rowSpan: number; colSpan: number }>();
    const covered = new Set<string>();

    if (grid) {
      for (const c of grid.cells) byRef.set(c.ref, c);

      for (const m of grid.merges ?? []) {
        anchors.set(m.ref, { rowSpan: m.rowSpan, colSpan: m.colSpan });
        for (let r = m.row; r < m.row + m.rowSpan; r++) {
          for (let c = m.col; c < m.col + m.colSpan; c++) {
            const ref = `${colName(c)}${r}`;
            if (ref !== m.ref) covered.add(ref);
          }
        }
      }
    }
    return { byRef, anchors, covered };
  }, [grid]);

  const selRef = `${colName(sel.col)}${sel.row}`;
  const selCell = byRef.get(selRef);

  // What the formula bar shows: the formula if there is one, else the value.
  const barValue = draft !== null ? draft : selCell?.formula ? `=${selCell.formula}` : selCell?.value ?? "";

  const commit = useCallback(
    async (ref: string, value: string) => {
      if (!grid) return;
      setDraft(null);
      setSave("saving");
      try {
        const next = await api.saveSheet(entry.path, grid.active, [{ ref, value }]);
        setGrid(next);
        setSave("saved");
        setTimeout(() => setSave((s) => (s === "saved" ? "idle" : s)), 1600);
      } catch (e) {
        setError((e as Error).message);
        setSave("failed");
      }
    },
    [entry.path, grid]
  );

  // Focus the inline editor the moment editing starts.
  useEffect(() => {
    if (draft !== null) cellInput.current?.focus();
  }, [draft !== null]);

  const move = (dr: number, dc: number) => {
    if (!grid) return;
    setDraft(null);
    setSel((s) => ({
      row: Math.min(Math.max(1, s.row + dr), grid.maxRow),
      col: Math.min(Math.max(1, s.col + dc), grid.maxCol),
    }));
  };

  const onGridKey = (e: React.KeyboardEvent) => {
    if (draft !== null) return; // the inline input owns the keyboard

    switch (e.key) {
      case "ArrowUp":    e.preventDefault(); return move(-1, 0);
      case "ArrowDown":  e.preventDefault(); return move(1, 0);
      case "ArrowLeft":  e.preventDefault(); return move(0, -1);
      case "ArrowRight": e.preventDefault(); return move(0, 1);
      case "Tab":
        e.preventDefault();
        return move(0, e.shiftKey ? -1 : 1);
      case "Enter":
      case "F2":
        e.preventDefault();
        return setDraft(selCell?.formula ? `=${selCell.formula}` : selCell?.value ?? "");
      case "Delete":
      case "Backspace":
        e.preventDefault();
        return commit(selRef, "");
      case "Escape":
        return onClose();
    }

    // A printable character starts an edit, replacing the cell, as Excel does.
    if (e.key.length === 1 && !e.metaKey && !e.ctrlKey && !e.altKey) {
      setDraft(e.key);
      e.preventDefault();
    }
  };

  if (error && !grid) {
    return (
      <Shell entry={entry} onClose={onClose}>
        <div className="state error">
          <h2>Could not open this workbook</h2>
          <p>{error}</p>
        </div>
      </Shell>
    );
  }

  if (!grid) {
    return (
      <Shell entry={entry} onClose={onClose}>
        <div className="state">
          <p>Opening…</p>
        </div>
      </Shell>
    );
  }

  const widths = new Map(grid.columns.map((c) => [c.index, c.width]));
  const heights = new Map(grid.rows.map((r) => [r.index, r.height]));

  return (
    <Shell entry={entry} onClose={onClose}>
      <div className="tabs" role="tablist">
        {grid.sheets.map((name) => (
          <button
            key={name}
            role="tab"
            className="tab"
            aria-selected={name === grid.active}
            onClick={() => {
              setDraft(null);
              setSel({ row: 1, col: 1 });
              load(name);
            }}
          >
            {name}
          </button>
        ))}
      </div>

      <div className="formula-bar">
        <span className="formula-ref">{selRef}</span>
        <input
          ref={barInput}
          className="formula-input"
          value={barValue}
          placeholder={selCell?.formula ? "" : "Empty cell"}
          onChange={(e) => setDraft(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter") {
              e.preventDefault();
              commit(selRef, barValue);
              barInput.current?.blur();
            } else if (e.key === "Escape") {
              setDraft(null);
              barInput.current?.blur();
            }
          }}
        />
      </div>

      <div className="grid-wrap" tabIndex={0} onKeyDown={onGridKey}>
        <table className="grid">
          <colgroup>
            <col style={{ width: 42 }} />
            {Array.from({ length: grid.maxCol }, (_, i) => (
              <col key={i} style={{ width: charsToPx(widths.get(i + 1)) }} />
            ))}
          </colgroup>

          <thead>
            <tr>
              <th className="corner" />
              {Array.from({ length: grid.maxCol }, (_, i) => (
                <th key={i} className="colhead">
                  {colName(i + 1)}
                </th>
              ))}
            </tr>
          </thead>

          <tbody>
            {Array.from({ length: grid.maxRow }, (_, r) => {
              const row = r + 1;
              return (
                <tr key={row} style={{ height: ptToPx(heights.get(row)) }}>
                  <th className="rowhead">{row}</th>

                  {Array.from({ length: grid.maxCol }, (_, c) => {
                    const col = c + 1;
                    const ref = `${colName(col)}${row}`;
                    if (covered.has(ref)) return null;

                    const cell = byRef.get(ref);
                    const span = anchors.get(ref);
                    const isSel = sel.row === row && sel.col === col;
                    const editing = isSel && draft !== null;

                    return (
                      <td
                        key={ref}
                        rowSpan={span?.rowSpan}
                        colSpan={span?.colSpan}
                        className={[
                          isSel ? "sel" : "",
                          cell?.formula ? "calc" : "",
                          cell?.style?.wrap ? "wrap" : "",
                        ].filter(Boolean).join(" ")}
                        style={cellStyle(cell)}
                        onMouseDown={() => {
                          if (!editing) {
                            setDraft(null);
                            setSel({ row, col });
                          }
                        }}
                        onDoubleClick={() =>
                          setDraft(cell?.formula ? `=${cell.formula}` : cell?.value ?? "")
                        }
                      >
                        {editing ? (
                          <input
                            ref={cellInput}
                            value={draft}
                            onChange={(e) => setDraft(e.target.value)}
                            onBlur={() => commit(ref, draft)}
                            onKeyDown={(e) => {
                              if (e.key === "Enter") {
                                e.preventDefault();
                                commit(ref, draft);
                                move(1, 0);
                              } else if (e.key === "Escape") {
                                e.preventDefault();
                                setDraft(null);
                              } else if (e.key === "Tab") {
                                e.preventDefault();
                                commit(ref, draft);
                                move(0, e.shiftKey ? -1 : 1);
                              }
                            }}
                          />
                        ) : (
                          cell?.value ?? ""
                        )}
                      </td>
                    );
                  })}
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>

      <div className="sheet-note">
        <span>Enter edits · Esc closes · ▲ marks a calculated cell</span>
        {grid.truncated && <span>Showing the first {grid.maxRow} rows</span>}
        {save === "saving" && <span className="saving">Saving…</span>}
        {save === "saved" && <span className="saved">Saved</span>}
        {save === "failed" && <span className="failed">{error ?? "Save failed"}</span>}
      </div>
    </Shell>
  );
}

function Shell({
  entry,
  onClose,
  children,
}: {
  entry: Entry;
  onClose: () => void;
  children: React.ReactNode;
}) {
  return (
    <div className="overlay" role="dialog" aria-modal="true" aria-label={entry.name}>
      <div className="overlay-bar">
        <button className="btn btn-icon" onClick={onClose} aria-label="Close workbook">
          ✕
        </button>
        <div className="overlay-title">
          <strong>{entry.name}</strong>
        </div>
        <a className="btn" href={api.downloadUrl(entry.path)} download>
          Download
        </a>
      </div>
      {children}
    </div>
  );
}

/** Excel column width is measured in characters of the default font. */
function charsToPx(w: number | undefined): number {
  return Math.round((w ?? 10) * 7.2) + 8;
}

/** Row height is in points. */
function ptToPx(h: number | undefined): number {
  return Math.max(22, Math.round((h ?? 15) * 1.34));
}

function cellStyle(cell: Cell | undefined): React.CSSProperties {
  const s = cell?.style;
  if (!s) return {};
  return {
    fontWeight: s.bold ? 600 : undefined,
    fontStyle: s.italic ? "italic" : undefined,
    color: s.color || undefined,
    background: s.fill || undefined,
    textAlign: (s.align as React.CSSProperties["textAlign"]) || undefined,
    fontSize: s.size ? `${Math.min(s.size, 20)}px` : undefined,
    fontFamily: s.family?.includes("Courier") ? "var(--mono)" : undefined,
  };
}
