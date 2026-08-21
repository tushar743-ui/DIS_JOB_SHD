"use client";

import { useEffect, useState, useCallback } from "react";
import { jobs, queues, type Job, type Queue } from "@/lib/api";
import { useAuthStore } from "@/lib/auth-store";
import { StatusBadge } from "@/components/status-badge";
import { Skeleton } from "@/components/ui/skeleton";
import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";
import { fmtDate, fmtRelative, fmtDuration } from "@/lib/status";
import {
  Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
} from "@/components/ui/select";
import { ArrowCounterClockwise, ArrowLeft, ArrowRight } from "@phosphor-icons/react";
import Link from "next/link";

const STATUS_OPTIONS = ["all","queued","scheduled","running","completed","failed","dead","cancelled"];
const PAGE_SIZE = 25;

export default function JobsPage() {
  const projectId = useAuthStore((s) => s.projectId);
  const [queueList, setQueueList] = useState<Queue[]>([]);
  const [selectedQueue, setSelectedQueue] = useState<string>("");
  const [status, setStatus] = useState("all");
  const [jobList, setJobList] = useState<Job[]>([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(0);
  const [loading, setLoading] = useState(false);
  const [search, setSearch] = useState("");

  useEffect(() => {
    if (!projectId) return;
    queues.list(projectId).then((q) => {
      setQueueList(q);
      if (q.length > 0) setSelectedQueue(q[0].id);
    });
  }, [projectId]);

  const load = useCallback(async () => {
    if (!selectedQueue) return;
    setLoading(true);
    try {
      const res = await jobs.list(selectedQueue, {
        limit: PAGE_SIZE,
        offset: page * PAGE_SIZE,
        status: status !== "all" ? status : undefined,
      });
      setJobList(res.data);
      setTotal(res.total);
    } finally {
      setLoading(false);
    }
  }, [selectedQueue, status, page]);

  useEffect(() => { load(); }, [load]);

  const filtered = search
    ? jobList.filter(j =>
        j.id.includes(search) ||
        j.type.toLowerCase().includes(search.toLowerCase()) ||
        j.tags?.some(t => t.includes(search))
      )
    : jobList;

  const totalPages = Math.ceil(total / PAGE_SIZE);

  return (
    <div className="p-6 space-y-4">
      <div className="flex items-center justify-between">
        <h1 className="text-lg font-semibold">Jobs</h1>
        <button onClick={load} className="text-muted-foreground hover:text-foreground transition-colors">
          <ArrowCounterClockwise size={16} />
        </button>
      </div>

      <div className="flex items-center gap-3 flex-wrap">
        <Select value={selectedQueue} onValueChange={(v) => { if (v) { setSelectedQueue(v); setPage(0); } }}>
          <SelectTrigger className="w-48">
            <SelectValue placeholder="Select queue" />
          </SelectTrigger>
          <SelectContent>
            {queueList.map((q) => (
              <SelectItem key={q.id} value={q.id}>{q.name}</SelectItem>
            ))}
          </SelectContent>
        </Select>

        <Select value={status} onValueChange={(v) => { if (v) { setStatus(v); setPage(0); } }}>
          <SelectTrigger className="w-36">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {STATUS_OPTIONS.map((s) => (
              <SelectItem key={s} value={s}>{s === "all" ? "All statuses" : s}</SelectItem>
            ))}
          </SelectContent>
        </Select>

        <Input
          placeholder="Search by ID, type, tag..."
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          className="w-64 font-mono text-sm"
        />
      </div>

      <div className="border border-border rounded-lg overflow-hidden">
        <div className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-border bg-muted/50 text-xs text-muted-foreground uppercase tracking-wide">
                <th className="px-4 py-2.5 text-left font-medium">Job ID</th>
                <th className="px-4 py-2.5 text-left font-medium">Type</th>
                <th className="px-4 py-2.5 text-left font-medium">Status</th>
                <th className="px-4 py-2.5 text-left font-medium">Attempts</th>
                <th className="px-4 py-2.5 text-left font-medium">Created</th>
                <th className="px-4 py-2.5 text-left font-medium">Actions</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-border">
              {loading ? (
                [...Array(8)].map((_, i) => (
                  <tr key={i}>
                    {[...Array(6)].map((_, j) => (
                      <td key={j} className="px-4 py-3">
                        <Skeleton className="h-4 w-full" />
                      </td>
                    ))}
                  </tr>
                ))
              ) : filtered.length === 0 ? (
                <tr>
                  <td colSpan={6} className="px-4 py-12 text-center text-muted-foreground">
                    No jobs match the current filters.
                  </td>
                </tr>
              ) : (
                filtered.map((j) => (
                  <JobRow key={j.id} job={j} onAction={load} />
                ))
              )}
            </tbody>
          </table>
        </div>

        {total > PAGE_SIZE && (
          <div className="px-4 py-3 border-t border-border flex items-center justify-between text-xs text-muted-foreground">
            <span>{total} total · page {page + 1} of {totalPages}</span>
            <div className="flex items-center gap-1">
              <Button size="sm" variant="outline" onClick={() => setPage(p => Math.max(0, p - 1))} disabled={page === 0}>
                <ArrowLeft size={13} />
              </Button>
              <Button size="sm" variant="outline" onClick={() => setPage(p => Math.min(totalPages - 1, p + 1))} disabled={page >= totalPages - 1}>
                <ArrowRight size={13} />
              </Button>
            </div>
          </div>
        )}
      </div>
    </div>
  );
}

function JobRow({ job: j, onAction }: { job: Job; onAction: () => void }) {
  const [acting, setActing] = useState(false);

  async function retry() {
    setActing(true);
    try { await jobs.retry(j.id); onAction(); }
    finally { setActing(false); }
  }

  async function cancel() {
    setActing(true);
    try { await jobs.cancel(j.id); onAction(); }
    finally { setActing(false); }
  }

  return (
    <tr className="hover:bg-accent/30 transition-colors">
      <td className="px-4 py-3">
        <Link href={`/dashboard/jobs/${j.id}`} className="font-mono text-xs text-amber-400 hover:underline">
          {j.id.slice(0, 8)}
        </Link>
      </td>
      <td className="px-4 py-3">
        <span className="font-mono text-xs bg-secondary px-1.5 py-0.5 rounded">{j.type}</span>
      </td>
      <td className="px-4 py-3">
        <StatusBadge status={j.status} />
      </td>
      <td className="px-4 py-3 font-mono text-xs text-muted-foreground">
        {j.attempt_count}/{j.max_attempts}
      </td>
      <td className="px-4 py-3 text-xs text-muted-foreground whitespace-nowrap">
        {fmtRelative(j.created_at)}
      </td>
      <td className="px-4 py-3">
        <div className="flex items-center gap-1">
          {["failed","dead","cancelled"].includes(j.status) && (
            <Button size="sm" variant="outline" onClick={retry} disabled={acting} className="h-6 text-xs px-2">
              Retry
            </Button>
          )}
          {["queued","scheduled"].includes(j.status) && (
            <Button size="sm" variant="outline" onClick={cancel} disabled={acting} className="h-6 text-xs px-2 hover:text-destructive hover:border-destructive">
              Cancel
            </Button>
          )}
        </div>
      </td>
    </tr>
  );
}
