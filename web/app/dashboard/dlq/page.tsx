"use client";

import { useEffect, useState, useCallback } from "react";
import { dlq, queues, type DLQEntry, type Queue } from "@/lib/api";
import { useAuthStore } from "@/lib/auth-store";
import { Skeleton } from "@/components/ui/skeleton";
import { Button } from "@/components/ui/button";
import { fmtRelative, fmtDate } from "@/lib/status";
import {
  Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
} from "@/components/ui/select";
import { ArrowCounterClockwise, Trash, ArrowBendUpLeft } from "@phosphor-icons/react";

export default function DLQPage() {
  const projectId = useAuthStore((s) => s.projectId);
  const [queueList, setQueueList] = useState<Queue[]>([]);
  const [selectedQueue, setSelectedQueue] = useState<string>("");
  const [entries, setEntries] = useState<DLQEntry[]>([]);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(false);

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
      const res = await dlq.list(selectedQueue, { limit: 50 });
      setEntries(res.data);
      setTotal(res.total);
    } finally {
      setLoading(false);
    }
  }, [selectedQueue]);

  useEffect(() => { load(); }, [load]);

  async function retry(id: string) {
    await dlq.retry(id);
    load();
  }

  async function discard(id: string) {
    if (!confirm("Permanently discard this job?")) return;
    await dlq.discard(id);
    load();
  }

  return (
    <div className="p-6 max-w-5xl mx-auto space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-lg font-semibold">Dead Letter Queue</h1>
          <p className="text-xs text-muted-foreground mt-0.5">
            Jobs that exceeded their retry limit. {total > 0 && `${total} pending.`}
          </p>
        </div>
        <div className="flex items-center gap-2">
          <Select value={selectedQueue} onValueChange={(v) => v && setSelectedQueue(v)}>
            <SelectTrigger className="w-44">
              <SelectValue placeholder="Select queue" />
            </SelectTrigger>
            <SelectContent>
              {queueList.map((q) => (
                <SelectItem key={q.id} value={q.id}>{q.name}</SelectItem>
              ))}
            </SelectContent>
          </Select>
          <button onClick={load} className="text-muted-foreground hover:text-foreground transition-colors">
            <ArrowCounterClockwise size={16} />
          </button>
        </div>
      </div>

      {loading ? (
        <div className="space-y-2">
          {[...Array(5)].map((_, i) => <Skeleton key={i} className="h-20 rounded-lg" />)}
        </div>
      ) : (
        <div className="border border-border rounded-lg divide-y divide-border overflow-hidden">
          {entries.length === 0 && (
            <div className="px-4 py-12 text-center text-sm text-muted-foreground">
              No dead-letter jobs. Your workers are healthy.
            </div>
          )}
          {entries.map((e) => (
            <div key={e.id} className="px-4 py-4 hover:bg-accent/30 transition-colors">
              <div className="flex items-start justify-between gap-4">
                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-2 mb-1">
                    <span className="font-mono text-xs text-amber-400">{e.job_id.slice(0, 8)}</span>
                    <span className="text-xs text-muted-foreground">{e.attempts} attempts</span>
                    <span className="text-xs text-muted-foreground">moved {fmtRelative(e.moved_at)}</span>
                  </div>
                  {e.final_error && (
                    <p className="text-xs text-red-400 font-mono bg-red-500/10 px-2 py-1 rounded mt-1 truncate">
                      {e.final_error}
                    </p>
                  )}
                </div>
                <div className="flex items-center gap-1 shrink-0">
                  {!e.resolved_at && (
                    <>
                      <Button size="sm" variant="outline" onClick={() => retry(e.id)} className="h-7 text-xs gap-1">
                        <ArrowBendUpLeft size={12} /> Retry
                      </Button>
                      <Button size="sm" variant="outline" onClick={() => discard(e.id)}
                        className="h-7 text-xs gap-1 hover:text-destructive hover:border-destructive">
                        <Trash size={12} /> Discard
                      </Button>
                    </>
                  )}
                  {e.resolved_at && (
                    <span className="text-xs text-muted-foreground">Resolved {fmtDate(e.resolved_at)}</span>
                  )}
                </div>
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
