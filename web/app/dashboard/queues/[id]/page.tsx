"use client";

import { useEffect, useState, useCallback } from "react";
import { useParams } from "next/navigation";
import Link from "next/link";
import {
  queues, jobs, dlq,
  type Queue, type Job, type DLQEntry, type QueueStats,
} from "@/lib/api";
import { StatusBadge } from "@/components/status-badge";
import { ErrorBanner } from "@/components/error-banner";
import { errMessage, reportError } from "@/lib/errors";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { fmtRelative, fmtDate } from "@/lib/status";
import { PauseCircle, PlayCircle, ArrowBendUpLeft, Trash, ArrowCounterClockwise } from "@phosphor-icons/react";

export default function QueueDetailPage() {
  const { id } = useParams<{ id: string }>();
  const [queue, setQueue] = useState<Queue | null>(null);
  const [stats, setStats] = useState<QueueStats | null>(null);
  const [jobList, setJobList] = useState<Job[]>([]);
  const [dlqList, setDlqList] = useState<DLQEntry[]>([]);
  const [loading, setLoading] = useState(true);
  const [acting, setActing] = useState(false);
  const [error, setError] = useState("");

  // State lands in the promise callbacks rather than an async try/catch so the
  // effect below can never trigger a synchronous re-render.
  const load = useCallback(() => {
    return Promise.all([
      queues.get(id),
      queues.stats(id),
      jobs.list(id, { limit: 50 }),
      dlq.list(id, { limit: 20 }),
    ]).then(
      ([q, s, j, d]) => {
        setQueue(q);
        setStats(s);
        setJobList(j.data);
        setDlqList(d.data);
        setError("");
        setLoading(false);
      },
      (e) => { setError(errMessage(e, "Failed to load queue")); setLoading(false); }
    );
  }, [id]);

  useEffect(() => { load(); }, [load]);

  async function togglePause() {
    if (!queue) return;
    setActing(true);
    try {
      if (queue.paused) await queues.resume(id);
      else await queues.pause(id);
      const q = await queues.get(id);
      setQueue(q);
    } catch (e) {
      reportError(e, queue.paused ? "Failed to resume queue" : "Failed to pause queue");
    } finally {
      setActing(false);
    }
  }

  async function retryDLQ(entryId: string) {
    try {
      await dlq.retry(entryId);
    } catch (e) {
      reportError(e, "Failed to retry job");
    }
    load();
  }

  async function discardDLQ(entryId: string) {
    if (!confirm("Permanently discard this job?")) return;
    try {
      await dlq.discard(entryId);
    } catch (e) {
      reportError(e, "Failed to discard job");
    }
    load();
  }

  if (loading) return (
    <div className="p-6 space-y-4">
      <Skeleton className="h-8 w-56" />
      <div className="grid grid-cols-4 gap-3">
        {[...Array(4)].map((_, i) => <Skeleton key={i} className="h-20 rounded-lg" />)}
      </div>
      <Skeleton className="h-64 rounded-lg" />
    </div>
  );

  if (!queue) return (
    <div className="p-6 max-w-5xl mx-auto space-y-4">
      {error
        ? <ErrorBanner message={error} />
        : <p className="text-center text-muted-foreground">Queue not found.</p>}
    </div>
  );

  const statCards = [
    { label: "Queued", value: stats?.by_status?.queued ?? 0, color: "text-amber-400" },
    { label: "Running", value: stats?.by_status?.running ?? 0, color: "text-blue-400" },
    { label: "Completed", value: stats?.by_status?.completed ?? 0, color: "text-emerald-400" },
    { label: "Failed", value: (stats?.by_status?.failed ?? 0) + (stats?.by_status?.dead ?? 0), color: "text-red-400" },
  ];

  return (
    <div className="p-6 max-w-5xl mx-auto space-y-6">
      <div className="flex items-start justify-between gap-4">
        <div>
          <div className="flex items-center gap-2 mb-1">
            <Link href="/dashboard/queues" className="text-muted-foreground hover:text-foreground text-sm">
              Queues
            </Link>
            <span className="text-muted-foreground text-sm">/</span>
            <span className="text-sm font-medium">{queue.name}</span>
          </div>
          <div className="flex items-center gap-3">
            <h1 className="text-lg font-semibold">{queue.name}</h1>
            {queue.paused && (
              <span className="text-xs px-2 py-0.5 rounded-full bg-amber-500/10 text-amber-400 border border-amber-500/20">
                Paused
              </span>
            )}
          </div>
          <p className="text-xs text-muted-foreground mt-0.5 font-mono">{queue.id}</p>
        </div>
        <div className="flex items-center gap-2">
          <button onClick={load} className="text-muted-foreground hover:text-foreground transition-colors p-1.5">
            <ArrowCounterClockwise size={16} />
          </button>
          <Button
            size="sm"
            variant="outline"
            onClick={togglePause}
            disabled={acting}
            className="gap-1.5"
          >
            {queue.paused
              ? <><PlayCircle size={14} /> Resume</>
              : <><PauseCircle size={14} /> Pause</>
            }
          </Button>
        </div>
      </div>

      <div className="border border-border rounded-lg p-4 bg-card grid grid-cols-2 md:grid-cols-4 gap-4 text-sm">
        {[
          ["Concurrency", queue.concurrency_limit],
          ["Priority", queue.priority],
          ["Created", fmtDate(queue.created_at)],
        ].map(([label, val]) => (
          <div key={String(label)}>
            <p className="text-xs text-muted-foreground">{label}</p>
            <p className="font-mono mt-0.5">{String(val)}</p>
          </div>
        ))}
      </div>

      <div className="grid grid-cols-2 md:grid-cols-4 gap-3">
        {statCards.map(({ label, value, color }) => (
          <div key={label} className="border border-border rounded-lg p-4 bg-card">
            <p className="text-xs text-muted-foreground">{label}</p>
            <p className={`text-2xl font-semibold font-mono mt-1 ${color}`}>{value.toLocaleString()}</p>
          </div>
        ))}
      </div>

      <Tabs defaultValue="jobs">
        <TabsList className="h-8">
          <TabsTrigger value="jobs" className="text-xs">Jobs ({jobList.length})</TabsTrigger>
          <TabsTrigger value="dlq" className="text-xs">Dead letter ({dlqList.length})</TabsTrigger>
        </TabsList>

        <TabsContent value="jobs" className="mt-3">
          <div className="border border-border rounded-lg divide-y divide-border overflow-hidden">
            {jobList.length === 0 && (
              <div className="px-4 py-10 text-center text-sm text-muted-foreground">
                No jobs in this queue.
              </div>
            )}
            {jobList.map((job) => (
              <Link
                key={job.id}
                href={`/dashboard/jobs/${job.id}`}
                className="flex items-center justify-between px-4 py-3 hover:bg-accent/30 transition-colors"
              >
                <div className="flex items-center gap-3 min-w-0">
                  <StatusBadge status={job.status} />
                  <span className="font-mono text-sm truncate">{job.type}</span>
                  <span className="text-xs text-muted-foreground font-mono hidden md:block">{job.id.slice(0, 8)}</span>
                </div>
                <div className="text-xs text-muted-foreground shrink-0">{fmtRelative(job.created_at)}</div>
              </Link>
            ))}
          </div>
        </TabsContent>

        <TabsContent value="dlq" className="mt-3">
          <div className="border border-border rounded-lg divide-y divide-border overflow-hidden">
            {dlqList.length === 0 && (
              <div className="px-4 py-10 text-center text-sm text-muted-foreground">
                No dead-letter jobs. Your workers are healthy.
              </div>
            )}
            {dlqList.map((e) => (
              <div key={e.id} className="px-4 py-4 hover:bg-accent/30 transition-colors">
                <div className="flex items-start justify-between gap-4">
                  <div className="min-w-0 flex-1">
                    <div className="flex items-center gap-2 mb-1">
                      <span className="font-mono text-xs text-amber-400">{e.job_id.slice(0, 8)}</span>
                      <span className="text-xs text-muted-foreground">{e.attempts} attempts</span>
                      <span className="text-xs text-muted-foreground">moved {fmtRelative(e.moved_at)}</span>
                    </div>
                    {e.final_error && (
                      <p className="text-xs text-red-400 font-mono bg-red-500/10 px-2 py-1 rounded truncate">
                        {e.final_error}
                      </p>
                    )}
                  </div>
                  {!e.resolved_at && (
                    <div className="flex items-center gap-1 shrink-0">
                      <Button size="sm" variant="outline" onClick={() => retryDLQ(e.id)} className="h-7 text-xs gap-1">
                        <ArrowBendUpLeft size={12} /> Retry
                      </Button>
                      <Button size="sm" variant="outline" onClick={() => discardDLQ(e.id)}
                        className="h-7 text-xs gap-1 hover:text-destructive hover:border-destructive">
                        <Trash size={12} /> Discard
                      </Button>
                    </div>
                  )}
                </div>
              </div>
            ))}
          </div>
        </TabsContent>
      </Tabs>
    </div>
  );
}
