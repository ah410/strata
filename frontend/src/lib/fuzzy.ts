/**
 * Scores how well query matches text as an in-order subsequence, or -1.
 * Matches at the start, after a separator, and in runs score higher, so
 * "nf" ranks "New folder" above "Info".
 */
export function score(query: string, text: string): number {
  const q = query.toLowerCase().replace(/\s+/g, "");
  if (!q) return 0;
  const t = text.toLowerCase();
  let s = 0, ti = 0, run = 0;
  for (const ch of q) {
    const at = t.indexOf(ch, ti);
    if (at < 0) return -1;
    const boundary = at === 0 || /[\s/_.\-]/.test(t[at - 1]);
    run = at === ti && ti > 0 ? run + 1 : 0;
    s += 1 + (boundary ? 8 : 0) + run * 4 - Math.min(at - ti, 5) * 0.5;
    ti = at + 1;
  }
  // Prefer shorter texts when the match is otherwise equal.
  return s - t.length * 0.01;
}

/** Items that match query, best first; all of them, in order, for an empty query. */
export function rank<T>(items: T[], query: string, text: (t: T) => string): T[] {
  if (!query.trim()) return items;
  return items
    .map((it) => ({ it, s: score(query, text(it)) }))
    .filter((x) => x.s >= 0)
    .sort((a, b) => b.s - a.s)
    .map((x) => x.it);
}
