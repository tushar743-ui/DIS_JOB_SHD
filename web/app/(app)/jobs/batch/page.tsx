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
            <Card className="rounded-xl p-6 transition-colors hover:border-primary">
              <div className="flex items-start justify-between gap-3">
                <p className="t-data min-w-0 truncate font-normal text-muted-foreground">{b.id.slice(0, 12)}</p>
                <Badge variant="outline" className="t-label shrink-0 rounded-full px-2.5 py-1">{b.queue}</Badge>
              </div>

              <div className="mt-4 flex items-baseline gap-2.5">
                <span className="t-metric">{b.jobs.length}</span>
                <span className="t-meta text-muted-foreground">jobs</span>
              </div>
              <p className="t-meta mt-1.5 text-muted-foreground">created {fmtRelative(b.created)}</p>

              <div className="mt-5">
                <div className="mb-2 flex items-baseline justify-between">
                  <span className="t-label text-muted-foreground">Completed</span>
                  <span className="t-data text-[0.9375rem] font-semibold">
                    {done}<span className="text-muted-foreground">/{b.jobs.length}</span>
                  </span>
                </div>
                <Progress value={pct} className="h-2" aria-label={`Batch ${pct.toFixed(0)} percent complete`} />
              </div>

              <div className="mt-4 flex flex-wrap gap-2">
                <Badge variant="outline" className="t-chip rounded-full px-2.5 py-1">
                  <span className="font-mono font-semibold tabular-nums">{done}</span> done
                </Badge>
                <Badge variant="outline" className="t-chip rounded-full px-2.5 py-1">
                  <span className="font-mono font-semibold tabular-nums">{failed}</span> failed
                </Badge>
              </div>

              <ul className="mt-5 space-y-0.5">
                {b.jobs.slice(0, 4).map((j) => (
                  <li key={j.id}>
                    <button
                      onClick={() => router.push(`/jobs/${j.id}`)}
                      className="t-meta flex w-full items-center gap-2.5 rounded-md px-1.5 py-1.5 text-left transition-colors hover:bg-accent focus-visible:ring-2 focus-visible:ring-primary"
                    >
                      <StateDot state={j.status} />
                      <span className="min-w-0 flex-1 truncate">{j.type}</span>
                    </button>
                  </li>
                ))}
                {b.jobs.length > 4 && (
                  <li className="t-meta px-1.5 pt-1 text-muted-foreground">+{b.jobs.length - 4} more</li>
                )}
              </ul>
            </Card>
          </motion.div>
        );
      })}
    </div>
  );
}
