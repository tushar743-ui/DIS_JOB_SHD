"use client";

import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import type { LiveStatus } from "@/hooks/use-live-events";

const CONFIG: Record<LiveStatus, { label: string; dot: string; hint: string }> = {
  live: {
    label: "Live",
    dot: "bg-emerald-400 animate-pulse",
    hint: "Connected — updates arrive over WebSocket as they happen.",
  },
  connecting: {
    label: "Connecting",
    dot: "bg-amber-400 animate-pulse",
    hint: "Opening the live connection…",
  },
  reconnecting: {
    label: "Polling",
    dot: "bg-amber-400 animate-pulse",
    hint: "Live connection dropped — falling back to periodic polling while it retries.",
  },
  offline: {
    label: "Polling",
    dot: "bg-zinc-400",
    hint: "No live connection — data refreshes on a timer instead.",
  },
};

export function LiveIndicator({ status }: { status: LiveStatus }) {
  const c = CONFIG[status];
  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <span
            role="status"
            aria-label={`Live updates: ${c.label}`}
            className="flex items-center gap-1.5 rounded-full border border-border px-2.5 py-1 text-[11px] font-medium text-muted-foreground"
          >
            <span className={`size-1.5 shrink-0 rounded-full ${c.dot}`} aria-hidden="true" />
            {c.label}
          </span>
        }
      />
      <TooltipContent className="max-w-56 text-xs">{c.hint}</TooltipContent>
    </Tooltip>
  );
}
