"use client";

import { motion } from "framer-motion";
import { CalendarClock, CheckCircle2, Clock, Play, Timer, XCircle, type LucideIcon } from "lucide-react";
import { BorderBeam } from "@/components/ui/border-beam";
import { formatElapsed, useElapsedTime } from "@/hooks/use-elapsed-time";
import { fmtRelative } from "@/lib/status";
import type { Job } from "@/lib/api";

interface Step {
  key: string;
  label: string;
  Icon: LucideIcon;
  token: string;
  at?: string | null;
  detail?: string;
  active?: boolean;
}

function buildSteps(job: Job, runningFor: string): Step[] {
  const steps: Step[] = [
    { key: "created", label: "Created", Icon: Clock, token: "--state-queued", at: job.created_at },
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
    at: job.run_at, detail: formatElapsed(waited),
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
      key: "complete", label: "Complete", Icon: CheckCircle2,
      token: "--state-completed", at: job.completed_at ?? job.updated_at,
    });
  } else if (job.status === "failed" || job.status === "dead" || job.status === "cancelled") {
    steps.push({
      key: "failed", label: job.status === "cancelled" ? "Cancelled" : "Failed", Icon: XCircle,
      token: job.status === "dead" ? "--state-dlq" : "--state-failed",
      at: job.completed_at ?? job.updated_at, detail: job.last_error ?? undefined,
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
      {steps.map((step, i) => (
        <motion.li
          key={step.key}
          initial={{ opacity: 0, y: 6 }}
          animate={{ opacity: 1, y: 0 }}
          transition={{ duration: 0.2, delay: i * 0.1 }}
          className="flex gap-3 pb-5 last:pb-0"
        >
          <div className="relative flex flex-col items-center">
            <span
              className="relative grid size-8 shrink-0 place-items-center overflow-hidden rounded-full"
              style={{ background: `hsl(var(${step.token}) / 0.15)`, color: `hsl(var(${step.token}))` }}
            >
              <step.Icon className="size-4" aria-hidden="true" />
              {step.active && <BorderBeam size={28} duration={3} colorFrom={`hsl(var(${step.token}))`} colorTo="transparent" />}
            </span>
            {i < steps.length - 1 && <span className="mt-1 w-0 flex-1 border-l-2 border-dashed border-border" />}
          </div>

          <div className="min-w-0 flex-1 pt-1">
            <div className="flex flex-wrap items-baseline gap-2">
              <p className="text-sm font-medium tracking-tight">{step.label}</p>
              <span className="text-[11px] text-muted-foreground">{fmtRelative(step.at)}</span>
              {step.detail && (
                <span className="font-mono text-[11px] tabular-nums text-muted-foreground">({step.detail})</span>
              )}
            </div>
          </div>
        </motion.li>
      ))}
    </ol>
  );
}
