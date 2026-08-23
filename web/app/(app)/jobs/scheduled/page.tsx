"use client";

import { useMemo } from "react";
import { useRouter } from "next/navigation";
import { CalendarClock, MoreVertical } from "lucide-react";
import { useAuthStore } from "@/lib/auth-store";
import { useAllJobs } from "@/hooks/use-data";
import { jobs as jobsApi } from "@/lib/api";
import { JobStateBadge } from "@/components/job-state-badge";
import { useCountdown } from "@/hooks/use-elapsed-time";
import { EmptyState, ErrorState, TableSkeleton } from "@/components/states";
import { reportError } from "@/lib/errors";
import { canCancel } from "@/lib/job-actions";
import { Badge } from "@/components/ui/badge";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import {
  DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { fmtDate, fmtRelative } from "@/lib/status";
import type { JobRow } from "@/hooks/use-data";

function describeCron(expr: string): string {
  const known: Record<string, string> = {
    "0 */6 * * *": "Every 6 hours",
    "0 9 * * 1-5": "Weekdays at 09:00",
    "*/5 * * * *": "Every 5 minutes",
    "0 0 * * *": "Daily at midnight",
    "0 * * * *": "Hourly",
  };
  return known[expr] ?? "Custom schedule";
}

function NextRunCell({ job }: { job: JobRow }) {
  const text = useCountdown(job.scheduled_at ?? job.run_at);
  return <span className="font-mono text-xs tabular-nums">{text}</span>;
}

export default function ScheduledJobsPage() {
  const router = useRouter();
  const projectId = useAuthStore((s) => s.projectId);
  const { data, error, isLoading, mutate } = useAllJobs(projectId);

  const rows = useMemo(
    () => (data ?? []).filter((j) => j.cron_expression || j.status === "scheduled" || j.scheduled_at),
    [data]
  );

  if (error) return <ErrorState onRetry={() => mutate()} />;
  if (isLoading && !data) return <TableSkeleton rows={5} cols={6} />;
  if (!rows.length) {
    return (
      <EmptyState
        icon={CalendarClock}
        title="No scheduled jobs"
        description="Delayed and cron jobs will appear here once you enqueue one with a run time."
      />
    );
  }

  return (
    <div className="overflow-hidden rounded-xl border border-border">
      <div className="overflow-x-auto">
        <table className="w-full text-sm">
          <thead>
            <tr className="border-b border-border bg-muted/50 text-[10px] uppercase tracking-wide text-muted-foreground">
              <th className="h-9 px-3 text-left font-medium">Kind</th>
              <th className="h-9 px-3 text-left font-medium">Schedule</th>
              <th className="h-9 px-3 text-left font-medium">Next Run</th>
              <th className="h-9 px-3 text-left font-medium">Queue</th>
              <th className="h-9 px-3 text-left font-medium">Created</th>
              <th className="h-9 px-3 text-left font-medium">Status</th>
              <th className="h-9 w-10 px-3" />
            </tr>
          </thead>
          <tbody>
            {rows.map((j) => (
              <tr
                key={j.id}
                onClick={() => router.push(`/jobs/${j.id}`)}
                className="h-12 cursor-pointer border-b border-border transition-colors last:border-0 hover:bg-accent/40"
              >
                <td className="px-3 font-medium">{j.type}</td>
                <td className="px-3">
                  {j.cron_expression ? (
                    <Tooltip>
                      <TooltipTrigger render={<span className="font-mono text-xs">{j.cron_expression}</span>} />
                      <TooltipContent>{describeCron(j.cron_expression)}</TooltipContent>
                    </Tooltip>
                  ) : (
                    <span className="text-xs text-muted-foreground">one-time</span>
                  )}
                </td>
                <td className="px-3"><NextRunCell job={j} /></td>
                <td className="px-3">
                  <Badge variant="outline" className="rounded-full text-[10px]">{j.queue_name}</Badge>
                </td>
                <td className="px-3 text-xs text-muted-foreground">
                  <Tooltip>
                    <TooltipTrigger render={<span>{fmtRelative(j.created_at)}</span>} />
                    <TooltipContent className="font-mono text-xs">{fmtDate(j.created_at)}</TooltipContent>
                  </Tooltip>
                </td>
                <td className="px-3"><JobStateBadge state={j.status} /></td>
                <td className="px-3">
                  <DropdownMenu>
                    <DropdownMenuTrigger
                      render={
                        <button
                          aria-label={`Actions for ${j.type}`}
                          onClick={(e) => e.stopPropagation()}
                          className="grid size-7 place-items-center rounded-md text-muted-foreground hover:bg-accent hover:text-foreground focus-visible:ring-2 focus-visible:ring-primary"
                        >
                          <MoreVertical className="size-3.5" />
                        </button>
                      }
                    />
                    <DropdownMenuContent align="end">
                      <DropdownMenuItem onClick={() => router.push(`/jobs/${j.id}`)}>View details</DropdownMenuItem>
                      <DropdownMenuItem
                        disabled={!canCancel(j.status)}
                        onClick={async () => {
                          try { await jobsApi.cancel(j.id); mutate(); }
                          catch (e) { reportError(e, "Failed to cancel job"); }
                        }}
                      >
                        Cancel
                      </DropdownMenuItem>
                      <DropdownMenuItem
                        onClick={async () => {
                          try { await jobsApi.remove(j.id); mutate(); }
                          catch (e) { reportError(e, "Failed to delete job"); }
                        }}
                      >
                        Delete
                      </DropdownMenuItem>
                    </DropdownMenuContent>
                  </DropdownMenu>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}
