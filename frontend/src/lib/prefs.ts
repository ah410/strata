import { useSyncExternalStore } from "react";

/** Per-device settings, kept in localStorage like the layout choice. */
export interface Prefs {
  /** Grid cards show a peek of documents instead of their icon. */
  previews: boolean;
}

const KEY = "strata:prefs";
const DEFAULTS: Prefs = { previews: false };
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
