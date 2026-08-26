"use client";

import useSWR from "swr";
import {
  API_BASE,
  queues, jobs, workers, dlq, metrics, system, failureSummary,
  type Queue, type Job, type Worker, type DLQEntry,
  type ProjectMetrics, type QueueMetrics, type JobLog, type JobExecution,
  type QueueStats, type JobDependencies, type Features, type FailureSummary,
} from "@/lib/api";
import { useIsLive } from "@/lib/live-status";

const POLL_LIVE = 5000;
const POLL_SLOW = 15000;
const POLL_STATIC = 300000;
const PUSH_SAFETY_NET = 45000;

function useRefresh(base: number) {
  const live = useIsLive();
  return { refreshInterval: live ? Math.max(base, PUSH_SAFETY_NET) : base };
}

export function useQueues(projectId: string | null) {
  return useSWR<Queue[]>(projectId ? ["queues", projectId] : null, () => queues.list(projectId!), useRefresh(POLL_SLOW));
}

export function useQueue(queueId: string | null) {
  return useSWR<Queue>(queueId ? ["queue", queueId] : null, () => queues.get(queueId!), useRefresh(POLL_SLOW));
}

export function useQueueStats(queueId: string | null) {
  return useSWR<QueueStats>(queueId ? ["queue-stats", queueId] : null, () => queues.stats(queueId!), useRefresh(POLL_LIVE));
}

export interface JobRow extends Job {
  queue_name: string;
}

async function fetchAllJobs(projectId: string, status: string | undefined, limit: number): Promise<JobRow[]> {
  const res = await jobs.listByProject(projectId, { limit, status });
  return res.data;
}

export function useAllJobs(projectId: string | null, status?: string, live = false, limit = 100) {
  return useSWR<JobRow[]>(
    projectId ? ["all-jobs", projectId, status, limit] : null,
    () => fetchAllJobs(projectId!, status, limit),
    useRefresh(live ? POLL_LIVE : POLL_SLOW)
  );
}

export function useJob(jobId: string | null) {
  return useSWR<Job>(jobId ? ["job", jobId] : null, () => jobs.get(jobId!), useRefresh(POLL_LIVE));
}

export function useJobLogs(jobId: string | null) {
  return useSWR<JobLog[]>(jobId ? ["job-logs", jobId] : null, () => jobs.logs(jobId!, { limit: 500 }), useRefresh(POLL_LIVE));
}

export function useJobExecutions(jobId: string | null) {
  return useSWR<JobExecution[]>(jobId ? ["job-execs", jobId] : null, () => jobs.executions(jobId!), useRefresh(POLL_LIVE));
}

export function useJobDependencies(jobId: string | null) {
  return useSWR<JobDependencies>(jobId ? ["job-deps", jobId] : null, () => jobs.dependencies(jobId!), useRefresh(POLL_LIVE));
}

export function useFeatures() {
  return useSWR<Features>("features", () => system.features(), { refreshInterval: POLL_STATIC });
}

export function useFailureSummary(jobId: string | null) {
  return useSWR<FailureSummary | null>(
    jobId ? ["failure-summary", jobId] : null,
    () => failureSummary.get(jobId!).catch(() => null),
    { refreshInterval: 0 }
  );
}

export function useHandledJobTypes(projectId: string | null) {
  return useSWR<string[]>(
    projectId ? ["job-types", projectId] : null,
    () => jobs.handledTypes(projectId!),
    { refreshInterval: POLL_STATIC }
  );
}

export function useWorkers(projectId: string | null) {
  return useSWR<Worker[]>(projectId ? ["workers", projectId] : null, () => workers.list(projectId!), useRefresh(POLL_SLOW));
}

export function useWorker(workerId: string | null) {
  return useSWR(workerId ? ["worker", workerId] : null, () => workers.get(workerId!), useRefresh(POLL_SLOW));
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
    useRefresh(POLL_SLOW)
  );
}

export function useProjectMetrics(projectId: string | null, hours = 24) {
  return useSWR<ProjectMetrics>(
    projectId ? ["project-metrics", projectId, hours] : null,
    () => metrics.project(projectId!, hours),
    useRefresh(POLL_LIVE)
  );
}

export function useQueueMetrics(queueId: string | null, hours = 24) {
  return useSWR<QueueMetrics>(
    queueId ? ["queue-metrics", queueId, hours] : null,
    () => metrics.queue(queueId!, hours),
    useRefresh(POLL_SLOW)
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
    { refreshInterval: 30000, shouldRetryOnError: true, dedupingInterval: 20000 }
  );
  return { online: Boolean(data) && !error };
}
