import { useSyncExternalStore } from "react";

/** Per-device settings, kept in localStorage like the layout choice. */
export interface Prefs {
  /** Grid cards show a peek of documents instead of their icon. */
  previews: boolean;
  /** Index into CARD_SIZES for grid view. */
  cardSize: number;
}

/** Grid card sizes, smallest first: minimum card width and how much the art scales. */
export const CARD_SIZES = [
  { label: "Small", px: 120, scale: 0.85 },
  { label: "Medium", px: 148, scale: 1 },
  { label: "Large", px: 200, scale: 1.35 },
  { label: "Extra large", px: 264, scale: 1.75 },
];

const KEY = "strata:prefs";
const DEFAULTS: Prefs = { previews: false, cardSize: 1 };
const listeners = new Set<() => void>();

function load(): Prefs {
  try {
    return { ...DEFAULTS, ...JSON.parse(localStorage.getItem(KEY) ?? "{}") };
  } catch {
    return { ...DEFAULTS };
  }
}

let current = load();

export function getPrefs(): Prefs {
  return current;
}

export function setPref<K extends keyof Prefs>(key: K, value: Prefs[K]) {
  current = { ...current, [key]: value };
  try { localStorage.setItem(KEY, JSON.stringify(current)); } catch { /* per-device nicety only */ }
  listeners.forEach((l) => l());
}

export function usePrefs(): Prefs {
  return useSyncExternalStore(
    (l) => { listeners.add(l); return () => listeners.delete(l); },
    getPrefs,
  );
}
