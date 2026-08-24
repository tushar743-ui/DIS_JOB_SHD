"use client";

import { useMemo } from "react";
import Link from "next/link";
import { motion } from "framer-motion";
import { GitBranch, PauseCircle, PlayCircle, Plus, Settings2, Trash2 } from "lucide-react";
import { useAuthStore } from "@/lib/auth-store";
import { useQueues, useProjectMetrics } from "@/hooks/use-data";
import { queues as queuesApi, type Queue } from "@/lib/api";
import { QueueConfigSheet } from "@/components/queues/queue-config-sheet";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { EmptyState, ErrorState, TableSkeleton } from "@/components/states";
import { reportError } from "@/lib/errors";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Progress } from "@/components/ui/progress";
import { Separator } from "@/components/ui/separator";

function statusOf(q: Queue) {
  if (q.paused) return { label: "PAUSED", token: "--state-scheduled" };
  return { label: "ACTIVE", token: "--state-completed" };
}

export default function QueueManagerPage() {
  const projectId = useAuthStore((s) => s.projectId);
  const { data: queueList, error, isLoading, mutate } = useQueues(projectId);
  const { data: metrics } = useProjectMetrics(projectId);

  const statsById = useMemo(() => {
    const map: Record<string, Record<string, number>> = {};
    for (const q of metrics?.queues ?? []) map[q.queue_id] = q.by_status as Record<string, number>;
    return map;
  }, [metrics]);

  async function togglePause(q: Queue) {
    const next = (queueList ?? []).map((x) => (x.id === q.id ? { ...x, paused: !x.paused } : x));
    mutate(next, false);
    try {
      await (q.paused ? queuesApi.resume(q.id) : queuesApi.pause(q.id));
    } catch (e) {
      reportError(e, q.paused ? "Failed to resume queue" : "Failed to pause queue");
    }
    mutate();
  }

  async function remove(q: Queue) {
    try { await queuesApi.delete(q.id); }
    catch (e) { reportError(e, "Failed to delete queue"); }
    mutate();
  }

  if (error) return <ErrorState onRetry={() => mutate()} />;

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <p className="t-meta text-muted-foreground">
          <span className="font-mono font-medium tabular-nums text-foreground">{queueList?.length ?? 0}</span>
          {" "}queue{(queueList?.length ?? 0) === 1 ? "" : "s"} in this project
        </p>
        <QueueConfigSheet
          queue={null}
          projectId={projectId}
          onSaved={() => mutate()}
          trigger={
            <Button size="sm" className="rounded-lg" aria-label="Create queue">
              <Plus className="size-3.5" aria-hidden="true" /> Create Queue
            </Button>
          }
        />
      </div>

      {isLoading && !queueList ? (
        <TableSkeleton rows={3} cols={4} />
      ) : !queueList?.length ? (
        <EmptyState
          icon={GitBranch}
          title="No queues yet"
          description="Create a queue to start routing jobs to your workers."
          action={
            <QueueConfigSheet
              queue={null}
              projectId={projectId}
              onSaved={() => mutate()}
              trigger={
                <Button size="sm" className="rounded-lg" aria-label="Create queue">
                  <Plus className="size-3.5" aria-hidden="true" /> Create Queue
                </Button>
              }
            />
          }
        />
      ) : (
        <div className="grid gap-6 md:grid-cols-2 xl:grid-cols-3">
          {queueList.map((q, i) => {
            const st = statusOf(q);
            const by = statsById[q.id] ?? {};
            const running = by.running ?? 0;
            const queued = by.queued ?? 0;
            const retry = by.failed ?? 0;
            const util = q.concurrency_limit ? Math.min(100, (running / q.concurrency_limit) * 100) : 0;
            const bar = util > 90 ? "--state-failed" : util >= 70 ? "--state-scheduled" : "--state-completed";

            return (
              <motion.div
                key={q.id}
                initial={{ opacity: 0, scale: 0.97 }}
                animate={{ opacity: 1, scale: 1 }}
                transition={{ duration: 0.2, delay: i * 0.05 }}
              >
                <Card className="rounded-xl p-6 transition-colors hover:border-primary">
                  <div className="flex items-start justify-between gap-2">
                    <Link
                      href={`/queues/${q.id}`}
                      className="t-title min-w-0 truncate hover:text-primary focus-visible:ring-2 focus-visible:ring-primary"
                    >
                      {q.name}
                    </Link>
                    <Badge
                      variant="outline"
                      role="status"
                      className="t-label shrink-0 gap-1.5 rounded-full px-2.5 py-1"
                      style={{ borderColor: `color-mix(in oklab, var(${st.token}) 40%, transparent)`, color: `var(${st.token})` }}
                    >
                      <span className="size-1.5 rounded-full bg-current" aria-hidden="true" />
                      {st.label}
                    </Badge>
                  </div>

                  <Separator className="my-3" />

                  <div className="flex items-baseline gap-6">
                    <div>
                      <p className="t-label text-muted-foreground">Concurrency</p>
                      <p className="t-data mt-1 text-[1.125rem] font-semibold text-foreground">{q.concurrency_limit}</p>
                    </div>
                    <div>
                      <p className="t-label text-muted-foreground">Priority</p>
                      <p className="t-data mt-1 text-[1.125rem] font-semibold text-foreground">{q.priority}</p>
                    </div>
                  </div>

                  <div className="mt-5">
                    <div className="mb-2 flex items-baseline justify-between">
                      <span className="t-label text-muted-foreground">Utilization</span>
                      <span className="t-data text-[0.9375rem] font-semibold">{util.toFixed(0)}%</span>
                    </div>
                    <Progress
                      value={util}
                      aria-label={`Queue utilization ${util.toFixed(0)} percent`}
                      className="h-2"
                      style={{ ["--progress-color" as string]: `var(${bar})` }}
                    />
                  </div>

                  <div className="mt-4 flex flex-wrap gap-2">
                    <Badge variant="outline" className="t-chip rounded-full px-2.5 py-1">
                      <span className="font-mono font-semibold tabular-nums">{queued}</span> queued
                    </Badge>
                    <Badge variant="outline" className="t-chip rounded-full px-2.5 py-1">
                      <span className="font-mono font-semibold tabular-nums">{running}</span> running
                    </Badge>
                    <Badge variant="outline" className="t-chip rounded-full px-2.5 py-1">
                      <span className="font-mono font-semibold tabular-nums">{retry}</span> retry
                    </Badge>
                  </div>

                  <div className="mt-5 flex flex-wrap gap-2">
                    <ConfirmDialog
                      title={q.paused ? "Resume this queue?" : "Pause this queue?"}
                      description={
                        q.paused
                          ? `Workers will begin claiming jobs from "${q.name}" again.`
                          : `Workers will stop claiming new jobs from "${q.name}". Running jobs finish normally.`
                      }
                      confirmLabel={q.paused ? "Resume" : "Pause"}
                      onConfirm={() => togglePause(q)}
                      trigger={
                        <Button size="sm" variant="outline" className="t-chip rounded-lg">
                          {q.paused
                            ? <><PlayCircle className="size-3.5" aria-hidden="true" /> Resume</>
                            : <><PauseCircle className="size-3.5" aria-hidden="true" /> Pause</>}
                        </Button>
                      }
                    />
                    <QueueConfigSheet
                      queue={q}
                      projectId={projectId}
                      onSaved={() => mutate()}
                      trigger={
                        <Button size="sm" variant="outline" className="t-chip rounded-lg" aria-label={`Configure ${q.name}`}>
                          <Settings2 className="size-3.5" aria-hidden="true" /> Configure
                        </Button>
                      }
                    />
                    <ConfirmDialog
                      title="Delete this queue?"
                      description={`Queue "${q.name}" and all of its jobs will be permanently deleted. This cannot be undone.`}
                      confirmLabel="Delete"
                      onConfirm={() => remove(q)}
                      trigger={
                        <Button size="sm" variant="ghost" className="t-chip rounded-lg text-muted-foreground hover:text-destructive" aria-label={`Delete queue ${q.name}`}>
                          <Trash2 className="size-3.5" aria-hidden="true" />
                        </Button>
                      }
                    />
                  </div>
                </Card>
              </motion.div>
            );
          })}
        </div>
      )}

    </div>
  );
}
