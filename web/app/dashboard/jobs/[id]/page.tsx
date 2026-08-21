"use client";

import { useEffect, useState } from "react";
import { useParams } from "next/navigation";
import { jobs, type Job, type JobLog, type JobExecution } from "@/lib/api";
import { StatusBadge } from "@/components/status-badge";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { fmtDate, fmtRelative, fmtDuration } from "@/lib/status";
import { ArrowBendUpLeft, X } from "@phosphor-icons/react";
import Link from "next/link";

export default function JobDetailPage() {
  const { id } = useParams<{ id: string }>();
  const [job, setJob] = useState<Job | null>(null);
  const [logs, setLogs] = useState<JobLog[]>([]);
  const [executions, setExecutions] = useState<JobExecution[]>([]);
  const [loading, setLoading] = useState(true);
  const [acting, setActing] = useState(false);

  useEffect(() => {
    Promise.all([
      jobs.get(id),
      jobs.logs(id, { limit: 100 }),
      jobs.executions(id),
    ]).then(([j, l, e]) => {
      setJob(j);
      setLogs(l);
      setExecutions(e);
    }).finally(() => setLoading(false));
  }, [id]);

  async function retry() {
    setActing(true);
    try { await jobs.retry(id); const j = await jobs.get(id); setJob(j); }
    finally { setActing(false); }
  }

  async function cancel() {
    setActing(true);
    try { await jobs.cancel(id); const j = await jobs.get(id); setJob(j); }
    finally { setActing(false); }
  }

  if (loading) return (
    <div className="p-6 space-y-4">
      <Skeleton className="h-8 w-48" />
      <Skeleton className="h-32 rounded-lg" />
      <Skeleton className="h-64 rounded-lg" />
    </div>
  );

  if (!job) return (
    <div className="p-6 text-center text-muted-foreground">Job not found.</div>
  );

  return (
    <div className="p-6 max-w-4xl mx-auto space-y-6">
      <div className="flex items-start justify-between gap-4">
        <div>
          <div className="flex items-center gap-2 mb-1">
            <Link href="/dashboard/jobs" className="text-muted-foreground hover:text-foreground text-sm">
              Jobs
            </Link>
            <span className="text-muted-foreground text-sm">/</span>
            <span className="font-mono text-sm text-amber-400">{job.id.slice(0, 8)}</span>
          </div>
          <div className="flex items-center gap-3">
            <h1 className="text-lg font-semibold font-mono">{job.type}</h1>
            <StatusBadge status={job.status} />
          </div>
          <p className="text-xs text-muted-foreground mt-1 font-mono">{job.id}</p>
        </div>
        <div className="flex items-center gap-2">
          {["failed","dead","cancelled"].includes(job.status) && (
            <Button size="sm" onClick={retry} disabled={acting} className="gap-1.5 bg-amber-500 hover:bg-amber-400 text-black">
              <ArrowBendUpLeft size={13} /> Retry
            </Button>
          )}
          {["queued","scheduled"].includes(job.status) && (
            <Button size="sm" variant="outline" onClick={cancel} disabled={acting}
              className="gap-1.5 hover:text-destructive hover:border-destructive">
              <X size={13} /> Cancel
            </Button>
          )}
        </div>
      </div>

      <div className="grid grid-cols-2 md:grid-cols-3 gap-3">
        {[
          ["Queue", job.queue_id.slice(0, 8)],
          ["Priority", job.priority],
          ["Attempts", `${job.attempt_count}/${job.max_attempts}`],
          ["Created", fmtDate(job.created_at)],
          ["Run at", fmtDate(job.run_at)],
          ["Completed", fmtDate(job.completed_at)],
          ["Timeout", `${job.timeout_secs}s`],
          job.cron_expression ? ["Cron", job.cron_expression] : null,
          job.batch_id ? ["Batch", job.batch_id.slice(0, 8)] : null,
        ].filter((x): x is [string, string | number] => x !== null).map(([label, value]) => (
          <div key={String(label)} className="border border-border rounded-lg p-3 bg-card">
            <p className="text-xs text-muted-foreground">{label}</p>
            <p className="text-sm font-mono mt-0.5 truncate">{String(value)}</p>
          </div>
        ))}
      </div>

      {job.last_error && (
        <div className="border border-red-500/30 bg-red-500/5 rounded-lg p-4">
          <p className="text-xs text-red-400 font-medium mb-1">Last error</p>
          <p className="text-sm font-mono text-red-300 whitespace-pre-wrap break-all">{job.last_error}</p>
        </div>
      )}

      <Tabs defaultValue="payload">
        <TabsList className="h-8">
          <TabsTrigger value="payload" className="text-xs">Payload</TabsTrigger>
          <TabsTrigger value="executions" className="text-xs">Executions ({executions.length})</TabsTrigger>
          <TabsTrigger value="logs" className="text-xs">Logs ({logs.length})</TabsTrigger>
        </TabsList>

        <TabsContent value="payload" className="mt-3">
          <div className="border border-border rounded-lg bg-card p-4 overflow-auto max-h-96 scrollbar-thin">
            <pre className="text-xs font-mono text-foreground whitespace-pre-wrap break-all">
              {JSON.stringify(job.payload, null, 2)}
            </pre>
          </div>
        </TabsContent>

        <TabsContent value="executions" className="mt-3">
          <div className="border border-border rounded-lg divide-y divide-border overflow-hidden">
            {executions.length === 0 && (
              <div className="px-4 py-8 text-center text-sm text-muted-foreground">No executions recorded.</div>
            )}
            {executions.map((ex) => (
              <div key={ex.id} className="px-4 py-3 flex items-center justify-between gap-4">
                <div className="flex items-center gap-3">
                  <StatusBadge status={ex.status} />
                  <span className="text-xs text-muted-foreground">attempt {ex.attempt_number}</span>
                </div>
                <div className="flex items-center gap-4 text-xs text-muted-foreground font-mono">
                  <span>{fmtDate(ex.started_at)}</span>
                  <span>{fmtDuration(ex.duration_ms)}</span>
                </div>
                {ex.error_message && (
                  <p className="text-xs text-red-400 truncate max-w-xs">{ex.error_message}</p>
                )}
              </div>
            ))}
          </div>
        </TabsContent>

        <TabsContent value="logs" className="mt-3">
          <div className="border border-border rounded-lg bg-card overflow-auto max-h-96 scrollbar-thin">
            {logs.length === 0 && (
              <div className="px-4 py-8 text-center text-sm text-muted-foreground">No logs recorded.</div>
            )}
            {logs.map((l) => (
              <div key={l.id} className={[
                "px-4 py-2 border-b border-border/50 flex items-start gap-3 text-xs font-mono",
                l.level === "error" ? "bg-red-500/5" : "",
                l.level === "warn" ? "bg-amber-500/5" : "",
              ].join(" ")}>
                <span className={[
                  "shrink-0 w-10",
                  l.level === "error" ? "text-red-400" : "",
                  l.level === "warn" ? "text-amber-400" : "",
                  l.level === "info" ? "text-blue-400" : "",
                  l.level === "debug" ? "text-zinc-500" : "",
                ].join(" ")}>{l.level}</span>
                <span className="text-muted-foreground shrink-0">{fmtRelative(l.logged_at)}</span>
                <span className="text-foreground break-all">{l.message}</span>
              </div>
            ))}
          </div>
        </TabsContent>
      </Tabs>
    </div>
  );
}
