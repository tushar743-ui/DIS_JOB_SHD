"use client";

import { Ban, CheckCircle2, Loader, Monitor, XCircle, type LucideIcon } from "lucide-react";
import { fmtDuration, fmtRelative } from "@/lib/status";
import { stateSpec } from "@/components/job-state-badge";
import type { JobExecution } from "@/lib/api";

const ICON: Record<string, LucideIcon> = {
  completed: CheckCircle2,
  failed: XCircle,
  dead: XCircle,
  cancelled: Ban,
  running: Loader,
  claimed: Loader,
};

export function AttemptsList({ attempts }: { attempts: JobExecution[] }) {
  if (attempts.length === 0) {
    return <p className="text-sm text-muted-foreground">This job has not been attempted yet.</p>;
  }

  return (
    <ul className="divide-y divide-border">
      {attempts.map((attempt) => {
        const { label, token } = stateSpec(attempt.status);
        const Icon = ICON[attempt.status] ?? CheckCircle2;
        const live = attempt.status === "running" || attempt.status === "claimed";

        return (
          <li key={attempt.id} className="flex items-start gap-3 py-3 first:pt-0 last:pb-0">
            <Icon
              className={`mt-0.5 size-4 shrink-0 ${live ? "animate-spin" : ""}`}
              style={{ color: `var(${token})` }}
              aria-hidden="true"
            />

            <div className="min-w-0 flex-1">
              <p className="text-sm">
                <span className="font-medium">{label}</span>{" "}
                <span className="text-muted-foreground">(Attempt {attempt.attempt_number})</span>
              </p>
              <p className="mt-1 flex items-center gap-1.5 font-mono text-xs text-muted-foreground">
                <Monitor className="size-3.5 shrink-0" aria-hidden="true" />
                <span className="truncate">{attempt.worker_id ?? attempt.id}</span>
              </p>
              {attempt.error_message && (
                <p className="mt-2 rounded-md bg-destructive/10 px-2.5 py-1.5 font-mono text-xs break-all text-destructive">
                  {attempt.error_message}
                </p>
              )}
            </div>

            <div className="flex shrink-0 items-baseline gap-3 text-xs text-muted-foreground">
              <span className="font-mono tabular-nums">{fmtDuration(attempt.duration_ms)}</span>
              <span>{fmtRelative(attempt.completed_at ?? attempt.started_at)}</span>
            </div>
          </li>
        );
      })}
    </ul>
  );
}
