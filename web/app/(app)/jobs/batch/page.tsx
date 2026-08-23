"use client";

import { useMemo } from "react";
import { useRouter } from "next/navigation";
import { Layers } from "lucide-react";
import { motion } from "framer-motion";
import { useAuthStore } from "@/lib/auth-store";
import { useAllJobs } from "@/hooks/use-data";
import { StateDot } from "@/components/job-state-badge";
import { EmptyState, ErrorState, TableSkeleton } from "@/components/states";
import { Card } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Progress } from "@/components/ui/progress";
import { fmtRelative } from "@/lib/status";
import type { JobRow } from "@/hooks/use-data";

interface Batch {
  id: string;
  queue: string;
  jobs: JobRow[];
  created: string;
}

export default function BatchJobsPage() {
  const router = useRouter();
  const projectId = useAuthStore((s) => s.projectId);
  const { data, error, isLoading, mutate } = useAllJobs(projectId);

  const batches = useMemo<Batch[]>(() => {
    const map = new Map<string, Batch>();
    for (const j of data ?? []) {
      if (!j.batch_id) continue;
      const b = map.get(j.batch_id) ?? { id: j.batch_id, queue: j.queue_name, jobs: [], created: j.created_at };
      b.jobs.push(j);
      if (new Date(j.created_at) < new Date(b.created)) b.created = j.created_at;
      map.set(j.batch_id, b);
    }
    return [...map.values()].sort((a, b) => new Date(b.created).getTime() - new Date(a.created).getTime());
  }, [data]);

  if (error) return <ErrorState onRetry={() => mutate()} />;
  if (isLoading && !data) return <TableSkeleton rows={4} cols={4} />;
  if (!batches.length) {
    return (
      <EmptyState
        icon={Layers}
        title="No batch jobs"
        description="Jobs enqueued through the batch endpoint are grouped here by their batch ID."
      />
    );
  }

  return (
    <div className="grid gap-6 md:grid-cols-2 xl:grid-cols-3">
      {batches.map((b, i) => {
        const done = b.jobs.filter((j) => j.status === "completed").length;
        const failed = b.jobs.filter((j) => j.status === "failed" || j.status === "dead").length;
        const pct = b.jobs.length ? (done / b.jobs.length) * 100 : 0;

        return (
          <motion.div
            key={b.id}
            initial={{ opacity: 0, scale: 0.97 }}
            animate={{ opacity: 1, scale: 1 }}
            transition={{ duration: 0.2, delay: i * 0.05 }}
          >
            <Card className="rounded-xl p-5 transition-all hover:border-primary hover:shadow-md">
              <div className="flex items-start justify-between gap-2">
                <p className="min-w-0 truncate font-mono text-xs">{b.id.slice(0, 12)}</p>
                <Badge variant="outline" className="shrink-0 rounded-full text-[10px]">{b.queue}</Badge>
              </div>

              <p className="mt-2 text-2xl font-bold tabular-nums tracking-tight">{b.jobs.length}</p>
              <p className="text-[11px] text-muted-foreground">jobs · created {fmtRelative(b.created)}</p>

              <div className="mt-3">
                <div className="mb-1 flex justify-between text-[11px]">
                  <span className="text-muted-foreground">Completed</span>
                  <span className="font-mono tabular-nums">{done}/{b.jobs.length}</span>
                </div>
                <Progress value={pct} className="h-2" aria-label={`Batch ${pct.toFixed(0)} percent complete`} />
              </div>

              <div className="mt-3 flex flex-wrap gap-1.5">
                <Badge variant="outline" className="rounded-full text-[10px]">{done} done</Badge>
                <Badge variant="outline" className="rounded-full text-[10px]">{failed} failed</Badge>
              </div>

              <ul className="mt-3 space-y-1">
                {b.jobs.slice(0, 4).map((j) => (
                  <li key={j.id}>
                    <button
                      onClick={() => router.push(`/jobs/${j.id}`)}
                      className="flex w-full items-center gap-2 rounded-md px-1 py-0.5 text-left text-[11px] transition-colors hover:bg-accent focus-visible:ring-2 focus-visible:ring-primary"
                    >
                      <StateDot state={j.status} />
                      <span className="min-w-0 flex-1 truncate">{j.type}</span>
                    </button>
                  </li>
                ))}
                {b.jobs.length > 4 && (
                  <li className="px-1 text-[11px] text-muted-foreground">+{b.jobs.length - 4} more</li>
                )}
              </ul>
            </Card>
          </motion.div>
        );
      })}
    </div>
  );
}
