// relativeTime formats an ISO timestamp as a short "x ago" string. Minimal by design —
// no date library (UI-SPEC: no new dependencies). Returns "" for an empty/invalid input.
// Shared by SeriesDetail and the per-issue timeline (OBS-01).
export function relativeTime(iso: string): string {
  if (!iso) return "";
  const then = Date.parse(iso);
  if (Number.isNaN(then)) return "";
  const seconds = Math.max(0, Math.round((Date.now() - then) / 1000));
  if (seconds < 60) return "just now";
  const minutes = Math.round(seconds / 60);
  if (minutes < 60) return `${minutes}m ago`;
  const hours = Math.round(minutes / 60);
  if (hours < 24) return `${hours}h ago`;
  const days = Math.round(hours / 24);
  return `${days}d ago`;
}
