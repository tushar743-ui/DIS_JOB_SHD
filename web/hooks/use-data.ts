"use client";

import useSWR from "swr";
import {
  API_BASE,
  queues, jobs, workers, dlq, metrics, system, failureSummary,
  type Queue, type Job, type Worker, type DLQEntry,
  type ProjectMetrics, type QueueMetrics, type JobLog, type JobExecution,
  type QueueStats, type JobDependencies, type Features, type FailureSummary,
} from "@/lib/api";

const LIVE = { refreshInterval: 5000 };
const SLOW = { refreshInterval: 15000 };

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

async function fetchAllJobs(projectId: string, status?: string, limit = 200): Promise<JobRow[]> {
  const res = await jobs.listByProject(projectId, { limit, status });
  return res.data;
}

export function useAllJobs(projectId: string | null, status?: string, live = false) {
  return useSWR<JobRow[]>(
    projectId ? ["all-jobs", projectId, status] : null,
    () => fetchAllJobs(projectId!, status),
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

export function useJobDependencies(jobId: string | null) {
  return useSWR<JobDependencies>(jobId ? ["job-deps", jobId] : null, () => jobs.dependencies(jobId!), LIVE);
}

export function useFeatures() {
  return useSWR<Features>("features", () => system.features(), SLOW);
}

export function useFailureSummary(jobId: string | null) {
  return useSWR<FailureSummary | null>(
    jobId ? ["failure-summary", jobId] : null,
    () => failureSummary.get(jobId!).catch(() => null),
    SLOW
  );
}

export function useHandledJobTypes(projectId: string | null) {
  return useSWR<string[]>(
    projectId ? ["job-types", projectId] : null,
    () => jobs.handledTypes(projectId!),
    SLOW
  );
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

export function useProjectMetrics(projectId: string | null, hours = 24) {
  return useSWR<ProjectMetrics>(
    projectId ? ["project-metrics", projectId, hours] : null,
    () => metrics.project(projectId!, hours),
    LIVE
  );
}

export function useQueueMetrics(queueId: string | null, hours = 24) {
  return useSWR<QueueMetrics>(
    queueId ? ["queue-metrics", queueId, hours] : null,
    () => metrics.queue(queueId!, hours),
    SLOW
  );
}

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
