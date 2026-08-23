"use client";

import { motion } from "framer-motion";
// Per-icon imports, not the package barrel: the root export pulls ~1500 icon
// modules. This is the import pattern Next's own docs prescribe for this package.
import { ClockIcon } from "@phosphor-icons/react/dist/csr/Clock";
import { CalendarDotsIcon } from "@phosphor-icons/react/dist/csr/CalendarDots";
import { CircleNotchIcon } from "@phosphor-icons/react/dist/csr/CircleNotch";
import { CheckCircleIcon } from "@phosphor-icons/react/dist/csr/CheckCircle";
import { XCircleIcon } from "@phosphor-icons/react/dist/csr/XCircle";
import { ArrowsClockwiseIcon } from "@phosphor-icons/react/dist/csr/ArrowsClockwise";
import { ProhibitIcon } from "@phosphor-icons/react/dist/csr/Prohibit";
import { WarningOctagonIcon } from "@phosphor-icons/react/dist/csr/WarningOctagon";
import { QuestionIcon } from "@phosphor-icons/react/dist/csr/Question";
import type { Icon as PhosphorIcon, IconWeight } from "@phosphor-icons/react/dist/lib/types";
import { cn } from "@/lib/utils";

export type JobState =
  | "queued" | "scheduled" | "claimed" | "running"
  | "completed" | "failed" | "cancelled" | "dead" | "retrying";

interface StateSpec {
  label: string;
  token: string;
  Icon: PhosphorIcon;
  weight: IconWeight;
}

export const STATE_SPEC: Record<string, StateSpec> = {
  queued: { label: "Queued", token: "--state-queued", Icon: ClockIcon, weight: "fill" },
  scheduled: { label: "Scheduled", token: "--state-scheduled", Icon: CalendarDotsIcon, weight: "fill" },
  claimed: { label: "Claimed", token: "--state-running", Icon: CircleNotchIcon, weight: "bold" },
  running: { label: "Running", token: "--state-running", Icon: CircleNotchIcon, weight: "bold" },
  completed: { label: "Completed", token: "--state-completed", Icon: CheckCircleIcon, weight: "fill" },
  failed: { label: "Failed", token: "--state-failed", Icon: XCircleIcon, weight: "fill" },
  retrying: { label: "Retrying", token: "--state-retrying", Icon: ArrowsClockwiseIcon, weight: "bold" },
  cancelled: { label: "Cancelled", token: "--state-cancelled", Icon: ProhibitIcon, weight: "fill" },
  dead: { label: "Dead", token: "--state-dlq", Icon: WarningOctagonIcon, weight: "fill" },
};

export function stateSpec(state: string): StateSpec {
  return (
    STATE_SPEC[state] ?? {
      label: state || "Unknown",
      token: "--state-cancelled",
      Icon: QuestionIcon,
      weight: "fill" as IconWeight,
    }
  );
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
  const { label, token, Icon, weight } = stateSpec(state);
  const spinning = state === "running" || state === "claimed";
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
        "inline-flex items-center gap-1.5 rounded-md px-2 py-0.5 text-xs font-medium",
        className
      )}
      style={{
        color: `var(${token})`,
        backgroundColor: `color-mix(in oklab, var(${token}) 12%, transparent)`,
        boxShadow: `inset 0 0 0 1px color-mix(in oklab, var(${token}) 28%, transparent)`,
      }}
    >
      <Icon
        className={cn("size-3.5 shrink-0", spinning && "animate-spin")}
        weight={weight}
        aria-hidden="true"
      />
      {label}
    </motion.span>
  );
}
