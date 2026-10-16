import type { Kind } from "../lib/api";

const COLOR: Record<Kind, string> = {
  folder: "var(--k-folder)",
  sheet: "var(--k-sheet)",
  csv: "var(--k-sheet)",
  image: "var(--k-image)",
  pdf: "var(--k-pdf)",
  video: "var(--k-video)",
  audio: "var(--k-audio)",
  text: "var(--k-text)",
  archive: "var(--k-archive)",
  doc: "var(--k-doc)",
  slides: "var(--k-slides)",
  file: "var(--k-file)",
};

/**
 * One 16px mark per file kind. Shape distinguishes the kind for anyone who
 * cannot separate the colours; colour makes the listing scannable at speed.
 */
export function Glyph({ kind }: { kind: Kind }) {
  const c = COLOR[kind] ?? COLOR.file;
  const common = {
    width: 15,
    height: 15,
    viewBox: "0 0 16 16",
    fill: "none",
    stroke: c,
    strokeWidth: 1.3,
    strokeLinecap: "round" as const,
    strokeLinejoin: "round" as const,
  };

  switch (kind) {
    case "folder":
      return (
        <svg {...common} aria-hidden="true">
          <path d="M1.8 12.6V3.9a.8.8 0 0 1 .8-.8h3.1l1.4 1.7h5.1a.8.8 0 0 1 .8.8v7a.8.8 0 0 1-.8.8H2.6a.8.8 0 0 1-.8-.8Z" />
        </svg>
      );
    case "sheet":
    case "csv":
      return (
        <svg {...common} aria-hidden="true">
          <rect x="2.2" y="2.2" width="11.6" height="11.6" rx="1.1" />
          <path d="M2.2 6.3h11.6M6.3 6.3v7.5M2.2 10h11.6" />
        </svg>
      );
    case "image":
      return (
        <svg {...common} aria-hidden="true">
          <rect x="2.2" y="2.9" width="11.6" height="10.2" rx="1.1" />
          <circle cx="5.9" cy="6.4" r="1.1" />
          <path d="m2.9 11.4 3.2-3 2.5 2.3 2.2-1.9 2.5 2.2" />
        </svg>
      );
    case "pdf":
      return (
        <svg {...common} aria-hidden="true">
          <path d="M9 1.9H4.3a1 1 0 0 0-1 1v10.2a1 1 0 0 0 1 1h7.4a1 1 0 0 0 1-1V5.4Z" />
          <path d="M9 1.9v3.5h3.7" />
        </svg>
      );
    case "video":
      return (
        <svg {...common} aria-hidden="true">
          <rect x="1.9" y="3.4" width="9" height="9.2" rx="1.1" />
          <path d="m11.3 9 2.8 2V5l-2.8 2.1z" />
        </svg>
      );
    case "audio":
      return (
        <svg {...common} aria-hidden="true">
          <path d="M6.2 11.3V3.8l6.3-1.3v7.4" />
          <circle cx="4.5" cy="11.6" r="1.8" />
          <circle cx="10.8" cy="10.2" r="1.8" />
        </svg>
      );
    case "archive":
      return (
        <svg {...common} aria-hidden="true">
          <rect x="2.2" y="2.4" width="11.6" height="11.2" rx="1.1" />
          <path d="M6.6 2.4v3.1M9.4 2.4v3.1M6.6 7.3h2.8M6.6 9.6h2.8" />
        </svg>
      );
    case "doc":
    case "slides":
    case "text":
      return (
        <svg {...common} aria-hidden="true">
          <path d="M9 1.9H4.3a1 1 0 0 0-1 1v10.2a1 1 0 0 0 1 1h7.4a1 1 0 0 0 1-1V5.4Z" />
          <path d="M9 1.9v3.5h3.7M5.6 8.4h4.8M5.6 10.8h3.2" />
        </svg>
      );
    default:
      return (
        <svg {...common} aria-hidden="true">
          <path d="M9 1.9H4.3a1 1 0 0 0-1 1v10.2a1 1 0 0 0 1 1h7.4a1 1 0 0 0 1-1V5.4Z" />
          <path d="M9 1.9v3.5h3.7" />
        </svg>
      );
  }
}

export function kindColor(kind: Kind): string {
  return COLOR[kind] ?? COLOR.file;
}
