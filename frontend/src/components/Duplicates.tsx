import { useEffect, useRef, useState } from "react";
import { api, type DupeGroup } from "../lib/api";
import { bytes, parentOf } from "../lib/format";
import { follow } from "../lib/jobs";

/** Every copy but the first in each group: the usual "keep one" choice. */
export function defaultMarks(groups: DupeGroup[]): Set<string> {
  return new Set(groups.flatMap((g) => g.paths.slice(1)));
}

/** Bytes freed by trashing the marked paths. */
export function freed(groups: DupeGroup[], marks: Set<string>): number {
  return groups.reduce((n, g) => n + g.size * g.paths.filter((p) => marks.has(p)).length, 0);
}

/** True if every group keeps at least one unmarked copy. */
export function keepsOne(groups: DupeGroup[], marks: Set<string>): boolean {
  return groups.every((g) => g.paths.some((p) => !marks.has(p)));
}

const SIZES = [
  { label: "64 KB", n: 64 << 10 },
  { label: "1 MB", n: 1 << 20 },
  { label: "100 MB", n: 100 << 20 },
];

/** Finds identical files below a folder and trashes the extra copies. */
export function Duplicates({ path, onNavigate }: { path: string; onNavigate: (folder: string) => void }) {
  const [minSize, setMinSize] = useState(1 << 20);
  const [pct, setPct] = useState<number | null | undefined>(undefined);
  const [groups, setGroups] = useState<DupeGroup[] | null>(null);
  const [marks, setMarks] = useState<Set<string>>(new Set());
  const [note, setNote] = useState<string | null>(null);
  const job = useRef<string | null>(null);

  // A new folder means a new search; leaving stops a running one.
  useEffect(() => {
    setGroups(null);
    setNote(null);
    return () => { if (job.current) api.cancelJob(job.current); };
  }, [path]);

  const scan = async () => {
    setGroups(null);
    setNote(null);
    setPct(null);
    try {
      job.current = await api.duplicates(path, minSize);
      const g = await follow<DupeGroup[]>(job.current, setPct, 700);
      setGroups(g);
      setMarks(defaultMarks(g));
    } catch (e) {
      setNote((e as Error).message);
    } finally {
      job.current = null;
      setPct(undefined);
    }
  };

  const trash = async () => {
    const todo = [...marks];
    let ok = 0;
    for (const p of todo) {
      try {
        await api.remove(p);
        ok++;
      } catch (e) {
        setNote(`${p}: ${(e as Error).message}`);
      }
    }
    const left = groups!.map((g) => ({ ...g, paths: g.paths.filter((p) => !marks.has(p)) })).filter((g) => g.paths.length > 1);
    setGroups(left);
    setMarks(defaultMarks(left));
    if (ok) setNote(`Moved ${ok} file${ok === 1 ? "" : "s"} to the trash. You can restore them from Trash.`);
  };

  const toggle = (p: string) => setMarks((m) => {
    const next = new Set(m);
    next.has(p) ? next.delete(p) : next.add(p);
    return next;
  });

  const running = pct !== undefined;
  const safe = groups ? keepsOne(groups, marks) : true;

  return (
    <div className="dupes">
      <div className="dupes-bar">
        <label>
          Files of at least{" "}
          <select className="select compact" value={minSize} onChange={(e) => setMinSize(Number(e.target.value))} disabled={running}>
            {SIZES.map((s) => <option key={s.n} value={s.n}>{s.label}</option>)}
          </select>
        </label>
        <button className="btn primary" onClick={scan} disabled={running}>
          {running ? (pct == null ? "Looking…" : `Comparing… ${pct}%`) : groups ? "Search again" : "Find duplicates"}
        </button>
      </div>
      {note && <p className="hint">{note}</p>}
      {!groups && !running && <p className="hint">Compares file contents below this folder, so renamed copies are found too. Only files of the same size are read.</p>}
      {groups && groups.length === 0 && <p className="hint">No duplicates here.</p>}
      {groups && groups.length > 0 && (
        <>
          <div className="dupes-bar">
            <span>{groups.length} sets of copies. Trashing the ticked files frees <strong>{bytes(freed(groups, marks))}</strong>.</span>
            <button className="btn danger" disabled={!marks.size || !safe} onClick={trash}>Move {marks.size} to trash</button>
          </div>
          {!safe && <p className="dash-error">Every set needs at least one copy left unticked.</p>}
          <ul className="dupe-list">
            {groups.map((g) => (
              <li key={g.paths[0]}>
                <div className="dupe-head">{g.paths.length} copies of {bytes(g.size)}</div>
                {g.paths.map((p) => (
                  <label key={p} className="dupe-row">
                    <input type="checkbox" checked={marks.has(p)} onChange={() => toggle(p)} />
                    <span className="bar-label">{p.split("/").pop()}</span>
                    <button className="link" onClick={(e) => { e.preventDefault(); onNavigate(parentOf(p)); }}>/{parentOf(p)}</button>
                  </label>
                ))}
              </li>
            ))}
          </ul>
        </>
      )}
    </div>
  );
}
