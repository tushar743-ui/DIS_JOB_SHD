"use client";

import { motion } from "framer-motion";
import {
  Clock, CalendarClock, Loader, CheckCircle2, XCircle,
  RotateCcw, Ban, Skull, HelpCircle, type LucideIcon,
} from "lucide-react";
import { cn } from "@/lib/utils";

export type JobState =
  | "queued" | "scheduled" | "claimed" | "running"
  | "completed" | "failed" | "cancelled" | "dead" | "retrying";

interface StateSpec {
  label: string;
  token: string;
  Icon: LucideIcon;
}

export const STATE_SPEC: Record<string, StateSpec> = {
  queued: { label: "Queued", token: "--state-queued", Icon: Clock },
  scheduled: { label: "Scheduled", token: "--state-scheduled", Icon: CalendarClock },
  claimed: { label: "Claimed", token: "--state-running", Icon: Loader },
  running: { label: "Running", token: "--state-running", Icon: Loader },
  completed: { label: "Completed", token: "--state-completed", Icon: CheckCircle2 },
  failed: { label: "Failed", token: "--state-failed", Icon: XCircle },
  retrying: { label: "Retrying", token: "--state-retrying", Icon: RotateCcw },
  cancelled: { label: "Cancelled", token: "--state-cancelled", Icon: Ban },
  dead: { label: "Dead", token: "--state-dlq", Icon: Skull },
};

export function stateSpec(state: string): StateSpec {
  return STATE_SPEC[state] ?? { label: state || "Unknown", token: "--state-cancelled", Icon: HelpCircle };
}

export function StateDot({ state, className }: { state: string; className?: string }) {
  const { token } = stateSpec(state);
  const live = state === "running" || state === "claimed";
  return (
    <span
      className={cn("relative inline-flex size-2 shrink-0", className)}
      style={{ color: `var(${token})` }}
      aria-hidden="true"
    >
      {live && (
        <span className="absolute inline-flex size-full animate-ping rounded-full bg-current opacity-70" />
      )}
      <span className="relative inline-flex size-2 rounded-full bg-current" />
    </span>
  );
}

export function JobStateBadge({ state, className }: { state: string; className?: string }) {
  const { label, token, Icon } = stateSpec(state);
  return (
    <motion.span
      key={state}
      initial={{ opacity: 0 }}
      animate={{ opacity: 1 }}
      transition={{ duration: 0.2 }}
      role="status"
      aria-live="polite"
      aria-label={`State: ${label}`}
      className={cn(
        "inline-flex items-center gap-1.5 rounded-full border border-l-2 bg-transparent px-2.5 py-0.5 text-xs font-medium",
        className
      )}
      style={{ borderLeftColor: `var(${token})`, color: `var(${token})` }}
    >
      <Icon className={cn("size-3", state === "running" && "animate-spin")} aria-hidden="true" />
      {label}
    </motion.span>
  );
}
