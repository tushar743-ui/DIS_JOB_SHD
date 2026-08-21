import { cn } from "@/lib/utils";
import { STATUS_COLOR, STATUS_DOT } from "@/lib/status";

export function StatusBadge({ status }: { status: string }) {
  return (
    <span className={cn(
      "inline-flex items-center gap-1.5 px-2 py-0.5 rounded-md text-xs font-medium border font-mono",
      STATUS_COLOR[status] ?? "bg-zinc-500/15 text-zinc-400 border-zinc-500/20"
    )}>
      <span className={cn("w-1.5 h-1.5 rounded-full", STATUS_DOT[status] ?? "bg-zinc-400")} />
      {status}
    </span>
  );
}
