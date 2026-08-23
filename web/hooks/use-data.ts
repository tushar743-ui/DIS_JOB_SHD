"use client";

import useSWR from "swr";
import {
  queues, jobs, workers, dlq, metrics,
  type Queue, type Job, type Worker, type DLQEntry,
  type ProjectMetrics, type QueueMetrics, type JobLog, type JobExecution,
  type QueueStats,
} from "@/lib/api";

const LIVE = { refreshInterval: 2000 };
const SLOW = { refreshInterval: 5000 };

export function useQueues(projectId: string | null) {
  return useSWR<Queue[]>(projectId ? ["queues", projectId] : null, () => queues.list(projectId!), SLOW);
}

export function useQueue(queueId: string | null) {
  return useSWR<Queue>(queueId ? ["queue", queueId] : null, () => queues.get(queueId!), SLOW);
}

export function useQueueStats(queueId: string | null) {
  return useSWR<QueueStats>(queueId ? ["queue-stats", queueId] : null, () => queues.stats(queueId!), LIVE);
}

export interface JobRow extends Job {
  queue_name: string;
}

async function fetchAllJobs(list: Queue[], status?: string, perQueue = 100): Promise<JobRow[]> {
  const pages = await Promise.all(
    list.map(async (q) => {
      const res = await jobs.list(q.id, { limit: perQueue, status }).catch(() => null);
      return (res?.data ?? []).map((j) => ({ ...j, queue_name: q.name }));
    })
  );
  return pages.flat().sort(
    (a, b) => new Date(b.created_at).getTime() - new Date(a.created_at).getTime()
  );
}

export function useAllJobs(projectId: string | null, status?: string, live = false) {
  const { data: queueList } = useQueues(projectId);
  return useSWR<JobRow[]>(
    queueList && queueList.length ? ["all-jobs", projectId, status, queueList.length] : null,
    () => fetchAllJobs(queueList!, status),
    live ? LIVE : SLOW
  );
}

export function useJob(jobId: string | null) {
  return useSWR<Job>(jobId ? ["job", jobId] : null, () => jobs.get(jobId!), LIVE);
}

export function useJobLogs(jobId: string | null) {
  return useSWR<JobLog[]>(jobId ? ["job-logs", jobId] : null, () => jobs.logs(jobId!, { limit: 500 }), LIVE);
}

export function useJobExecutions(jobId: string | null) {
  return useSWR<JobExecution[]>(jobId ? ["job-execs", jobId] : null, () => jobs.executions(jobId!), LIVE);
}

export function useWorkers(projectId: string | null) {
  return useSWR<Worker[]>(projectId ? ["workers", projectId] : null, () => workers.list(projectId!), SLOW);
}

export function useWorker(workerId: string | null) {
  return useSWR(workerId ? ["worker", workerId] : null, () => workers.get(workerId!), SLOW);
}

export interface DLQRow extends DLQEntry {
  queue_name: string;
}

export function useAllDLQ(projectId: string | null) {
  const { data: queueList } = useQueues(projectId);
  return useSWR<DLQRow[]>(
    queueList && queueList.length ? ["all-dlq", projectId, queueList.length] : null,
    async () => {
      const pages = await Promise.all(
        queueList!.map(async (q) => {
          const res = await dlq.list(q.id, { limit: 100 }).catch(() => null);
          return (res?.data ?? []).map((e) => ({ ...e, queue_name: q.name }));
        })
      );
      return pages.flat().sort((a, b) => new Date(b.moved_at).getTime() - new Date(a.moved_at).getTime());
    },
    SLOW
  );
}

export function useProjectMetrics(projectId: string | null) {
  return useSWR<ProjectMetrics>(
    projectId ? ["project-metrics", projectId] : null,
    () => metrics.project(projectId!),
    LIVE
  );
}

export function useQueueMetrics(queueId: string | null) {
  return useSWR<QueueMetrics>(
    queueId ? ["queue-metrics", queueId] : null,
    () => metrics.queue(queueId!),
    SLOW
  );
}

const API_BASE = process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080";

export function useBackendHealth() {
  const { data, error } = useSWR(
    "backend-health",
    async () => {
      const res = await fetch(`${API_BASE}/health`);
      if (!res.ok) throw new Error("unhealthy");
      return true;
    },
    { refreshInterval: 10000, shouldRetryOnError: true }
  );
  return { online: Boolean(data) && !error };
}
