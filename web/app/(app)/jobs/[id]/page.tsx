"use client";

import { useEffect, useRef } from "react";
import Link from "next/link";
import { useParams, useRouter } from "next/navigation";
import { Ban, Download, RotateCcw, Trash2 } from "lucide-react";
import { useJob, useJobLogs, useJobExecutions, useQueues } from "@/hooks/use-data";
import { jobs as jobsApi } from "@/lib/api";
import { useAuthStore } from "@/lib/auth-store";
import { JobStateBadge } from "@/components/job-state-badge";
import { LifecycleTimeline } from "@/components/jobs/lifecycle-timeline";
import { AttemptsList } from "@/components/jobs/attempts-list";
import { JsonView } from "@/components/json-view";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { ErrorState, TableSkeleton } from "@/components/states";
import { reportError } from "@/lib/errors";
import { canCancel, canRetry, cancelHint, retryHint } from "@/lib/job-actions";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { Button } from "@/components/ui/button";
import { fmtRelative } from "@/lib/status";
import { cn } from "@/lib/utils";

const LEVEL_CLASS: Record<string, string> = {
  info: "text-muted-foreground",
  warn: "text-state-scheduled",
  warning: "text-state-scheduled",
  error: "text-destructive",
  debug: "text-muted-foreground/70",
};

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="min-w-0">
      <dt className="text-sm font-medium text-foreground">{label}</dt>
      <dd className="mt-1.5 truncate text-sm text-muted-foreground">{children}</dd>
    </div>
  );
}

export default function JobDetailPage() {
  const { id } = useParams<{ id: string }>();
  const router = useRouter();
  const projectId = useAuthStore((s) => s.projectId);
  const { data: job, error, mutate } = useJob(id);
  const { data: logs } = useJobLogs(id);
  const { data: attempts } = useJobExecutions(id);
  const { data: queueList } = useQueues(projectId);
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

  const queueName = queueList?.find((q) => q.id === job.queue_id)?.name ?? job.queue_id;

  return (
    <div className="mx-auto max-w-[1400px]">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          <h1 className="truncate text-2xl font-semibold tracking-tight">{job.type}</h1>
          <p className="mt-1 font-mono text-sm text-muted-foreground">ID: {job.id}</p>
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
              <Button size="sm" variant="outline" className="rounded-lg text-destructive" aria-label="Delete job">
                <Trash2 className="size-3.5" aria-hidden="true" /> Delete
              </Button>
            }
          />
        </div>
      </div>

      <div className="mt-8 grid gap-8 border-t border-border pt-6 lg:grid-cols-2 lg:gap-12">
        <dl>
          <div className="grid grid-cols-3 gap-6 border-b border-border pb-5">
            <Field label="State">
              <JobStateBadge state={job.status} />
            </Field>
            <Field label="Attempt">
              <span className="font-mono tabular-nums">
                {job.attempt_count} / {job.max_attempts}
              </span>
            </Field>
            <Field label="Priority">
              <span className="font-mono tabular-nums">{job.priority}</span>
            </Field>
          </div>

          <div className="grid grid-cols-2 gap-6 border-b border-border py-5">
            <Field label="Queue">
              <Link
                href={`/queues/${job.queue_id}`}
                className="font-mono text-state-running hover:underline focus-visible:ring-2 focus-visible:ring-primary"
              >
                {queueName}
              </Link>
            </Field>
            <Field label="Tags">
              {job.tags?.length ? job.tags.join(", ") : "–"}
            </Field>
          </div>

          <div className="grid grid-cols-2 gap-6 py-5">
            <Field label="Created">{fmtRelative(job.created_at)}</Field>
            <Field label="Batch">
              {job.batch_id ? (
                <Link
                  href={`/jobs/batch/${job.batch_id}`}
                  className="font-mono text-state-running hover:underline focus-visible:ring-2 focus-visible:ring-primary"
                >
                  {job.batch_id}
                </Link>
              ) : (
                "–"
              )}
            </Field>
          </div>
        </dl>

        <LifecycleTimeline job={job} />
      </div>

      <div className="mt-8 grid gap-8 border-t border-border pt-6 lg:grid-cols-2 lg:gap-12">
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

      <section className="mt-8 border-t border-border pt-6">
        <h2 className="mb-4 text-sm font-semibold tracking-tight">Attempts</h2>
        <AttemptsList attempts={attempts ?? []} />
      </section>

      <section className="mt-8 border-t border-border pt-6">
        <div className="mb-4 flex items-center justify-between">
          <h2 className="text-sm font-semibold tracking-tight">Execution Logs</h2>
          <Button size="sm" variant="outline" onClick={downloadLogs} className="rounded-lg" aria-label="Download logs">
            <Download className="size-3.5" aria-hidden="true" /> Download
          </Button>
        </div>
        <div
          ref={logRef}
          className="max-h-72 overflow-auto rounded-lg border border-border p-3 font-mono text-xs scrollbar-thin"
        >
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
      </section>
    </div>
  );
}
