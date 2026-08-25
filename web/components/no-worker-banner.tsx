"use client";

import Link from "next/link";
import { AlertTriangle } from "lucide-react";
import { useAuthStore } from "@/lib/auth-store";
import { useWorkers } from "@/hooks/use-data";
import { useNow } from "@/hooks/use-elapsed-time";
import { Alert, AlertTitle, AlertDescription } from "@/components/ui/alert";

const STALE_MS = 30_000;

export function NoActiveWorkerBanner() {
  const projectId = useAuthStore((s) => s.projectId);
  const { data: workerList } = useWorkers(projectId);
  const now = useNow();

  if (!workerList) return null;

  const hasLiveWorker = workerList.some(
    (w) => now - new Date(w.last_heartbeat_at).getTime() <= STALE_MS
  );
  if (hasLiveWorker) return null;

  return (
    <Alert className="border-state-scheduled/40 bg-state-scheduled/10">
      <AlertTriangle className="text-state-scheduled" />
      <AlertTitle>No active worker for this project</AlertTitle>
      <AlertDescription>
        Jobs created here will sit in the queue indefinitely until a worker process is
        running and polling this project. <Link href="/workers">Check Worker Monitor</Link>.
      </AlertDescription>
    </Alert>
  );
}
