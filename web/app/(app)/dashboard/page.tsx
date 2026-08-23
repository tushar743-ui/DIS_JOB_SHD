"use client";

import { useMemo } from "react";
import { useRouter } from "next/navigation";
import { motion } from "framer-motion";
import { Activity, CheckCircle2, Cpu, FolderPlus, Timer } from "lucide-react";
import { useAuthStore } from "@/lib/auth-store";
import { useProjectMetrics, useAllJobs, useWorkers, useQueues, useQueueMetrics } from "@/hooks/use-data";
import { KpiCard } from "@/components/dashboard/kpi-card";
import { LiveFeed } from "@/components/dashboard/live-feed";
import { ThroughputChart } from "@/components/dashboard/throughput-chart";
import { JobStatesDonut, buildJobStateSlices } from "@/components/dashboard/job-states-donut";
import { EmptyState, ErrorState, KpiSkeleton, ChartSkeleton } from "@/components/states";
import { Card } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { BlurFade } from "@/components/ui/blur-fade";
import { cn } from "@/lib/utils";
import { fmtRelative } from "@/lib/status";
import { useNow } from "@/hooks/use-elapsed-time";

export default function DashboardPage() {
  const router = useRouter();
  const projectId = useAuthStore((s) => s.projectId);

  const { data: metrics, error: metricsError, mutate: refetchMetrics } = useProjectMetrics(projectId);
  const { data: queueList } = useQueues(projectId);
  const { data: runningJobs } = useAllJobs(projectId, "running", true);
  const { data: allJobs } = useAllJobs(projectId);
  const { data: workerList } = useWorkers(projectId);
  const { data: primaryMetrics } = useQueueMetrics(queueList?.[0]?.id ?? null);
  const now = useNow();

  const totals = useMemo(() => {
    const acc: Record<string, number> = {};
    for (const q of metrics?.queues ?? []) {
      for (const [state, n] of Object.entries(q.by_status)) acc[state] = (acc[state] ?? 0) + (n ?? 0);
    }
    return acc;
  }, [metrics]);

  const totalJobs = Object.values(totals).reduce((a, b) => a + b, 0);
  const completed = totals.completed ?? 0;
  const failed = (totals.failed ?? 0) + (totals.dead ?? 0);
  const finished = completed + failed;
  const successRate = finished ? (completed / finished) * 100 : 100;
  const avgMs = primaryMetrics?.avg_duration_ms ?? 0;

  const throughput = useMemo(
    () =>
      (primaryMetrics?.throughput_24h ?? []).map((p) => ({
        t: new Date(p.hour).toLocaleTimeString("en-US", { hour: "2-digit", minute: "2-digit", hour12: false }),
        completed: p.completed,
        failed: p.failed,
      })),
    [primaryMetrics]
  );

  const donut = useMemo(() => buildJobStateSlices(totals), [totals]);

  const sparkOf = (key: string) =>
    (primaryMetrics?.throughput_24h ?? []).slice(-12).map((p) => (key === "failed" ? p.failed : p.completed));

  if (metricsError) return <ErrorState onRetry={() => refetchMetrics()} />;
  if (!projectId)
    return (
      <EmptyState
        icon={FolderPlus}
        title="Setting up your workspace"
        description="Creating your first project. This happens once and only takes a moment."
      />
    );
  if (!metrics) return <div className="space-y-6"><KpiSkeleton /><ChartSkeleton /></div>;

  return (
    <div className="space-y-6">
      <BlurFade delay={0.05}>
        <div className="grid gap-6 sm:grid-cols-2 xl:grid-cols-4">
          <KpiCard
            index={0} label="Total Jobs" value={totalJobs} icon={Activity}
            spark={sparkOf("completed")} accent="hsl(var(--state-queued))"
            trend={{ value: 12, label: "vs yesterday" }}
          />
          <KpiCard
            index={1} label="Running Now" value={runningJobs?.length ?? 0} icon={Cpu}
            accent="hsl(var(--state-running))" ripple
          />
          <KpiCard
            index={2} label="Success Rate" value={successRate} suffix="%" decimals={1}
            icon={CheckCircle2} accent="hsl(var(--state-completed))"
            spark={sparkOf("completed")}
            trend={{ value: successRate - 99, label: "vs target" }}
          />
          <KpiCard
            index={3} label="Avg Duration" value={avgMs} suffix="ms" icon={Timer}
            accent="hsl(var(--state-scheduled))" spark={sparkOf("failed")}
          />
        </div>
      </BlurFade>

      <BlurFade delay={0.1}>
        <div className="grid gap-6 lg:grid-cols-5">
          <Card className="rounded-xl p-5 lg:col-span-3">
            <div className="mb-4 flex items-center justify-between">
              <h2 className="text-sm font-semibold tracking-tight">Throughput</h2>
              <span className="text-[11px] text-muted-foreground">last 24h · {queueList?.[0]?.name ?? "-"}</span>
            </div>
            {throughput.length === 0 ? (
              <EmptyState
                icon={Activity}
                title="No throughput yet"
                description="Completed and failed jobs will chart here once a queue starts processing."
              />
            ) : (
              <ThroughputChart data={throughput} />
            )}
          </Card>

          <Card className="rounded-xl p-5 lg:col-span-2">
            <h2 className="mb-4 text-sm font-semibold tracking-tight">Job States</h2>
            {donut.length === 0 ? (
              <EmptyState
                icon={Activity}
                title="No jobs yet"
                description="Enqueue a job and its lifecycle states will break down here."
              />
            ) : (
              <JobStatesDonut data={donut} total={totalJobs} />
            )}
          </Card>
        </div>
      </BlurFade>

      <BlurFade delay={0.15}>
        <section>
          <div className="mb-3 flex items-center gap-2">
            <h2 className="text-sm font-semibold tracking-tight">Live Activity</h2>
            <Badge variant="outline" role="status" aria-live="polite" className="gap-1.5 rounded-full border-state-completed/40 px-2 text-[10px] text-state-completed">
              <span className="relative flex size-1.5">
                <span className="absolute inline-flex size-full animate-ping rounded-full bg-current opacity-75" />
                <span className="relative inline-flex size-1.5 rounded-full bg-current" />
              </span>
              LIVE
            </Badge>
            <span className="ml-auto text-[11px] text-muted-foreground">polling every 2s</span>
          </div>
          <LiveFeed jobs={runningJobs ?? allJobs?.filter((j) => j.status === "running") ?? []} />
        </section>
      </BlurFade>

      <BlurFade delay={0.2}>
        <section>
          <h2 className="mb-3 text-sm font-semibold tracking-tight">Worker Health</h2>
          <div className="flex flex-wrap gap-2">
            {(workerList ?? []).slice(0, 24).map((w, i) => {
              const stale = now - new Date(w.last_heartbeat_at).getTime() > 30_000;
              return (
                <motion.button
                  key={w.id}
                  initial={{ opacity: 0, scale: 0.97 }}
                  animate={{ opacity: 1, scale: 1 }}
                  transition={{ duration: 0.2, delay: Math.min(i, 12) * 0.02 }}
                  onClick={() => router.push(`/workers/${w.id}`)}
                  aria-label={`Worker ${w.hostname}, ${stale ? "stale" : w.status}`}
                  className={cn(
                    "flex items-center gap-2 rounded-full border border-border px-3 py-1.5 text-[11px] transition-colors hover:bg-accent focus-visible:ring-2 focus-visible:ring-primary",
                    stale && "opacity-60"
                  )}
                >
                  <span
                    className="size-1.5 rounded-full"
                    style={{ background: stale ? "hsl(var(--state-failed))" : "hsl(var(--state-completed))" }}
                    aria-hidden="true"
                  />
                  <span className="font-mono">{w.hostname}</span>
                  <span className="text-muted-foreground">
                    {stale ? "stale" : `${w.concurrency} slots`}
                  </span>
                  <span className="text-muted-foreground">· {fmtRelative(w.last_heartbeat_at)}</span>
                </motion.button>
              );
            })}
            {!workerList?.length && (
              <p className="text-xs text-muted-foreground">No workers registered.</p>
            )}
          </div>
        </section>
      </BlurFade>
    </div>
  );
}
