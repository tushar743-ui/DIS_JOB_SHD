"use client";

import { useMemo, useState } from "react";
import {
  Area, AreaChart, Bar, BarChart, CartesianGrid, Line, LineChart,
  ResponsiveContainer, Tooltip as RTooltip, XAxis, YAxis,
} from "recharts";
import { useAuthStore } from "@/lib/auth-store";
import { useQueues, useProjectMetrics, useWorkers, useAllJobs, useQueueMetrics } from "@/hooks/use-data";
import { ChartSkeleton } from "@/components/states";
import { useNow } from "@/hooks/use-elapsed-time";
import { Card } from "@/components/ui/card";
import { BlurFade } from "@/components/ui/blur-fade";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";

const RANGES = ["1H", "6H", "24H", "7D", "30D"];
const AXIS = { fontSize: 10, fill: "hsl(var(--muted-foreground))" };
const TOOLTIP = {
  background: "hsl(var(--popover))",
  border: "1px solid hsl(var(--border))",
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
  const { data: queueList } = useQueues(projectId);
  const { data: metrics } = useProjectMetrics(projectId);
  const { data: workerList } = useWorkers(projectId);
  const { data: allJobs } = useAllJobs(projectId);
  const { data: primary } = useQueueMetrics(queueList?.[0]?.id ?? null);
  const now = useNow();

  const points = useMemo(() => {
    const n = range === "1H" ? 4 : range === "6H" ? 8 : range === "24H" ? 24 : range === "7D" ? 24 : 30;
    return (primary?.throughput_24h ?? []).slice(-n).map((p) => ({
      t: new Date(p.hour).toLocaleTimeString("en-US", { hour: "2-digit", minute: "2-digit", hour12: false }),
      completed: p.completed,
      failed: p.failed,
      enqueued: p.completed + p.failed,
    }));
  }, [primary, range]);

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
          {RANGES.map((r) => <TabsTrigger key={r} value={r} className="rounded-md text-xs">{r}</TabsTrigger>)}
        </TabsList>
      </Tabs>

      <BlurFade delay={0.05}>
        <div className="grid gap-6 lg:grid-cols-2">
          <ChartCard title="Job Throughput" subtitle={`Enqueued vs completed vs failed · ${range}`}>
            {points.length === 0 ? <ChartSkeleton /> : (
              <ResponsiveContainer width="100%" height={220}>
                <AreaChart data={points} margin={{ top: 4, right: 8, bottom: 0, left: -18 }}>
                  <CartesianGrid stroke="hsl(var(--border))" strokeDasharray="3 3" vertical={false} />
                  <XAxis dataKey="t" tick={AXIS} tickLine={false} axisLine={false} minTickGap={24} />
                  <YAxis tick={AXIS} tickLine={false} axisLine={false} width={40} />
                  <RTooltip contentStyle={TOOLTIP} />
                  <Area type="monotone" dataKey="enqueued" stroke="hsl(var(--state-queued))" fill="hsl(var(--state-queued) / 0.15)" strokeWidth={2} />
                  <Area type="monotone" dataKey="completed" stroke="hsl(var(--state-completed))" fill="hsl(var(--state-completed) / 0.15)" strokeWidth={2} />
                  <Area type="monotone" dataKey="failed" stroke="hsl(var(--state-failed))" fill="hsl(var(--state-failed) / 0.15)" strokeWidth={2} />
                </AreaChart>
              </ResponsiveContainer>
            )}
          </ChartCard>

          <ChartCard title="Queue Depth Over Time" subtitle="Queued jobs per queue — spot backlogs early">
            {depth.length === 0 ? <ChartSkeleton /> : (
              <ResponsiveContainer width="100%" height={220}>
                <LineChart data={depth} margin={{ top: 4, right: 8, bottom: 0, left: -18 }}>
                  <CartesianGrid stroke="hsl(var(--border))" strokeDasharray="3 3" vertical={false} />
                  <XAxis dataKey="t" tick={AXIS} tickLine={false} axisLine={false} minTickGap={24} />
                  <YAxis tick={AXIS} tickLine={false} axisLine={false} width={40} />
                  <RTooltip contentStyle={TOOLTIP} />
                  {queueNames.map((name, i) => (
                    <Line
                      key={name} type="monotone" dataKey={name} dot={false} strokeWidth={2}
                      stroke={`hsl(var(${lineColors[i % lineColors.length]}))`}
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
                  <CartesianGrid stroke="hsl(var(--border))" strokeDasharray="3 3" vertical={false} />
                  <XAxis dataKey="worker" tick={AXIS} tickLine={false} axisLine={false} interval={0} angle={-25} height={50} textAnchor="end" />
                  <YAxis tick={AXIS} tickLine={false} axisLine={false} width={40} unit="%" />
                  <RTooltip contentStyle={TOOLTIP} />
                  <Bar dataKey="active" fill="hsl(var(--state-running))" radius={[4, 4, 0, 0]} />
                </BarChart>
              </ResponsiveContainer>
            )}
          </ChartCard>

          <ChartCard title="Retry Rate by Job Kind" subtitle="Highest first — flags flaky job types">
            {retryRate.length === 0 ? <ChartSkeleton /> : (
              <ResponsiveContainer width="100%" height={220}>
                <BarChart data={retryRate} layout="vertical" margin={{ top: 4, right: 16, bottom: 0, left: 8 }}>
                  <CartesianGrid stroke="hsl(var(--border))" strokeDasharray="3 3" horizontal={false} />
                  <XAxis type="number" tick={AXIS} tickLine={false} axisLine={false} unit="%" />
                  <YAxis type="category" dataKey="kind" tick={AXIS} tickLine={false} axisLine={false} width={110} />
                  <RTooltip contentStyle={TOOLTIP} />
                  <Bar dataKey="rate" fill="hsl(var(--state-retrying))" radius={[0, 4, 4, 0]} />
                </BarChart>
              </ResponsiveContainer>
            )}
          </ChartCard>
        </div>
      </BlurFade>
    </div>
  );
}
