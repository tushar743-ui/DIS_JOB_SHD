"use client";

import { useEffect, useState, useCallback } from "react";
import { metrics, type ProjectMetrics } from "@/lib/api";
import { useAuthStore } from "@/lib/auth-store";
import { StatusBadge } from "@/components/status-badge";
import { ErrorBanner } from "@/components/error-banner";
import { errMessage } from "@/lib/errors";
import { Skeleton } from "@/components/ui/skeleton";
import { fmtRelative } from "@/lib/status";
import { ArrowCounterClockwise, Robot, CheckCircle, Stack } from "@phosphor-icons/react";

export default function OverviewPage() {
  const projectId = useAuthStore((s) => s.projectId);
  const [data, setData] = useState<ProjectMetrics | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");

  // State lands in the promise callbacks rather than an async try/catch so the
  // effect below can never trigger a synchronous re-render.
  const load = useCallback(() => {
    if (!projectId) return;
    return metrics.project(projectId).then(
      (m) => { setData(m); setError(""); setLoading(false); },
      (e) => { setError(errMessage(e, "Failed to load metrics")); setLoading(false); }
    );
  }, [projectId]);

  useEffect(() => {
    load();
    const t = setInterval(load, 15_000);
    return () => clearInterval(t);
  }, [load]);

  const totalCompleted = data?.completed_24h ?? 0;
  const totalQueued = data?.queues?.reduce(
    (sum, q) => sum + (q.by_status.queued ?? 0), 0
  ) ?? 0;
  const totalFailed = data?.queues?.reduce(
    (sum, q) => sum + (q.by_status.failed ?? 0) + (q.by_status.dead ?? 0), 0
  ) ?? 0;

  return (
    <div className="p-6 max-w-5xl mx-auto space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-lg font-semibold">Overview</h1>
          {data && (
            <p className="text-xs text-muted-foreground mt-0.5">
              Updated {fmtRelative(data.generated_at)}
            </p>
          )}
        </div>
        <button
          onClick={load}
          className="text-muted-foreground hover:text-foreground transition-colors"
          title="Refresh"
        >
          <ArrowCounterClockwise size={16} />
        </button>
      </div>

      {loading ? (
        <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
          {[...Array(4)].map((_, i) => (
            <Skeleton key={i} className="h-24 rounded-lg" />
          ))}
        </div>
      ) : (
        <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
          <KPICard
            label="Active workers"
            value={data?.active_workers ?? 0}
            icon={<Robot size={18} weight="fill" className="text-emerald-400" />}
          />
          <KPICard
            label="Completed (24h)"
            value={totalCompleted}
            icon={<CheckCircle size={18} weight="fill" className="text-blue-400" />}
          />
          <KPICard
            label="Queued"
            value={totalQueued}
            icon={<Stack size={18} weight="fill" className="text-amber-400" />}
            accent="amber"
          />
          <KPICard
            label="Failed / Dead"
            value={totalFailed}
            icon={<Stack size={18} weight="fill" className="text-red-400" />}
            accent={totalFailed > 0 ? "red" : undefined}
          />
        </div>
      )}

      <ErrorBanner message={error} />

      <div>
        <h2 className="text-sm font-medium text-muted-foreground mb-3 uppercase tracking-wide text-[11px]">
          Queues
        </h2>
        {loading ? (
          <div className="space-y-2">
            {[...Array(3)].map((_, i) => <Skeleton key={i} className="h-14 rounded-lg" />)}
          </div>
        ) : (
          <div className="border border-border rounded-lg divide-y divide-border overflow-hidden">
            {data?.queues?.length === 0 && (
              <div className="px-4 py-10 text-center text-sm text-muted-foreground">
                No queues yet. Create one in the Queues section.
              </div>
            )}
            {data?.queues?.map((q) => {
              const total = Object.values(q.by_status).reduce((s, n) => s + n, 0);
              return (
                <div key={q.queue_id} className="px-4 py-3 flex items-center gap-4 hover:bg-accent/50 transition-colors">
                  <div className="flex-1 min-w-0">
                    <p className="text-sm font-medium truncate">{q.queue_name}</p>
                    <p className="text-xs text-muted-foreground font-mono">{q.queue_id.slice(0, 8)}</p>
                  </div>
                  <div className="flex items-center gap-2 flex-wrap justify-end">
                    {(["running","queued","completed","failed","dead"] as const).map((st) =>
                      q.by_status[st] ? (
                        <StatusBadge key={st} status={st} />
                      ) : null
                    )}
                  </div>
                  <div className="text-xs text-muted-foreground font-mono w-12 text-right">{total} total</div>
                </div>
              );
            })}
          </div>
        )}
      </div>
    </div>
  );
}

function KPICard({
  label, value, icon, accent,
}: {
  label: string; value: number; icon: React.ReactNode; accent?: "amber" | "red";
}) {
  return (
    <div className={[
      "border border-border rounded-lg p-4 bg-card",
      accent === "amber" && value > 0 ? "border-amber-500/30" : "",
      accent === "red" && value > 0 ? "border-red-500/30" : "",
    ].join(" ")}>
      <div className="flex items-center justify-between mb-2">
        <span className="text-xs text-muted-foreground">{label}</span>
        {icon}
      </div>
      <p className="text-2xl font-semibold font-mono tabular-nums">{value.toLocaleString()}</p>
    </div>
  );
}
