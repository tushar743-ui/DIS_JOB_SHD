export const CANCELLABLE = ["queued", "scheduled"];
export const RETRYABLE = ["failed", "dead", "cancelled"];

export function canCancel(status: string): boolean {
  return CANCELLABLE.includes(status);
}

export function canRetry(status: string): boolean {
  return RETRYABLE.includes(status);
}

export function cancelHint(status: string): string {
  return canCancel(status)
    ? "Stop this job before a worker claims it"
    : `Only queued or scheduled jobs can be cancelled - this one is ${status}`;
}

export function retryHint(status: string): string {
  return canRetry(status)
    ? "Re-enqueue with a reset attempt count"
    : `Only failed, dead or cancelled jobs can be retried - this one is ${status}`;
}

export type JobAction = "retry" | "cancel" | "delete";

export function applyJobAction<T extends { id: string; status: string; attempt_count: number }>(
  action: JobAction,
  ids: Set<string>
) {
  return (current: T[] | undefined): T[] =>
    (current ?? []).flatMap((job) => {
      if (!ids.has(job.id)) return [job];
      if (action === "delete") return [];
      if (action === "retry") return [{ ...job, status: "queued", attempt_count: 0 }];
      return [{ ...job, status: "cancelled" }];
    });
}
