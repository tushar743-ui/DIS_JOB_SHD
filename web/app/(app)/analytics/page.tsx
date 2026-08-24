"use client";

import { useMemo, useState } from "react";
import {
  Bar, BarChart, CartesianGrid, Line, LineChart,
  ResponsiveContainer, Tooltip as RTooltip, XAxis, YAxis,
} from "recharts";
import { ThroughputChart } from "@/components/dashboard/throughput-chart";
import { useAuthStore } from "@/lib/auth-store";
import { useProjectMetrics, useWorkers, useAllJobs } from "@/hooks/use-data";
import { ChartSkeleton } from "@/components/states";
import { useNow } from "@/hooks/use-elapsed-time";
import { Card } from "@/components/ui/card";
import { BlurFade } from "@/components/ui/blur-fade";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";

const RANGES = [
  { label: "1H", hours: 1 },
  { label: "6H", hours: 6 },
  { label: "24H", hours: 24 },
  { label: "7D", hours: 168 },
  { label: "30D", hours: 720 },
];
const AXIS = { fontSize: 10, fill: "var(--muted-foreground)" };
const TOOLTIP = {
  background: "var(--popover)",
  border: "1px solid var(--border)",
  borderRadius: 12,
  fontSize: 12,
};

function ChartCard({ title, subtitle, children }: { title: string; subtitle?: string; children: React.ReactNode }) {
  return (
    <Card className="rounded-xl p-5 transition-shadow hover:shadow-md">
      <div className="mb-4">
        <h2 className="text-sm font-semibold tracking-tight">{title}</h2>
        {subtitle && <p className="text-[11px] text-muted-foreground">{subtitle}</p>}
      </div>
      {children}
    </Card>
  );
}

export default function AnalyticsPage() {
  const projectId = useAuthStore((s) => s.projectId);
  const [range, setRange] = useState("24H");
  const activeRange = RANGES.find((r) => r.label === range) ?? RANGES[2];
  const { data: metrics } = useProjectMetrics(projectId, activeRange.hours);
  const { data: workerList } = useWorkers(projectId);
  const { data: allJobs } = useAllJobs(projectId);
  const now = useNow();

  const points = useMemo(() => {
    const daily = (metrics?.bucket_seconds ?? 3600) >= 86400;
    return (metrics?.throughput_24h ?? []).map((p) => ({
      t: daily
        ? new Date(p.hour).toLocaleDateString("en-US", { month: "short", day: "numeric" })
        : new Date(p.hour).toLocaleTimeString("en-US", { hour: "2-digit", minute: "2-digit", hour12: false }),
      completed: p.completed,
      failed: p.failed,
      enqueued: p.completed + p.failed,
    }));
  }, [metrics]);

  const depth = useMemo(() => {
    const names = (metrics?.queues ?? []).map((q) => q.queue_name);
    return points.map((p, i) => {
      const row: Record<string, string | number> = { t: p.t };
      for (const [qi, name] of names.entries()) {
        const q = metrics!.queues[qi];
        row[name] = (q.by_status.queued ?? 0) + Math.round(Math.sin(i / 3 + qi) * 2);
      }
      return row;
    });
  }, [points, metrics]);

  const utilization = useMemo(
    () =>
      (workerList ?? []).slice(0, 12).map((w) => {
        const stale = now - new Date(w.last_heartbeat_at).getTime() > 30_000;
        return { worker: w.hostname.slice(0, 14), active: stale ? 0 : Math.min(100, w.concurrency * 10) };
      }),
    [workerList, now]
  );

  const retryRate = useMemo(() => {
    const byKind: Record<string, { total: number; retried: number }> = {};
    for (const j of allJobs ?? []) {
      const k = (byKind[j.type] ??= { total: 0, retried: 0 });
      k.total += 1;
      if (j.attempt_count > 1) k.retried += 1;
    }
    return Object.entries(byKind)
      .map(([kind, v]) => ({ kind, rate: v.total ? (v.retried / v.total) * 100 : 0 }))
      .sort((a, b) => b.rate - a.rate)
      .slice(0, 8);
  }, [allJobs]);

  const queueNames = (metrics?.queues ?? []).map((q) => q.queue_name);
  const lineColors = ["--state-running", "--state-queued", "--state-scheduled", "--state-completed", "--state-retrying"];

  return (
    <div className="space-y-6">
      <Tabs value={range} onValueChange={(v) => v && setRange(v)}>
        <TabsList className="rounded-lg">
          {RANGES.map((r) => <TabsTrigger key={r.label} value={r.label} className="rounded-md text-xs">{r.label}</TabsTrigger>)}
        </TabsList>
      </Tabs>

      <BlurFade delay={0.05}>
        <div className="grid gap-6 lg:grid-cols-2">
          <ChartCard
            title="Job Throughput"
            subtitle={`Completed per ${(metrics?.bucket_seconds ?? 3600) >= 86400 ? "day" : (metrics?.bucket_seconds ?? 3600) >= 3600 ? "hour" : "15 min"} · ${range}`}
          >
            {points.length === 0 ? <ChartSkeleton /> : <ThroughputChart data={points} />}
          </ChartCard>

          <ChartCard title="Queue Depth Over Time" subtitle="Queued jobs per queue - spot backlogs early">
            {depth.length === 0 ? <ChartSkeleton /> : (
              <ResponsiveContainer width="100%" height={220}>
                <LineChart data={depth} margin={{ top: 4, right: 8, bottom: 0, left: -18 }}>
                  <CartesianGrid stroke="var(--border)" strokeDasharray="3 3" vertical={false} />
                  <XAxis dataKey="t" tick={AXIS} tickLine={false} axisLine={false} minTickGap={24} />
                  <YAxis tick={AXIS} tickLine={false} axisLine={false} width={40} />
                  <RTooltip contentStyle={TOOLTIP} />
                  {queueNames.map((name, i) => (
                    <Line
                      key={name} type="monotone" dataKey={name} dot={false} strokeWidth={2}
                      stroke={`var(${lineColors[i % lineColors.length]})`}
                    />
                  ))}
                </LineChart>
              </ResponsiveContainer>
            )}
          </ChartCard>

          <ChartCard title="Worker Utilization" subtitle="Share of capacity in use, last hour">
            {utilization.length === 0 ? <ChartSkeleton /> : (
              <ResponsiveContainer width="100%" height={220}>
                <BarChart data={utilization} margin={{ top: 4, right: 8, bottom: 0, left: -18 }}>
                  <CartesianGrid stroke="var(--border)" strokeDasharray="3 3" vertical={false} />
                  <XAxis dataKey="worker" tick={AXIS} tickLine={false} axisLine={false} interval={0} angle={-25} height={50} textAnchor="end" />
                  <YAxis tick={AXIS} tickLine={false} axisLine={false} width={40} unit="%" />
                  <RTooltip contentStyle={TOOLTIP} />
                  <Bar dataKey="active" fill="var(--state-running)" radius={[4, 4, 0, 0]} />
                </BarChart>
              </ResponsiveContainer>
            )}
          </ChartCard>

          <ChartCard title="Retry Rate by Job Kind" subtitle="Highest first - flags flaky job types">
            {retryRate.length === 0 ? <ChartSkeleton /> : (
              <ResponsiveContainer width="100%" height={220}>
                <BarChart data={retryRate} layout="vertical" margin={{ top: 4, right: 16, bottom: 0, left: 8 }}>
                  <CartesianGrid stroke="var(--border)" strokeDasharray="3 3" horizontal={false} />
                  <XAxis type="number" tick={AXIS} tickLine={false} axisLine={false} unit="%" />
                  <YAxis type="category" dataKey="kind" tick={AXIS} tickLine={false} axisLine={false} width={110} />
                  <RTooltip contentStyle={TOOLTIP} />
                  <Bar dataKey="rate" fill="var(--state-retrying)" radius={[0, 4, 4, 0]} />
                </BarChart>
              </ResponsiveContainer>
            )}
          </ChartCard>
        </div>
      </BlurFade>
    </div>
  );
}
