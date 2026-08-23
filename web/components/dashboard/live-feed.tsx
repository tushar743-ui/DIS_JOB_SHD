"use client";

import { useRouter } from "next/navigation";
import { AnimatePresence, motion } from "framer-motion";
import { Activity } from "lucide-react";
import { StateDot } from "@/components/job-state-badge";
import { useElapsedTime } from "@/hooks/use-elapsed-time";
import { EmptyState } from "@/components/states";
import { Badge } from "@/components/ui/badge";
import type { JobRow } from "@/hooks/use-data";

function argsPreview(payload: unknown): string {
  if (payload == null) return "-";
  try {
    const s = typeof payload === "string" ? payload : JSON.stringify(payload);
    return s.length > 48 ? s.slice(0, 48) + "…" : s;
  } catch {
    return "-";
  }
}

function FeedRow({ job }: { job: JobRow }) {
  const router = useRouter();
  const started = job.status === "running" || job.status === "claimed";
  const elapsed = useElapsedTime(started ? job.updated_at : job.created_at, started ? null : job.completed_at);

  return (
    <motion.li
      layout
      initial={{ opacity: 0, x: -4 }}
      animate={{ opacity: 1, x: 0 }}
      exit={{ opacity: 0, x: 8 }}
      transition={{ duration: 0.15 }}
      onClick={() => router.push(`/jobs/${job.id}`)}
      className="flex h-12 cursor-pointer items-center gap-3 border-b border-border px-4 transition-colors last:border-0 hover:bg-accent/40"
    >
      <StateDot state={job.status} />
      <span className="w-48 shrink-0 truncate text-sm font-medium">{job.type}</span>
      <span className="shrink-0 font-mono text-[11px] text-muted-foreground">
        {job.attempt_count}/{job.max_attempts}
      </span>
      <span className="min-w-0 flex-1 truncate font-mono text-[11px] text-muted-foreground">
        {argsPreview(job.payload)}
      </span>
      <span className="w-16 shrink-0 text-right font-mono text-[11px] tabular-nums text-muted-foreground">
        {elapsed}
      </span>
      <Badge variant="outline" className="shrink-0 rounded-full text-[10px]">{job.queue_name}</Badge>
    </motion.li>
  );
}

export function LiveFeed({ jobs }: { jobs: JobRow[] }) {
  const rows = jobs.slice(0, 20);

  if (!rows.length) {
    return (
      <EmptyState
        icon={Activity}
        title="No active jobs"
        description="Running jobs will appear here the moment a worker claims one."
      />
    );
  }

  return (
    <ul className="overflow-hidden rounded-xl border border-border">
      <AnimatePresence initial={false}>
        {rows.map((job) => <FeedRow key={job.id} job={job} />)}
      </AnimatePresence>
    </ul>
  );
}
