import { toast } from "@/components/ui/toast";

/** The message api.ts throws when a refresh failed and the redirect is running. */
const SESSION_EXPIRED = "session expired";

export function errMessage(e: unknown, fallback: string): string {
  const msg = e instanceof Error ? e.message : fallback;
  return msg === SESSION_EXPIRED ? "" : msg;
}

let lastToast = "";
let lastToastAt = 0;

/**
 * Surface a failed action. Polling screens can fail repeatedly against the same
 * dead backend, so an identical message is only shown once per window.
 */
export function reportError(e: unknown, fallback: string) {
  const description = errMessage(e, fallback);
  if (!description) return;

  const now = Date.now();
  if (description === lastToast && now - lastToastAt < 30_000) return;
  lastToast = description;
  lastToastAt = now;

  toast.add({ title: fallback, description, type: "error" });
}
