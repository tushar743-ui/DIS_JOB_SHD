"use client";

import { useEffect, useState, useCallback } from "react";
import { workers, type Worker } from "@/lib/api";
import { useAuthStore } from "@/lib/auth-store";
import { StatusBadge } from "@/components/status-badge";
import { ErrorBanner } from "@/components/error-banner";
import { Skeleton } from "@/components/ui/skeleton";
import { errMessage } from "@/lib/errors";
import { fmtRelative } from "@/lib/status";
import { ArrowCounterClockwise } from "@phosphor-icons/react";

export default function WorkersPage() {
  const projectId = useAuthStore((s) => s.projectId);
  const [list, setList] = useState<Worker[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");

  // State lands in the promise callbacks rather than an async try/catch so the
  // effect below can never trigger a synchronous re-render.
  const load = useCallback(() => {
    if (!projectId) return;
    return workers.list(projectId).then(
      (data) => { setList(data); setError(""); setLoading(false); },
      (e) => { setError(errMessage(e, "Failed to load workers")); setLoading(false); }
    );
  }, [projectId]);

  useEffect(() => {
    load();
    const t = setInterval(load, 10_000);
    return () => clearInterval(t);
  }, [load]);

  const active = list.filter((w) => w.status === "active");
  const draining = list.filter((w) => w.status === "draining");
  const offline = list.filter((w) => w.status === "offline");

  return (
    <div className="p-6 max-w-5xl mx-auto space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-lg font-semibold">Workers</h1>
        <button onClick={load} className="text-muted-foreground hover:text-foreground transition-colors">
          <ArrowCounterClockwise size={16} />
        </button>
      </div>

      <ErrorBanner message={error} />

      <div className="grid grid-cols-3 gap-4">
        {[
          { label: "Active", count: active.length, color: "text-emerald-400" },
          { label: "Draining", count: draining.length, color: "text-amber-400" },
          { label: "Offline", count: offline.length, color: "text-zinc-400" },
        ].map(({ label, count, color }) => (
          <div key={label} className="border border-border rounded-lg p-4 bg-card">
            <p className="text-xs text-muted-foreground">{label}</p>
            <p className={`text-2xl font-semibold font-mono mt-1 ${color}`}>{count}</p>
          </div>
        ))}
      </div>

      {loading ? (
        <div className="space-y-2">
          {[...Array(3)].map((_, i) => <Skeleton key={i} className="h-20 rounded-lg" />)}
        </div>
      ) : (
        <div className="border border-border rounded-lg divide-y divide-border overflow-hidden">
          {list.length === 0 && (
            <div className="px-4 py-12 text-center text-sm text-muted-foreground">
              No workers registered. Start a worker process to see it here.
            </div>
          )}
          {list.map((w) => (
            <div key={w.id} className="px-4 py-4 hover:bg-accent/30 transition-colors">
              <div className="flex items-start justify-between gap-4">
                <div className="min-w-0">
                  <div className="flex items-center gap-2 mb-1">
                    <StatusBadge status={w.status} />
                    <span className="text-sm font-medium font-mono">{w.hostname}</span>
                    <span className="text-xs text-muted-foreground">PID {w.pid}</span>
                  </div>
                  <div className="flex items-center gap-4 text-xs text-muted-foreground font-mono">
                    <span>ID: {w.id.slice(0, 8)}</span>
                    <span>concurrency: {w.concurrency}</span>
                    {w.version && <span>v{w.version}</span>}
                  </div>
                </div>
                <div className="text-right text-xs text-muted-foreground shrink-0">
                  <p>Last beat</p>
                  <p className={w.status === "active" ? "text-emerald-400" : ""}>
                    {fmtRelative(w.last_heartbeat_at)}
                  </p>
                </div>
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
