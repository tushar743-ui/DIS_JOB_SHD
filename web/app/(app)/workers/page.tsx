"use client";

import { useRouter } from "next/navigation";
import { motion } from "framer-motion";
import { Cpu, MoreVertical } from "lucide-react";
import { useAuthStore } from "@/lib/auth-store";
import { useWorkers, useAllJobs } from "@/hooks/use-data";
import { useNow } from "@/hooks/use-elapsed-time";
import { EmptyState, ErrorState, TableSkeleton } from "@/components/states";
import { Badge } from "@/components/ui/badge";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import {
  DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { fmtDate, fmtRelative } from "@/lib/status";
import { cn } from "@/lib/utils";

const STALE_MS = 30_000;

export default function WorkerMonitorPage() {
  const router = useRouter();
  const projectId = useAuthStore((s) => s.projectId);
  const { data: workerList, error, isLoading, mutate } = useWorkers(projectId);
  const { data: runningJobs } = useAllJobs(projectId, "running", true);
  const now = useNow();

  if (error) return <ErrorState onRetry={() => mutate()} />;
  if (isLoading && !workerList) return <TableSkeleton rows={5} cols={7} />;
  if (!workerList?.length) {
    return (
      <EmptyState
        icon={Cpu}
        title="No workers registered"
        description="Start a worker process and it will register itself here within a heartbeat."
      />
    );
  }

  const active = workerList.filter((w) => now - new Date(w.last_heartbeat_at).getTime() <= STALE_MS);

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center gap-2">
        <Badge variant="outline" className="t-chip rounded-full px-2.5 py-1">
          <span className="font-mono tabular-nums">{active.length}</span> live
        </Badge>
        <Badge variant="outline" className="t-chip rounded-full px-2.5 py-1">
          <span className="font-mono tabular-nums">{workerList.length - active.length}</span> stale
        </Badge>
        <span className="t-meta ml-auto text-muted-foreground">polling every 5s</span>
      </div>

      <div className="overflow-hidden rounded-xl border border-border">
        <div className="overflow-x-auto">
          <table className="w-full">
            <thead>
              <tr className="t-label border-b border-border bg-muted/60 text-muted-foreground">
                <th className="h-11 px-4 text-left">Worker</th>
                <th className="h-11 px-4 text-left">Status</th>
                <th className="h-11 px-4 text-left">Current Jobs</th>
                <th className="h-11 px-4 text-left">PID</th>
                <th className="h-11 px-4 text-left">Last Heartbeat</th>
                <th className="h-11 px-4 text-left">Registered</th>
                <th className="h-11 w-12 px-4" />
              </tr>
            </thead>
            <tbody>
              {workerList.map((w, i) => {
                const stale = now - new Date(w.last_heartbeat_at).getTime() > STALE_MS;
                const mine = (runningJobs ?? []).length;
                const label = stale ? "Dead" : mine > 0 ? "Active" : "Idle";
                const token = stale ? "--state-failed" : mine > 0 ? "--state-completed" : "--state-cancelled";

                return (
                  <motion.tr
                    key={w.id}
                    initial={{ opacity: 0, x: -4 }}
                    animate={{ opacity: 1, x: 0 }}
                    transition={{ duration: 0.15, delay: Math.min(i, 10) * 0.02 }}
                    onClick={() => router.push(`/workers/${w.id}`)}
                    className={cn(
                      "h-14 cursor-pointer border-b border-border transition-colors last:border-0 hover:bg-accent/40",
                      stale && "opacity-60"
                    )}
                  >
                    <td className="px-4">
                      <div className="flex items-baseline gap-2.5">
                        <span className={cn("t-body font-semibold", stale && "line-through decoration-1")}>{w.hostname}</span>
                        <span className="t-data text-[0.75rem] font-normal text-muted-foreground">{w.id.slice(0, 8)}</span>
                      </div>
                    </td>
                    <td className="px-4">
                      <span
                        role="status"
                        aria-live="polite"
                        className="t-chip inline-flex items-center gap-2"
                        style={{ color: `var(${token})` }}
                      >
                        <span className="relative flex size-2">
                          {!stale && mine > 0 && (
                            <span className="absolute inline-flex size-full animate-ping rounded-full bg-current opacity-75" />
                          )}
                          <span className="relative inline-flex size-2 rounded-full bg-current" />
                        </span>
                        {label}
                      </span>
                    </td>
                    <td className="t-data px-4">
                      {stale ? <span className="text-muted-foreground">-</span> : (
                        <>
                          <span className="font-semibold text-foreground">{Math.min(mine, w.concurrency)}</span>
                          <span className="mx-1 text-muted-foreground">/</span>
                          <span className="text-muted-foreground">{w.concurrency}</span>
                        </>
                      )}
                    </td>
                    <td className="t-data px-4 font-normal text-muted-foreground">{w.pid}</td>
                    <td className="t-meta px-4">
                      <Tooltip>
                        <TooltipTrigger
                          render={<span className={cn("font-medium", stale ? "text-destructive" : "text-state-completed")}>{fmtRelative(w.last_heartbeat_at)}</span>}
                        />
                        <TooltipContent className="t-data">{fmtDate(w.last_heartbeat_at)}</TooltipContent>
                      </Tooltip>
                    </td>
                    <td className="t-meta px-4 text-muted-foreground">{fmtRelative(w.registered_at)}</td>
                    <td className="px-4">
                      <DropdownMenu>
                        <DropdownMenuTrigger
                          render={
                            <button
                              aria-label={`Actions for worker ${w.hostname}`}
                              onClick={(e) => e.stopPropagation()}
                              className="grid size-8 place-items-center rounded-md text-muted-foreground hover:bg-accent hover:text-foreground focus-visible:ring-2 focus-visible:ring-primary"
                            >
                              <MoreVertical className="size-4" />
                            </button>
                          }
                        />
                        <DropdownMenuContent align="end">
                          <DropdownMenuItem onClick={() => router.push(`/workers/${w.id}`)}>View details</DropdownMenuItem>
                        </DropdownMenuContent>
                      </DropdownMenu>
                    </td>
                  </motion.tr>
                );
              })}
            </tbody>
          </table>
        </div>
      </div>
    </div>
  );
}
