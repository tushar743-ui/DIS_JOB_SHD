"use client";

import { motion } from "framer-motion";
import {
  Ban, CalendarClock, Check, Database, Play, Timer, X, type LucideIcon,
} from "lucide-react";
import { formatElapsed, useElapsedTime } from "@/hooks/use-elapsed-time";
import { fmtRelative } from "@/lib/status";
import { cn } from "@/lib/utils";
import type { Job } from "@/lib/api";

interface Step {
  key: string;
  label: string;
  Icon: LucideIcon;
  token: string;
  at?: string | null;
  detail?: string;
  durationOnly?: boolean;
  active?: boolean;
}

function buildSteps(job: Job, runningFor: string): Step[] {
  const steps: Step[] = [
    { key: "created", label: "Created", Icon: Database, token: "--state-queued", at: job.created_at },
  ];

  if (job.scheduled_at) {
    steps.push({
      key: "scheduled", label: "Scheduled", Icon: CalendarClock,
      token: "--state-scheduled", at: job.scheduled_at,
    });
  }

  const waited =
    job.updated_at && job.created_at
      ? Math.max(0, Math.floor((new Date(job.updated_at).getTime() - new Date(job.created_at).getTime()) / 1000))
      : 0;

  steps.push({
    key: "wait", label: "Wait", Icon: Timer, token: "--state-queued",
    at: job.run_at, detail: formatElapsed(waited), durationOnly: true,
  });

  const started = job.status !== "queued" && job.status !== "scheduled";
  if (started) {
    const live = job.status === "running" || job.status === "claimed";
    steps.push({
      key: "running", label: "Running", Icon: Play, token: "--state-running",
      at: job.updated_at, detail: runningFor, active: live,
    });
  }

  if (job.status === "completed") {
    steps.push({
      key: "complete", label: "Complete", Icon: Check,
      token: "--state-completed", at: job.completed_at ?? job.updated_at,
    });
  } else if (job.status === "cancelled") {
    steps.push({
      key: "cancelled", label: "Cancelled", Icon: Ban,
      token: "--state-cancelled", at: job.completed_at ?? job.updated_at,
    });
  } else if (job.status === "failed" || job.status === "dead") {
    steps.push({
      key: "failed", label: job.status === "dead" ? "Discarded" : "Failed", Icon: X,
      token: job.status === "dead" ? "--state-dlq" : "--state-failed",
      at: job.completed_at ?? job.updated_at,
    });
  }

  return steps;
}

export function LifecycleTimeline({ job }: { job: Job }) {
  const live = job.status === "running" || job.status === "claimed";
  const runningFor = useElapsedTime(job.updated_at, live ? null : job.completed_at);
  const steps = buildSteps(job, runningFor);

  return (
    <ol className="relative">
      {steps.map((step, i) => {
        const color = `hsl(var(${step.token}))`;
        const timestamp = step.durationOnly ? "" : fmtRelative(step.at);
        return (
          <motion.li
            key={step.key}
            initial={{ opacity: 0, y: 6 }}
            animate={{ opacity: 1, y: 0 }}
            transition={{ duration: 0.2, delay: i * 0.08 }}
            className="flex gap-3 pb-4 last:pb-0"
          >
            <div className="flex flex-col items-center">
              <span
                className={cn(
                  "grid size-7 shrink-0 place-items-center rounded-full text-white",
                  step.active && "animate-pulse"
                )}
                style={{ background: color }}
              >
                <step.Icon className="size-3.5" strokeWidth={2.5} aria-hidden="true" />
              </span>
              {i < steps.length - 1 && (
                <span className="w-px flex-1" style={{ background: `${color}` , opacity: 0.35 }} />
              )}
            </div>

            <div className="min-w-0 flex-1 pb-1">
              <p className="text-sm font-medium leading-tight" style={{ color }}>
                {step.label}
              </p>
              <p className="mt-0.5 text-xs text-muted-foreground">
                {timestamp}
                {step.detail && (
                  <span className="font-mono tabular-nums">
                    {timestamp ? " " : ""}({step.detail})
                  </span>
                )}
              </p>
            </div>
          </motion.li>
        );
      })}
    </ol>
  );
}
