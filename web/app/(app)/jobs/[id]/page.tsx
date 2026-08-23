"use client";

import { useEffect, useRef } from "react";
import { useParams, useRouter } from "next/navigation";
import { Ban, Download, RotateCcw, Trash2 } from "lucide-react";
import { useJob, useJobLogs, useJobExecutions } from "@/hooks/use-data";
import { jobs as jobsApi } from "@/lib/api";
import { JobStateBadge } from "@/components/job-state-badge";
import { LifecycleTimeline } from "@/components/jobs/lifecycle-timeline";
import { JsonView } from "@/components/json-view";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { ErrorState, TableSkeleton } from "@/components/states";
import { reportError } from "@/lib/errors";
import { canCancel, canRetry, cancelHint, retryHint } from "@/lib/job-actions";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import {
  Collapsible, CollapsibleContent, CollapsibleTrigger,
} from "@/components/ui/collapsible";
import { fmtDate, fmtDuration, fmtRelative } from "@/lib/status";
import { cn } from "@/lib/utils";

const LEVEL_CLASS: Record<string, string> = {
  info: "text-muted-foreground",
  warn: "text-state-scheduled",
  warning: "text-state-scheduled",
  error: "text-destructive",
  debug: "text-muted-foreground/70",
};

export default function JobDetailPage() {
  const { id } = useParams<{ id: string }>();
  const router = useRouter();
  const { data: job, error, mutate } = useJob(id);
  const { data: logs } = useJobLogs(id);
  const { data: execs } = useJobExecutions(id);
  const logRef = useRef<HTMLDivElement>(null);

  const running = job?.status === "running" || job?.status === "claimed";

  useEffect(() => {
    if (running && logRef.current) logRef.current.scrollTop = logRef.current.scrollHeight;
  }, [logs, running]);

  function downloadLogs() {
    const text = (logs ?? [])
      .map((l) => `${l.logged_at}  ${l.level.toUpperCase()}  ${l.message}`)
      .join("\n");
    const url = URL.createObjectURL(new Blob([text], { type: "text/plain" }));
    const a = document.createElement("a");
    a.href = url;
    a.download = `job-${id}-logs.txt`;
    a.click();
    URL.revokeObjectURL(url);
  }

  if (error) return <ErrorState onRetry={() => mutate()} />;
  if (!job) return <TableSkeleton rows={6} cols={4} />;

  const meta: [string, React.ReactNode][] = [
    ["State", <JobStateBadge key="s" state={job.status} />],
    ["Attempt", `${job.attempt_count} / ${job.max_attempts}`],
    ["Priority", String(job.priority)],
    ["Queue", <Badge key="q" variant="outline" className="rounded-full text-[10px]">{job.queue_id.slice(0, 8)}</Badge>],
    ["Tags", job.tags?.length ? job.tags.join(", ") : "—"],
    ["Created", fmtRelative(job.created_at)],
  ];

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          <h1 className="truncate text-xl font-semibold tracking-tight">{job.type}</h1>
          <p className="font-mono text-xs text-muted-foreground">ID: {job.id}</p>
        </div>
        <div className="flex flex-wrap gap-2">
          <Tooltip>
            <TooltipTrigger
              render={
                <span>
                  <Button
                    size="sm" variant="outline" className="rounded-lg"
                    disabled={!canRetry(job.status)}
                    aria-label="Retry job"
                    onClick={async () => {
                      try { await jobsApi.retry(job.id); mutate(); }
                      catch (e) { reportError(e, "Failed to retry job"); }
                    }}
                  >
                    <RotateCcw className="size-3.5" aria-hidden="true" /> Retry
                  </Button>
                </span>
              }
            />
            <TooltipContent>{retryHint(job.status)}</TooltipContent>
          </Tooltip>

          {canCancel(job.status) ? (
            <ConfirmDialog
              title="Cancel this job?"
              description={`Job "${job.type}" (ID: ${job.id}) will stop being eligible for execution.`}
              confirmLabel="Cancel job"
              onConfirm={async () => {
                try { await jobsApi.cancel(job.id); mutate(); }
                catch (e) { reportError(e, "Failed to cancel job"); }
              }}
              trigger={
                <Button size="sm" variant="outline" className="rounded-lg" aria-label="Cancel job">
                  <Ban className="size-3.5" aria-hidden="true" /> Cancel
                </Button>
              }
            />
          ) : (
            <Tooltip>
              <TooltipTrigger
                render={
                  <span>
                    <Button size="sm" variant="outline" className="rounded-lg" disabled aria-label="Cancel job">
                      <Ban className="size-3.5" aria-hidden="true" /> Cancel
                    </Button>
                  </span>
                }
              />
              <TooltipContent>{cancelHint(job.status)}</TooltipContent>
            </Tooltip>
          )}

          <ConfirmDialog
            title="Delete this job?"
            description={`Job "${job.type}" (ID: ${job.id}) will be permanently deleted, along with its execution history and logs. This cannot be undone.`}
            confirmLabel="Delete"
            onConfirm={async () => {
              try { await jobsApi.remove(job.id); router.push("/jobs"); }
              catch (e) { reportError(e, "Failed to delete job"); }
            }}
            trigger={
              <Button size="sm" variant="destructive" className="rounded-lg" aria-label="Delete job">
                <Trash2 className="size-3.5" aria-hidden="true" /> Delete
              </Button>
            }
          />
        </div>
      </div>

      <div className="grid gap-6 lg:grid-cols-5">
        <div className="space-y-6 lg:col-span-3">
          <Card className="rounded-xl p-5">
            <dl className="grid grid-cols-2 gap-x-6 gap-y-4 sm:grid-cols-3">
              {meta.map(([label, value]) => (
                <div key={label}>
                  <dt className="text-[10px] uppercase tracking-wide text-muted-foreground">{label}</dt>
                  <dd className="mt-1 text-sm">{value}</dd>
                </div>
              ))}
            </dl>
          </Card>

          <Card className="rounded-xl p-5">
            <h2 className="mb-4 text-sm font-semibold tracking-tight">Lifecycle</h2>
            <LifecycleTimeline job={job} />
          </Card>

          {(execs?.length ?? 0) > 1 && (
            <Collapsible defaultOpen>
              <Card className="rounded-xl p-5">
                <CollapsibleTrigger
                  render={
                    <button className="flex w-full items-center justify-between text-sm font-semibold tracking-tight focus-visible:ring-2 focus-visible:ring-primary">
                      Retry History ({execs!.length} attempts)
                    </button>
                  }
                />
                <CollapsibleContent className="mt-3 space-y-2">
                  {execs!.map((ex) => (
                    <div key={ex.id} className="flex flex-wrap items-baseline gap-2 border-b border-border pb-2 text-xs last:border-0">
                      <span className="font-medium">Attempt {ex.attempt_number}</span>
                      <JobStateBadge state={ex.status} />
                      <span className="text-muted-foreground">{fmtDate(ex.started_at)}</span>
                      <span className="font-mono tabular-nums text-muted-foreground">{fmtDuration(ex.duration_ms)}</span>
                      {ex.error_message && (
                        <span className="w-full truncate font-mono text-destructive">{ex.error_message}</span>
                      )}
                    </div>
                  ))}
                </CollapsibleContent>
              </Card>
            </Collapsible>
          )}
        </div>

        <div className="space-y-6 lg:col-span-2">
          <JsonView title="Args" value={job.payload} />
          <JsonView
            title="Metadata"
            value={{
              idempotency_key: job.idempotency_key ?? null,
              batch_id: job.batch_id ?? null,
              cron_expression: job.cron_expression ?? null,
              timeout_secs: job.timeout_secs,
              run_at: job.run_at,
              last_error: job.last_error ?? null,
            }}
          />
        </div>
      </div>

      <Card className="rounded-xl p-5">
        <div className="mb-3 flex items-center justify-between">
          <h2 className="text-sm font-semibold tracking-tight">Execution Logs</h2>
          <Button size="sm" variant="outline" onClick={downloadLogs} className="rounded-lg" aria-label="Download logs">
            <Download className="size-3.5" aria-hidden="true" /> Download
          </Button>
        </div>
        <div ref={logRef} className="max-h-72 overflow-auto rounded-md bg-muted/40 p-3 font-mono text-xs scrollbar-thin">
          {(logs ?? []).length === 0 ? (
            <p className="text-muted-foreground">No logs recorded for this job.</p>
          ) : (
            logs!.map((l) => (
              <div key={l.id} className="flex gap-3 leading-5">
                <span className="shrink-0 text-muted-foreground/70">{l.logged_at}</span>
                <span className={cn("w-12 shrink-0 uppercase", LEVEL_CLASS[l.level.toLowerCase()] ?? "")}>{l.level}</span>
                <span className="min-w-0 break-all">{l.message}</span>
              </div>
            ))
          )}
        </div>
      </Card>
    </div>
  );
}
