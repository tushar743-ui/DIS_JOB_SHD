"use client";

import { useMemo } from "react";
import { FolderPlus } from "lucide-react";
import { useAuthStore } from "@/lib/auth-store";
import { useProjectMetrics, useAllJobs } from "@/hooks/use-data";
import { SectionCards, type SectionCard } from "@/components/dashboard/section-cards";
import { ChartThroughputInteractive } from "@/components/dashboard/chart-throughput-interactive";
import { RecentJobsTable } from "@/components/dashboard/recent-jobs-table";
import { EmptyState, ErrorState, KpiSkeleton, ChartSkeleton } from "@/components/states";

function fmtDuration(ms: number) {
  if (ms >= 1000) return `${(ms / 1000).toFixed(1)}s`;
  return `${Math.round(ms)}ms`;
}

export default function DashboardPage() {
  const projectId = useAuthStore((s) => s.projectId);

  const { data: metrics, error: metricsError, mutate: refetchMetrics } = useProjectMetrics(projectId);
  const { data: allJobs } = useAllJobs(projectId);

  const totals = useMemo(() => {
    const acc: Record<string, number> = {};
    for (const q of metrics?.queues ?? []) {
      for (const [state, n] of Object.entries(q.by_status)) acc[state] = (acc[state] ?? 0) + (n ?? 0);
    }
    return acc;
  }, [metrics]);

  const throughput = useMemo(() => metrics?.throughput_24h ?? [], [metrics]);

  const cards = useMemo<SectionCard[]>(() => {
    const totalJobs = Object.values(totals).reduce((a, b) => a + b, 0);
    const running = (totals.running ?? 0) + (totals.claimed ?? 0);
    const completed = totals.completed ?? 0;
    const failed = (totals.failed ?? 0) + (totals.dead ?? 0);
    const finished = completed + failed;
    const successRate = finished ? (completed / finished) * 100 : 100;
    const avgMs = metrics?.avg_duration_ms ?? 0;

    const half = Math.floor(throughput.length / 2);
    const sum = (from: number, to: number) =>
      throughput.slice(from, to).reduce((a, p) => a + p.completed + p.failed, 0);
    const recent = sum(half, throughput.length);
    const earlier = sum(0, half);
    const volumeDelta = earlier > 0 ? ((recent - earlier) / earlier) * 100 : 0;

    return [
      {
        label: "Total Jobs",
        value: totalJobs.toLocaleString(),
        delta: volumeDelta,
        headline: volumeDelta >= 0 ? "Volume trending up" : "Volume trending down",
        detail: "Across every queue in this project",
      },
      {
        label: "Running Now",
        value: running.toLocaleString(),
        headline: `${metrics?.active_workers ?? 0} active workers`,
        detail: "Running plus claimed, refreshed every 2s",
      },
      {
        label: "Success Rate",
        value: `${successRate.toFixed(1)}%`,
        delta: successRate - 99,
        deltaSuffix: "pt",
        headline: successRate >= 99 ? "Meeting the 99% target" : "Below the 99% target",
        detail: `${completed.toLocaleString()} completed · ${failed.toLocaleString()} failed`,
      },
      {
        label: "Avg Duration",
        value: avgMs ? fmtDuration(avgMs) : "—",
        headline: `Across ${metrics?.queues.length ?? 0} queues`,
        detail: "Mean execution time over 24h",
      },
    ];
  }, [totals, throughput, metrics]);

  if (metricsError) return <ErrorState onRetry={() => refetchMetrics()} />;
  if (!projectId)
    return (
      <EmptyState
        icon={FolderPlus}
        title="Setting up your workspace"
        description="Creating your first project. This happens once and only takes a moment."
      />
    );
  if (!metrics)
    return (
      <div className="space-y-6">
        <KpiSkeleton />
        <ChartSkeleton />
      </div>
    );

  return (
    <div className="@container/main flex flex-1 flex-col gap-2">
      <div className="flex flex-col gap-4 md:gap-6">
        <SectionCards cards={cards} />
        <ChartThroughputInteractive data={throughput} />
        <RecentJobsTable jobs={allJobs ?? []} />
      </div>
    </div>
  );
}
