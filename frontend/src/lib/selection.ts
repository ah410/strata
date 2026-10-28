/** The paths from a to b inclusive, in display order; just b if a isn't shown. */
export function range(order: string[], a: string | null, b: string): string[] {
  const i = a === null ? -1 : order.indexOf(a);
  const j = order.indexOf(b);
  if (j < 0) return [];
  if (i < 0) return [b];
  return order.slice(Math.min(i, j), Math.max(i, j) + 1);
}

export interface Box { left: number; top: number; right: number; bottom: number }

/** The box spanned by two corner points, whichever way the drag went. */
export function box(x1: number, y1: number, x2: number, y2: number): Box {
  return { left: Math.min(x1, x2), top: Math.min(y1, y2), right: Math.max(x1, x2), bottom: Math.max(y1, y2) };
}

export function overlaps(a: Box, b: Box): boolean {
  return a.left < b.right && a.right > b.left && a.top < b.bottom && a.bottom > b.top;
}
