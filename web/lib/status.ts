export const STATUS_COLOR: Record<string, string> = {
  queued:    "bg-amber-500/15 text-amber-400 border-amber-500/20",
  scheduled: "bg-purple-500/15 text-purple-400 border-purple-500/20",
  claimed:   "bg-sky-500/15 text-sky-400 border-sky-500/20",
  running:   "bg-blue-500/15 text-blue-400 border-blue-500/20",
  completed: "bg-emerald-500/15 text-emerald-400 border-emerald-500/20",
  failed:    "bg-red-500/15 text-red-400 border-red-500/20",
  cancelled: "bg-zinc-500/15 text-zinc-400 border-zinc-500/20",
  dead:      "bg-rose-900/30 text-rose-400 border-rose-900/40",
  active:    "bg-emerald-500/15 text-emerald-400 border-emerald-500/20",
  draining:  "bg-amber-500/15 text-amber-400 border-amber-500/20",
  offline:   "bg-zinc-500/15 text-zinc-400 border-zinc-500/20",
};

export const STATUS_DOT: Record<string, string> = {
  queued:    "bg-amber-400",
  scheduled: "bg-purple-400",
  claimed:   "bg-sky-400",
  running:   "bg-blue-400 animate-pulse",
  completed: "bg-emerald-400",
  failed:    "bg-red-400",
  cancelled: "bg-zinc-400",
  dead:      "bg-rose-400",
  active:    "bg-emerald-400",
  draining:  "bg-amber-400 animate-pulse",
  offline:   "bg-zinc-400",
};

export function fmtDuration(ms?: number | null): string {
  if (ms == null) return "–";
  // Averages come back from the API as floats, so round rather than printing
  // every digit of e.g. 30.402843601895736.
  if (ms < 1000) return `${Math.round(ms)}ms`;
  if (ms < 60000) return `${(ms / 1000).toFixed(1)}s`;
  return `${(ms / 60000).toFixed(1)}m`;
}

export function fmtDate(iso?: string | null): string {
  if (!iso) return "–";
  return new Intl.DateTimeFormat("en-US", {
    month: "short", day: "numeric",
    hour: "2-digit", minute: "2-digit", second: "2-digit",
    hour12: false,
  }).format(new Date(iso));
}

export function fmtRelative(iso?: string | null): string {
  if (!iso) return "–";
  const diff = Date.now() - new Date(iso).getTime();
  if (diff < 60_000) return `${Math.round(diff / 1000)}s ago`;
  if (diff < 3_600_000) return `${Math.round(diff / 60_000)}m ago`;
  if (diff < 86_400_000) return `${Math.round(diff / 3_600_000)}h ago`;
  return `${Math.round(diff / 86_400_000)}d ago`;
}
