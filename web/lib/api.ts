const BASE = process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080";

let accessToken: string | null = null;
let refreshToken: string | null = null;

export function setAccessToken(t: string | null) {
  accessToken = t;
}

export function setRefreshToken(t: string | null) {
  refreshToken = t;
}

type TokenListener = (access: string, refresh: string) => void;
let onTokens: TokenListener | null = null;
let onAuthLost: (() => void) | null = null;

export function onTokensRefreshed(cb: TokenListener) {
  onTokens = cb;
}

export function onAuthFailure(cb: () => void) {
  onAuthLost = cb;
}

let refreshing: Promise<string | null> | null = null;

function refreshAccessToken(): Promise<string | null> {
  if (!refreshToken) return Promise.resolve(null);
  refreshing ??= (async () => {
    const presented = refreshToken;
    try {
      const res = await fetch(`${BASE}/api/v1/auth/refresh`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ refresh_token: presented }),
      });
      if (!res.ok) return null;
      const data = (await res.json()) as { access_token: string; refresh_token: string };
      accessToken = data.access_token;
      refreshToken = data.refresh_token;
      onTokens?.(data.access_token, data.refresh_token);
      return data.access_token;
    } catch {
      return null;
    } finally {
      refreshing = null;
    }
  })();
  return refreshing;
}

async function req<T>(path: string, init: RequestInit = {}, retry = true): Promise<T> {
  const send = () =>
    fetch(`${BASE}/api/v1${path}`, {
      ...init,
      headers: {
        "Content-Type": "application/json",
        ...(accessToken ? { Authorization: `Bearer ${accessToken}` } : {}),
        ...((init.headers as Record<string, string>) ?? {}),
      },
    });

  let res = await send();

  if (res.status === 401 && retry && refreshToken) {
    const fresh = await refreshAccessToken();
    if (!fresh) {
      onAuthLost?.();
      throw new Error("session expired");
    }
    res = await send();
  }

  if (!res.ok) {
    if (res.status === 401) onAuthLost?.();
    const body = await res.json().catch(() => ({ error: res.statusText }));
    throw new Error(body.error ?? res.statusText);
  }
  if (res.status === 204) return undefined as T;
  return res.json();
}

export const auth = {
  login: (email: string, password: string) =>
    req<{ access_token: string; refresh_token: string; user_id: string; name: string }>(
      "/auth/login", { method: "POST", body: JSON.stringify({ email, password }) }, false
    ),
  register: (email: string, password: string, name: string) =>
    req("/auth/register", { method: "POST", body: JSON.stringify({ email, password, name }) }, false),
  me: () => req<{ id: string; email: string; name: string }>("/auth/me"),
  logout: (refresh: string) =>
    req("/auth/logout", { method: "POST", body: JSON.stringify({ refresh_token: refresh }) }, false),
};

export const orgs = {
  list: () => req<Org[]>("/orgs"),
  create: (name: string) => req<{ id: string }>("/orgs", { method: "POST", body: JSON.stringify({ name }) }),
  get: (id: string) => req<Org>(`/orgs/${id}`),
  members: (id: string) => req<OrgMember[]>(`/orgs/${id}/members`),
};

export const projects = {
  list: (orgId: string) => req<Project[]>(`/orgs/${orgId}/projects`),
  create: (orgId: string, name: string) =>
    req<{ id: string; api_key: string }>(`/orgs/${orgId}/projects`, {
      method: "POST", body: JSON.stringify({ name }),
    }),
  get: (id: string) => req<Project>(`/projects/${id}`),
};

export const queues = {
  list: (projectId: string) => req<Queue[]>(`/projects/${projectId}/queues`),
  create: (projectId: string, data: CreateQueueInput) =>
    req<{ id: string }>(`/projects/${projectId}/queues`, { method: "POST", body: JSON.stringify(data) }),
  get: (id: string) => req<Queue>(`/queues/${id}`),
  update: (id: string, data: Partial<Queue>) =>
    req(`/queues/${id}`, { method: "PUT", body: JSON.stringify(data) }),
  stats: (id: string) => req<QueueStats>(`/queues/${id}/stats`),
  pause: (id: string) => req(`/queues/${id}/pause`, { method: "POST" }),
  resume: (id: string) => req(`/queues/${id}/resume`, { method: "POST" }),
  delete: (id: string) => req(`/queues/${id}`, { method: "DELETE" }),
};

export const jobs = {
  list: (queueId: string, params?: { limit?: number; offset?: number; status?: string }) => {
    const qs = new URLSearchParams();
    if (params?.limit) qs.set("limit", String(params.limit));
    if (params?.offset) qs.set("offset", String(params.offset));
    if (params?.status) qs.set("status", params.status);
    return req<Paginated<Job>>(`/queues/${queueId}/jobs?${qs}`);
  },
  create: (queueId: string, data: CreateJobInput) =>
    req<{ id: string; status: string }>(`/queues/${queueId}/jobs`, {
      method: "POST", body: JSON.stringify(data),
    }),
  get: (id: string) => req<Job>(`/jobs/${id}`),
  cancel: (id: string) => req(`/jobs/${id}`, { method: "DELETE" }),
  remove: (id: string) => req(`/jobs/${id}/purge`, { method: "DELETE" }),
  retry: (id: string) => req(`/jobs/${id}/retry`, { method: "POST" }),
  logs: (id: string, params?: { limit?: number; offset?: number }) => {
    const qs = new URLSearchParams();
    if (params?.limit) qs.set("limit", String(params.limit));
    if (params?.offset) qs.set("offset", String(params.offset));
    return req<JobLog[]>(`/jobs/${id}/logs?${qs}`);
  },
  executions: (id: string) => req<JobExecution[]>(`/jobs/${id}/executions`),
};

export const workers = {
  list: (projectId: string, status?: string) => {
    const qs = status ? `?status=${status}` : "";
    return req<Worker[]>(`/projects/${projectId}/workers${qs}`);
  },
  get: (id: string) => req<{ worker: Worker; heartbeats: Heartbeat[] }>(`/workers/${id}`),
};

export const dlq = {
  list: (queueId: string, params?: { limit?: number; offset?: number }) => {
    const qs = new URLSearchParams();
    if (params?.limit) qs.set("limit", String(params.limit));
    if (params?.offset) qs.set("offset", String(params.offset));
    return req<Paginated<DLQEntry>>(`/queues/${queueId}/dlq?${qs}`);
  },
  retry: (id: string) => req(`/dlq/${id}/retry`, { method: "POST" }),
  retryAll: (projectId: string) =>
    req<{ requeued: number; skipped_unhandled: number }>(
      `/projects/${projectId}/dlq/retry-all`,
      { method: "POST" }
    ),
  discardUnhandled: (projectId: string) =>
    req<{ discarded: number }>(`/projects/${projectId}/dlq/unhandled`, { method: "DELETE" }),
  discard: (id: string) => req(`/dlq/${id}`, { method: "DELETE" }),
};

export const metrics = {
  project: (projectId: string, hours = 24) =>
    req<ProjectMetrics>(`/projects/${projectId}/metrics?hours=${hours}`),
  queue: (queueId: string, hours = 24) =>
    req<QueueMetrics>(`/queues/${queueId}/metrics?hours=${hours}`),
};

export interface Org {
  id: string; name: string; slug: string; role?: string; created_at: string;
}
export interface OrgMember {
  id: string; email: string; name: string; role: string; joined_at: string;
}
export interface Project {
  id: string; org_id: string; name: string; slug: string; created_at: string;
}
export type JobStatus =
  | "queued" | "scheduled" | "claimed" | "running"
  | "completed" | "failed" | "cancelled" | "dead";

export interface Queue {
  id: string; project_id: string; name: string; description?: string;
  priority: number; concurrency_limit: number; paused: boolean;
  retry_policy_id?: string; created_at: string; updated_at: string;
}
export interface QueueStats {
  queue_id: string;
  by_status: Partial<Record<JobStatus, number>>;
  total: number;
}
export interface CreateQueueInput {
  name: string; description?: string; priority?: number;
  concurrency_limit?: number; retry_policy_id?: string;
}
export interface Job {
  id: string; queue_id: string; type: string; payload: unknown;
  status: JobStatus; priority: number; max_attempts: number;
  attempt_count: number; scheduled_at?: string; run_at: string;
  timeout_secs: number; cron_expression?: string; batch_id?: string;
  idempotency_key?: string; tags: string[]; last_error?: string;
  created_at: string; updated_at: string; completed_at?: string;
}
export interface CreateJobInput {
  type: string; payload?: unknown; priority?: number;
  max_attempts?: number; scheduled_at?: string; timeout_secs?: number;
  cron_expression?: string; idempotency_key?: string; tags?: string[];
}
export interface JobLog {
  id: number; level: string; message: string;
  metadata: unknown; logged_at: string;
}
export interface JobExecution {
  id: string; worker_id?: string; attempt_number: number; status: string;
  started_at: string; completed_at?: string; duration_ms?: number; error_message?: string;
}
export interface Worker {
  id: string; project_id: string; hostname: string; pid: number;
  version?: string; status: string; concurrency: number;
  registered_at: string; last_heartbeat_at: string;
}
export interface Heartbeat {
  at: string; jobs_running: number; jobs_completed: number;
}
export interface DLQEntry {
  id: string; job_id: string; queue_id: string; final_error?: string;
  attempts: number; moved_at: string; resolved_at?: string;
}
export interface Paginated<T> {
  data: T[]; total: number; limit: number; offset: number;
}
export interface ProjectMetrics {
  queues: { queue_id: string; queue_name: string; by_status: Partial<Record<JobStatus, number>> }[];
  active_workers: number;
  completed_24h: number;
  throughput_24h: { hour: string; completed: number; failed: number }[];
  range_hours: number;
  bucket_seconds: number;
  avg_duration_ms?: number;
  generated_at: string;
}
export interface QueueMetrics {
  queue_id: string;
  by_status: Partial<Record<JobStatus, number>>;
  throughput_24h: { hour: string; completed: number; failed: number }[];
  range_hours: number;
  bucket_seconds: number;
  avg_duration_ms?: number;
  generated_at: string;
}
