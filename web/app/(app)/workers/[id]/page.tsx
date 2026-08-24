"use client";

import { useMemo } from "react";
import { useParams, useRouter } from "next/navigation";
import { Bar, BarChart, CartesianGrid, ResponsiveContainer, Tooltip as RTooltip, XAxis, YAxis } from "recharts";
import { PowerOff } from "lucide-react";
import { useAuthStore } from "@/lib/auth-store";
import { useWorker, useAllJobs } from "@/hooks/use-data";
import { useNow } from "@/hooks/use-elapsed-time";
import { StateDot } from "@/components/job-state-badge";
import { ErrorState, TableSkeleton } from "@/components/states";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { fmtDate, fmtRelative } from "@/lib/status";

export default function WorkerDetailPage() {
  const { id } = useParams<{ id: string }>();
  const router = useRouter();
  const projectId = useAuthStore((s) => s.projectId);
  const { data, error, mutate } = useWorker(id);
  const { data: running } = useAllJobs(projectId, "running", true);
  const now = useNow();

  const worker = data?.worker;
  const beats = useMemo(() => data?.heartbeats ?? [], [data]);

  const history = useMemo(
    () =>
      beats.slice(-24).map((h) => ({
        t: new Date(h.at).toLocaleTimeString("en-US", { hour: "2-digit", hour12: false }),
        completed: h.jobs_completed,
        running: h.jobs_running,
      })),
    [beats]
  );

  if (error) return <ErrorState message={error.message} onRetry={() => mutate()} />;
  if (!worker) return <TableSkeleton rows={4} cols={4} />;

  const stale = now - new Date(worker.last_heartbeat_at).getTime() > 30_000;
  const token = stale ? "--state-failed" : "--state-completed";
  const totalDone = beats.reduce((a, h) => a + h.jobs_completed, 0);

  const meta: [string, string][] = [
    ["Status", stale ? "Dead" : worker.status],
    ["Concurrency", String(worker.concurrency)],
    ["PID", String(worker.pid)],
    ["Version", worker.version || "-"],
    ["Registered", fmtDate(worker.registered_at)],
    ["Last heartbeat", fmtRelative(worker.last_heartbeat_at)],
  ];

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          <h1 className="flex items-center gap-2 truncate text-xl font-semibold tracking-tight">
            <span className="font-mono">{worker.hostname}</span>
            <Badge
              variant="outline" role="status" className="gap-1.5 rounded-full text-[10px]"
              style={{ borderColor: `color-mix(in oklab, var(${token}) 40%, transparent)`, color: `var(${token})` }}
            >
              <span className="size-1.5 rounded-full bg-current" aria-hidden="true" />
              {stale ? "DEAD" : "ACTIVE"}
            </Badge>
          </h1>
          <p className="font-mono text-xs text-muted-foreground">ID: {worker.id}</p>
        </div>
        <Tooltip>
          <TooltipTrigger
            render={
              <span>
                <Button size="sm" variant="destructive" className="rounded-lg" disabled aria-label="Force deregister worker">
                  <PowerOff className="size-3.5" aria-hidden="true" /> Force Deregister
                </Button>
              </span>
            }
          />
          <TooltipContent>Workers deregister themselves on graceful shutdown; the API exposes no forced removal</TooltipContent>
        </Tooltip>
      </div>

      <Card className="rounded-xl p-5">
        <dl className="grid grid-cols-2 gap-x-6 gap-y-4 sm:grid-cols-3">
          {meta.map(([k, v]) => (
            <div key={k}>
              <dt className="text-[10px] uppercase tracking-wide text-muted-foreground">{k}</dt>
              <dd className="mt-1 font-mono text-sm">{v}</dd>
            </div>
          ))}
          <div>
            <dt className="text-[10px] uppercase tracking-wide text-muted-foreground">Jobs completed</dt>
            <dd className="mt-1 font-mono text-sm tabular-nums">{totalDone}</dd>
          </div>
        </dl>
      </Card>

      <div className="grid gap-6 lg:grid-cols-2">
        <Card className="rounded-xl p-5">
          <h2 className="mb-3 text-sm font-semibold tracking-tight">Current Jobs</h2>
          {(running ?? []).length === 0 ? (
            <p className="text-xs text-muted-foreground">No jobs running right now.</p>
          ) : (
            <ul className="space-y-1">
              {(running ?? []).slice(0, 8).map((j) => (
                <li key={j.id}>
                  <button
                    onClick={() => router.push(`/jobs/${j.id}`)}
                    className="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-xs transition-colors hover:bg-accent focus-visible:ring-2 focus-visible:ring-primary"
                  >
                    <StateDot state={j.status} />
                    <span className="min-w-0 flex-1 truncate font-medium">{j.type}</span>
                    <Badge variant="outline" className="rounded-full text-[10px]">{j.queue_name}</Badge>
                  </button>
                </li>
              ))}
            </ul>
          )}
        </Card>

        <Card className="rounded-xl p-5">
          <h2 className="mb-3 text-sm font-semibold tracking-tight">Execution History</h2>
          {history.length === 0 ? (
            <p className="text-xs text-muted-foreground">No heartbeat history recorded.</p>
          ) : (
            <ResponsiveContainer width="100%" height={200}>
              <BarChart data={history} margin={{ top: 4, right: 8, bottom: 0, left: -18 }}>
                <CartesianGrid stroke="var(--border)" strokeDasharray="3 3" vertical={false} />
                <XAxis dataKey="t" tick={{ fontSize: 10, fill: "var(--muted-foreground)" }} tickLine={false} axisLine={false} />
                <YAxis tick={{ fontSize: 10, fill: "var(--muted-foreground)" }} tickLine={false} axisLine={false} width={40} />
                <RTooltip contentStyle={{ background: "var(--popover)", border: "1px solid var(--border)", borderRadius: 12, fontSize: 12 }} />
                <Bar dataKey="completed" fill="var(--state-completed)" radius={[4, 4, 0, 0]} />
              </BarChart>
            </ResponsiveContainer>
          )}
        </Card>
      </div>
    </div>
  );
}
