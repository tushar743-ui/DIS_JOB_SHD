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
        <p className="text-xs text-muted-foreground">
          {queueList?.length ?? 0} queue{(queueList?.length ?? 0) === 1 ? "" : "s"} in this project
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
                <Card className="rounded-xl p-5 transition-all hover:border-primary hover:shadow-md">
                  <div className="flex items-start justify-between gap-2">
                    <Link
                      href={`/queues/${q.id}`}
                      className="min-w-0 truncate text-sm font-semibold tracking-tight hover:text-primary focus-visible:ring-2 focus-visible:ring-primary"
                    >
                      {q.name}
                    </Link>
                    <Badge
                      variant="outline"
                      role="status"
                      className="shrink-0 gap-1.5 rounded-full text-[10px]"
                      style={{ borderColor: `color-mix(in oklab, var(${st.token}) 40%, transparent)`, color: `var(${st.token})` }}
                    >
                      <span className="size-1.5 rounded-full bg-current" aria-hidden="true" />
                      {st.label}
                    </Badge>
                  </div>

                  <Separator className="my-3" />

                  <p className="text-[11px] text-muted-foreground">
                    Concurrency: <span className="font-mono text-foreground">{q.concurrency_limit}</span>
                    <span className="mx-2">·</span>
                    Priority: <span className="font-mono text-foreground">{q.priority}</span>
                  </p>

                  <div className="mt-3">
                    <div className="mb-1 flex items-center justify-between text-[11px]">
                      <span className="text-muted-foreground">Utilization</span>
                      <span className="font-mono tabular-nums">{util.toFixed(0)}%</span>
                    </div>
                    <Progress
                      value={util}
                      aria-label={`Queue utilization ${util.toFixed(0)} percent`}
                      className="h-2"
                      style={{ ["--progress-color" as string]: `var(${bar})` }}
                    />
                  </div>

                  <div className="mt-3 flex flex-wrap gap-1.5">
                    <Badge variant="outline" className="rounded-full text-[10px]">{queued} queued</Badge>
                    <Badge variant="outline" className="rounded-full text-[10px]">{running} running</Badge>
                    <Badge variant="outline" className="rounded-full text-[10px]">{retry} retry</Badge>
                  </div>

                  <div className="mt-4 flex flex-wrap gap-2">
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
                        <Button size="sm" variant="outline" className="rounded-lg text-xs">
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
                        <Button size="sm" variant="outline" className="rounded-lg text-xs" aria-label={`Configure ${q.name}`}>
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
                        <Button size="sm" variant="ghost" className="rounded-lg text-xs text-muted-foreground hover:text-destructive" aria-label={`Delete queue ${q.name}`}>
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
