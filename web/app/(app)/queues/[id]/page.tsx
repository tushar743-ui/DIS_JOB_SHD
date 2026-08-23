"use client";

import Link from "next/link";
import { useParams, useRouter } from "next/navigation";
import { motion } from "framer-motion";
import { ChevronLeft, PauseCircle, PlayCircle, RotateCcw, Settings2, Trash2 } from "lucide-react";
import { useAuthStore } from "@/lib/auth-store";
import { useQueue, useQueueStats, useQueueMetrics } from "@/hooks/use-data";
import { queues as queuesApi, jobs as jobsApi, dlq as dlqApi } from "@/lib/api";
import useSWR from "swr";
import { JobStateBadge, StateDot } from "@/components/job-state-badge";
import { QueueConfigSheet } from "@/components/queues/queue-config-sheet";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { ErrorState, TableSkeleton } from "@/components/states";
import { reportError } from "@/lib/errors";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { fmtDuration, fmtRelative } from "@/lib/status";

export default function QueueDetailPage() {
  const { id } = useParams<{ id: string }>();
  const router = useRouter();
  const projectId = useAuthStore((s) => s.projectId);

  const { data: queue, error, mutate } = useQueue(id);
  const { data: stats } = useQueueStats(id);
  const { data: metrics } = useQueueMetrics(id);
  const { data: jobPage, mutate: refetchJobs } = useSWR(
    id ? ["queue-jobs", id] : null,
    () => jobsApi.list(id, { limit: 50 }),
    { refreshInterval: 2000 }
  );
  const { data: dlqPage, mutate: refetchDlq } = useSWR(
    id ? ["queue-dlq", id] : null,
    () => dlqApi.list(id, { limit: 20 }),
    { refreshInterval: 5000 }
  );

  if (error) return <ErrorState onRetry={() => mutate()} />;
  if (!queue) return <TableSkeleton rows={5} cols={4} />;

  const by = stats?.by_status ?? {};
  const cards = [
    { label: "Queued", value: by.queued ?? 0, token: "--state-queued" },
    { label: "Running", value: by.running ?? 0, token: "--state-running" },
    { label: "Completed", value: by.completed ?? 0, token: "--state-completed" },
    { label: "Failed", value: (by.failed ?? 0) + (by.dead ?? 0), token: "--state-failed" },
  ];

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          <Link href="/queues" className="mb-1 inline-flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground">
            <ChevronLeft className="size-3" aria-hidden="true" /> Queues
          </Link>
          <h1 className="flex items-center gap-2 truncate text-xl font-semibold tracking-tight">
            {queue.name}
            {queue.paused && <Badge variant="outline" className="rounded-full text-[10px] text-state-scheduled">PAUSED</Badge>}
          </h1>
          <p className="font-mono text-xs text-muted-foreground">{queue.id}</p>
        </div>
        <div className="flex flex-wrap gap-2">
          <QueueConfigSheet
            queue={queue}
            projectId={projectId}
            onSaved={() => mutate()}
            trigger={
              <Button size="sm" variant="outline" className="rounded-lg" aria-label="Configure queue">
                <Settings2 className="size-3.5" aria-hidden="true" /> Configure
              </Button>
            }
          />
          <ConfirmDialog
            title={queue.paused ? "Resume this queue?" : "Pause this queue?"}
            description={
              queue.paused
                ? `Workers will begin claiming jobs from "${queue.name}" again.`
                : `Workers will stop claiming new jobs from "${queue.name}". Running jobs finish normally.`
            }
            confirmLabel={queue.paused ? "Resume" : "Pause"}
            onConfirm={async () => {
              try { await (queue.paused ? queuesApi.resume(queue.id) : queuesApi.pause(queue.id)); }
              catch (e) { reportError(e, "Failed to change queue state"); }
              mutate();
            }}
            trigger={
              <Button size="sm" variant="outline" className="rounded-lg">
                {queue.paused
                  ? <><PlayCircle className="size-3.5" aria-hidden="true" /> Resume</>
                  : <><PauseCircle className="size-3.5" aria-hidden="true" /> Pause</>}
              </Button>
            }
          />
        </div>
      </div>

      <div className="grid gap-6 sm:grid-cols-2 xl:grid-cols-4">
        {cards.map((c, i) => (
          <motion.div
            key={c.label}
            initial={{ opacity: 0, scale: 0.97 }}
            animate={{ opacity: 1, scale: 1 }}
            transition={{ duration: 0.2, delay: i * 0.05 }}
          >
            <Card className="rounded-xl p-5">
              <p className="text-xs font-medium text-muted-foreground">{c.label}</p>
              <p className="mt-2 text-2xl font-bold tabular-nums tracking-tight" style={{ color: `hsl(var(${c.token}))` }}>
                {c.value.toLocaleString()}
              </p>
            </Card>
          </motion.div>
        ))}
      </div>

      <Card className="rounded-xl p-5">
        <dl className="grid grid-cols-2 gap-x-6 gap-y-4 sm:grid-cols-4">
          <div>
            <dt className="text-[10px] uppercase tracking-wide text-muted-foreground">Concurrency</dt>
            <dd className="mt-1 font-mono text-sm">{queue.concurrency_limit}</dd>
          </div>
          <div>
            <dt className="text-[10px] uppercase tracking-wide text-muted-foreground">Priority</dt>
            <dd className="mt-1 font-mono text-sm">{queue.priority}</dd>
          </div>
          <div>
            <dt className="text-[10px] uppercase tracking-wide text-muted-foreground">Avg duration</dt>
            <dd className="mt-1 font-mono text-sm">{fmtDuration(metrics?.avg_duration_ms)}</dd>
          </div>
          <div>
            <dt className="text-[10px] uppercase tracking-wide text-muted-foreground">Created</dt>
            <dd className="mt-1 text-sm">{fmtRelative(queue.created_at)}</dd>
          </div>
        </dl>
      </Card>

      <Tabs defaultValue="jobs">
        <TabsList className="rounded-lg">
          <TabsTrigger value="jobs" className="rounded-md text-xs">Jobs ({jobPage?.data.length ?? 0})</TabsTrigger>
          <TabsTrigger value="dlq" className="rounded-md text-xs">Dead letter ({dlqPage?.data.length ?? 0})</TabsTrigger>
        </TabsList>

        <TabsContent value="jobs" className="mt-3">
          <ul className="overflow-hidden rounded-xl border border-border">
            {(jobPage?.data ?? []).map((j) => (
              <li key={j.id}>
                <button
                  onClick={() => router.push(`/jobs/${j.id}`)}
                  className="flex h-12 w-full items-center gap-3 border-b border-border px-4 text-left transition-colors last:border-0 hover:bg-accent/40 focus-visible:ring-2 focus-visible:ring-primary"
                >
                  <StateDot state={j.status} />
                  <span className="min-w-0 flex-1 truncate text-sm font-medium">{j.type}</span>
                  <span className="font-mono text-[11px] text-muted-foreground">{j.attempt_count}/{j.max_attempts}</span>
                  <JobStateBadge state={j.status} />
                  <span className="w-20 shrink-0 text-right text-[11px] text-muted-foreground">{fmtRelative(j.created_at)}</span>
                </button>
              </li>
            ))}
            {!jobPage?.data.length && (
              <li className="px-4 py-10 text-center text-xs text-muted-foreground">No jobs in this queue.</li>
            )}
          </ul>
        </TabsContent>

        <TabsContent value="dlq" className="mt-3">
          <ul className="overflow-hidden rounded-xl border border-border">
            {(dlqPage?.data ?? []).map((e) => (
              <li key={e.id} className="flex items-center gap-3 border-b border-border px-4 py-3 last:border-0">
                <div className="min-w-0 flex-1">
                  <p className="font-mono text-xs">{e.job_id.slice(0, 8)}</p>
                  <p className="truncate font-mono text-[11px] text-destructive">{e.final_error || "—"}</p>
                </div>
                <span className="shrink-0 text-[11px] text-muted-foreground">{fmtRelative(e.moved_at)}</span>
                <Button
                  size="sm" variant="outline" className="rounded-lg text-xs"
                  aria-label={`Retry job ${e.job_id}`}
                  onClick={async () => {
                    try { await dlqApi.retry(e.id); }
                    catch (err) { reportError(err, "Failed to retry job"); }
                    refetchDlq(); refetchJobs();
                  }}
                >
                  <RotateCcw className="size-3.5" aria-hidden="true" />
                </Button>
                <ConfirmDialog
                  title="Discard this job permanently?"
                  description={`Job ${e.job_id} will be removed from the dead letter queue. This cannot be undone.`}
                  confirmLabel="Discard"
                  onConfirm={async () => {
                    try { await dlqApi.discard(e.id); }
                    catch (err) { reportError(err, "Failed to discard job"); }
                    refetchDlq();
                  }}
                  trigger={
                    <Button size="sm" variant="ghost" className="rounded-lg text-xs text-muted-foreground hover:text-destructive" aria-label={`Discard job ${e.job_id}`}>
                      <Trash2 className="size-3.5" aria-hidden="true" />
                    </Button>
                  }
                />
              </li>
            ))}
            {!dlqPage?.data.length && (
              <li className="px-4 py-10 text-center text-xs text-muted-foreground">Nothing dead-lettered from this queue.</li>
            )}
          </ul>
        </TabsContent>
      </Tabs>

    </div>
  );
}
