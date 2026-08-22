"use client";

import { useEffect, useState, useCallback } from "react";
import {
  AreaChart, Area, XAxis, YAxis, CartesianGrid, Tooltip,
  ResponsiveContainer, BarChart, Bar,
} from "recharts";
import { metrics, queues, type QueueMetrics, type Queue } from "@/lib/api";
import { useAuthStore } from "@/lib/auth-store";
import { ErrorBanner } from "@/components/error-banner";
import { errMessage } from "@/lib/errors";
import { Skeleton } from "@/components/ui/skeleton";
import { fmtDuration } from "@/lib/status";
import {
  Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
} from "@/components/ui/select";

export default function MetricsPage() {
  const projectId = useAuthStore((s) => s.projectId);
  const [queueList, setQueueList] = useState<Queue[]>([]);
  const [selectedQueue, setSelectedQueue] = useState<string>("");
  const [error, setError] = useState("");

  // Results carry the queue they describe, so "is a fetch outstanding" is a
  // comparison during render instead of a flag toggled from an effect.
  const [result, setResult] = useState<{ key: string; data: QueueMetrics | null } | null>(null);
  const loading = Boolean(selectedQueue) && result?.key !== selectedQueue;
  const data = result?.data ?? null;

  useEffect(() => {
    if (!projectId) return;
    queues.list(projectId)
      .then((q) => {
        setQueueList(q);
        if (q.length > 0) setSelectedQueue(q[0].id);
      })
      .catch((e) => setError(errMessage(e, "Failed to load queues")));
  }, [projectId]);

  const load = useCallback(() => {
    if (!selectedQueue) return;
    const key = selectedQueue;
    return metrics.queue(selectedQueue).then(
      (m) => { setResult({ key, data: m }); setError(""); },
      (e) => {
        setResult({ key, data: null });
        setError(errMessage(e, "Failed to load metrics"));
      }
    );
  }, [selectedQueue]);

  useEffect(() => { load(); }, [load]);

  const throughputData = data?.throughput_24h?.map((p) => ({
    hour: new Date(p.hour).toLocaleTimeString("en-US", { hour: "2-digit", minute: "2-digit", hour12: false }),
    completed: p.completed,
    failed: p.failed,
  })) ?? [];

  const statusData = data ? Object.entries(data.by_status).map(([status, count]) => ({
    status, count,
  })) : [];

  return (
    <div className="p-6 max-w-5xl mx-auto space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-lg font-semibold">Metrics</h1>
        <Select value={selectedQueue} onValueChange={(v) => v && setSelectedQueue(v)}>
          <SelectTrigger className="w-48">
            <SelectValue placeholder="Select queue" />
          </SelectTrigger>
          <SelectContent>
            {queueList.map((q) => (
              <SelectItem key={q.id} value={q.id}>{q.name}</SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>

      <ErrorBanner message={error} />

      {data && (
        <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
          {[
            { label: "Completed", value: data.by_status.completed ?? 0, color: "text-emerald-400" },
            { label: "Failed", value: (data.by_status.failed ?? 0) + (data.by_status.dead ?? 0), color: "text-red-400" },
            { label: "Queued", value: data.by_status.queued ?? 0, color: "text-amber-400" },
            { label: "Avg duration", value: data.avg_duration_ms, color: "text-blue-400", fmt: (v: number) => fmtDuration(v) },
          ].map(({ label, value, color, fmt }) => (
            <div key={label} className="border border-border rounded-lg p-4 bg-card">
              <p className="text-xs text-muted-foreground">{label}</p>
              <p className={`text-2xl font-semibold font-mono mt-1 ${color}`}>
                {fmt ? fmt(value as number) : (value ?? 0).toLocaleString()}
              </p>
            </div>
          ))}
        </div>
      )}

      {loading && !data && (
        <div className="space-y-4">
          <Skeleton className="h-48 rounded-lg" />
          <Skeleton className="h-48 rounded-lg" />
        </div>
      )}

      {data && (
        <div className="border border-border rounded-lg p-4 bg-card">
          <h2 className="text-sm font-medium mb-4">Throughput (last 24h)</h2>
          {throughputData.length === 0 ? (
            <div className="h-40 flex items-center justify-center text-sm text-muted-foreground">
              No data yet for this time window.
            </div>
          ) : (
            <ResponsiveContainer width="100%" height={200}>
              <AreaChart data={throughputData} margin={{ top: 4, right: 4, left: 0, bottom: 0 }}>
                <defs>
                  <linearGradient id="gradCompleted" x1="0" y1="0" x2="0" y2="1">
                    <stop offset="5%" stopColor="#34d399" stopOpacity={0.2} />
                    <stop offset="95%" stopColor="#34d399" stopOpacity={0} />
                  </linearGradient>
                  <linearGradient id="gradFailed" x1="0" y1="0" x2="0" y2="1">
                    <stop offset="5%" stopColor="#f87171" stopOpacity={0.2} />
                    <stop offset="95%" stopColor="#f87171" stopOpacity={0} />
                  </linearGradient>
                </defs>
                <CartesianGrid strokeDasharray="3 3" stroke="hsl(var(--border))" />
                <XAxis dataKey="hour" tick={{ fontSize: 11, fill: "hsl(var(--muted-foreground))" }} />
                <YAxis tick={{ fontSize: 11, fill: "hsl(var(--muted-foreground))" }} />
                <Tooltip
                  contentStyle={{
                    background: "hsl(var(--card))", border: "1px solid hsl(var(--border))",
                    borderRadius: "6px", fontSize: "12px",
                  }}
                />
                <Area type="monotone" dataKey="completed" stroke="#34d399" fill="url(#gradCompleted)" strokeWidth={1.5} />
                <Area type="monotone" dataKey="failed" stroke="#f87171" fill="url(#gradFailed)" strokeWidth={1.5} />
              </AreaChart>
            </ResponsiveContainer>
          )}
        </div>
      )}

      {data && statusData.length > 0 && (
        <div className="border border-border rounded-lg p-4 bg-card">
          <h2 className="text-sm font-medium mb-4">Jobs by status</h2>
          <ResponsiveContainer width="100%" height={180}>
            <BarChart data={statusData} margin={{ top: 4, right: 4, left: 0, bottom: 0 }}>
              <CartesianGrid strokeDasharray="3 3" stroke="hsl(var(--border))" vertical={false} />
              <XAxis dataKey="status" tick={{ fontSize: 11, fill: "hsl(var(--muted-foreground))" }} />
              <YAxis tick={{ fontSize: 11, fill: "hsl(var(--muted-foreground))" }} />
              <Tooltip
                contentStyle={{
                  background: "hsl(var(--card))", border: "1px solid hsl(var(--border))",
                  borderRadius: "6px", fontSize: "12px",
                }}
              />
              <Bar dataKey="count" fill="#f59e0b" radius={[3, 3, 0, 0]} />
            </BarChart>
          </ResponsiveContainer>
        </div>
      )}
    </div>
  );
}
