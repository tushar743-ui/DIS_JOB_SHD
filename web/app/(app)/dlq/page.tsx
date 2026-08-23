"use client";

import { AlertTriangle, RotateCcw, ShieldCheck, Trash2 } from "lucide-react";
import { useAuthStore } from "@/lib/auth-store";
import { useAllDLQ } from "@/hooks/use-data";
import { dlq as dlqApi } from "@/lib/api";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { EmptyState, ErrorState, TableSkeleton } from "@/components/states";
import { reportError } from "@/lib/errors";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { ShimmerButton } from "@/components/ui/shimmer-button";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { fmtDate, fmtRelative } from "@/lib/status";

export default function DLQPage() {
  const projectId = useAuthStore((s) => s.projectId);
  const { data: entries, error, isLoading, mutate } = useAllDLQ(projectId);

  const pending = (entries ?? []).filter((e) => !e.resolved_at);

  async function retryAll() {
    const results = await Promise.allSettled(pending.map((e) => dlqApi.retry(e.id)));
    const failed = results.filter((r) => r.status === "rejected").length;
    if (failed) reportError(new Error(`${failed} of ${pending.length} could not be re-enqueued`), "Bulk retry incomplete");
    mutate();
  }

  if (error) return <ErrorState onRetry={() => mutate()} />;

  return (
    <div className="space-y-4">
      <Alert className="rounded-xl border-state-retrying/40 text-state-retrying">
        <AlertTriangle className="size-4" aria-hidden="true" />
        <AlertTitle className="tracking-tight">
          Dead Letter Queue - {pending.length} job{pending.length === 1 ? "" : "s"} permanently failed
        </AlertTitle>
        <AlertDescription className="text-muted-foreground">
          These jobs exceeded their max retry attempts. Review the error and retry manually once the cause is fixed.
        </AlertDescription>
      </Alert>

      {pending.length > 0 && (
        <div className="flex justify-end">
          <ConfirmDialog
            title={`Retry all ${pending.length} dead-letter jobs?`}
            description="Every pending job in the dead letter queue will be re-enqueued with a reset attempt count."
            confirmLabel="Retry all"
            onConfirm={retryAll}
            trigger={<ShimmerButton className="h-9 px-4 text-sm">Retry All</ShimmerButton>}
          />
        </div>
      )}

      {isLoading && !entries ? (
        <TableSkeleton rows={5} cols={6} />
      ) : pending.length === 0 ? (
        <EmptyState
          icon={ShieldCheck}
          title="Dead letter queue is empty"
          description="Nothing has exhausted its retries. Your workers are healthy."
        />
      ) : (
        <div className="overflow-hidden rounded-xl border border-border">
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-border bg-muted/50 text-[10px] uppercase tracking-wide text-muted-foreground">
                  <th className="h-9 px-3 text-left font-medium">Job</th>
                  <th className="h-9 px-3 text-left font-medium">Last Error</th>
                  <th className="h-9 px-3 text-left font-medium">Attempts</th>
                  <th className="h-9 px-3 text-left font-medium">Failed At</th>
                  <th className="h-9 px-3 text-left font-medium">Queue</th>
                  <th className="h-9 px-3 text-right font-medium">Actions</th>
                </tr>
              </thead>
              <tbody>
                {pending.map((e) => (
                  <tr key={e.id} className="h-12 border-b border-border transition-colors last:border-0 hover:bg-accent/40">
                    <td className="px-3 font-mono text-xs">{e.job_id.slice(0, 8)}</td>
                    <td className="max-w-[280px] px-3">
                      <Tooltip>
                        <TooltipTrigger
                          render={
                            <span className="block truncate font-mono text-xs text-destructive">
                              {e.final_error || "-"}
                            </span>
                          }
                        />
                        <TooltipContent className="max-w-sm break-all font-mono text-xs">
                          {e.final_error || "No error recorded"}
                        </TooltipContent>
                      </Tooltip>
                    </td>
                    <td className="px-3 font-mono text-xs tabular-nums">{e.attempts}</td>
                    <td className="px-3 text-xs text-muted-foreground">
                      <Tooltip>
                        <TooltipTrigger render={<span>{fmtRelative(e.moved_at)}</span>} />
                        <TooltipContent className="font-mono text-xs">{fmtDate(e.moved_at)}</TooltipContent>
                      </Tooltip>
                    </td>
                    <td className="px-3">
                      <Badge variant="outline" className="rounded-full text-[10px]">{e.queue_name}</Badge>
                    </td>
                    <td className="px-3">
                      <div className="flex justify-end gap-1">
                        <Button
                          size="sm" variant="outline" className="rounded-lg text-xs"
                          aria-label={`Retry job ${e.job_id}`}
                          onClick={async () => {
                            try { await dlqApi.retry(e.id); }
                            catch (err) { reportError(err, "Failed to retry job"); }
                            mutate();
                          }}
                        >
                          <RotateCcw className="size-3.5" aria-hidden="true" />
                        </Button>
                        <ConfirmDialog
                          title="Discard this job permanently?"
                          description={`Job ${e.job_id} will be removed from the dead letter queue. This cannot be undone.`}
                          confirmLabel="Discard"
                          onConfirm={async () => {
                            try { await dlqApi.discard(e.id); }
                            catch (err) { reportError(err, "Failed to discard job"); }
                            mutate();
                          }}
                          trigger={
                            <Button
                              size="sm" variant="ghost"
                              className="rounded-lg text-xs text-muted-foreground hover:text-destructive"
                              aria-label={`Discard job ${e.job_id}`}
                            >
                              <Trash2 className="size-3.5" aria-hidden="true" />
                            </Button>
                          }
                        />
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      )}
    </div>
  );
}
