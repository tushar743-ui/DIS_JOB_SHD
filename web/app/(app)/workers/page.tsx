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
      <div className="flex flex-wrap gap-2 text-xs text-muted-foreground">
        <Badge variant="outline" className="rounded-full">{active.length} live</Badge>
        <Badge variant="outline" className="rounded-full">{workerList.length - active.length} stale</Badge>
        <span className="ml-auto">polling every 5s</span>
      </div>

      <div className="overflow-hidden rounded-xl border border-border">
        <div className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-border bg-muted/50 text-[10px] uppercase tracking-wide text-muted-foreground">
                <th className="h-9 px-3 text-left font-medium">Worker</th>
                <th className="h-9 px-3 text-left font-medium">Status</th>
                <th className="h-9 px-3 text-left font-medium">Current Jobs</th>
                <th className="h-9 px-3 text-left font-medium">PID</th>
                <th className="h-9 px-3 text-left font-medium">Last Heartbeat</th>
                <th className="h-9 px-3 text-left font-medium">Registered</th>
                <th className="h-9 w-10 px-3" />
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
                      "h-12 cursor-pointer border-b border-border transition-colors last:border-0 hover:bg-accent/40",
                      stale && "opacity-60"
                    )}
                  >
                    <td className="px-3">
                      <span className={cn("font-mono text-xs", stale && "line-through")}>{w.hostname}</span>
                      <span className="ml-2 font-mono text-[10px] text-muted-foreground">{w.id.slice(0, 8)}</span>
                    </td>
                    <td className="px-3">
                      <span
                        role="status"
                        aria-live="polite"
                        className="inline-flex items-center gap-1.5 text-xs"
                        style={{ color: `hsl(var(${token}))` }}
                      >
                        <span className="relative flex size-1.5">
                          {!stale && mine > 0 && (
                            <span className="absolute inline-flex size-full animate-ping rounded-full bg-current opacity-75" />
                          )}
                          <span className="relative inline-flex size-1.5 rounded-full bg-current" />
                        </span>
                        {label}
                      </span>
                    </td>
                    <td className="px-3 font-mono text-xs tabular-nums">
                      {stale ? "—" : `${Math.min(mine, w.concurrency)} / ${w.concurrency}`}
                    </td>
                    <td className="px-3 font-mono text-xs text-muted-foreground">{w.pid}</td>
                    <td className="px-3 text-xs">
                      <Tooltip>
                        <TooltipTrigger
                          render={<span className={stale ? "text-destructive" : "text-state-completed"}>{fmtRelative(w.last_heartbeat_at)}</span>}
                        />
                        <TooltipContent className="font-mono text-xs">{fmtDate(w.last_heartbeat_at)}</TooltipContent>
                      </Tooltip>
                    </td>
                    <td className="px-3 text-xs text-muted-foreground">{fmtRelative(w.registered_at)}</td>
                    <td className="px-3">
                      <DropdownMenu>
                        <DropdownMenuTrigger
                          render={
                            <button
                              aria-label={`Actions for worker ${w.hostname}`}
                              onClick={(e) => e.stopPropagation()}
                              className="grid size-7 place-items-center rounded-md text-muted-foreground hover:bg-accent hover:text-foreground focus-visible:ring-2 focus-visible:ring-primary"
                            >
                              <MoreVertical className="size-3.5" />
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
