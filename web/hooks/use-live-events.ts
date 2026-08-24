"use client";

import { useEffect, useRef, useState } from "react";
import { mutate } from "swr";
import { useAuthStore } from "@/lib/auth-store";

const API_BASE = process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080";
const RECONNECT_BASE_MS = 1000;
const RECONNECT_MAX_MS = 15000;

interface LiveEvent {
  type: string;
  project_id: string;
  queue_id?: string;
  job_id?: string;
  worker_id?: string;
  job_type?: string;
  status?: string;
  shard: number;
  attempt?: number;
  error?: string;
  at: string;
}

export type LiveStatus = "connecting" | "live" | "reconnecting" | "offline";

function revalidatePrefix(prefix: string, match?: (key: unknown[]) => boolean) {
  mutate(
    (key) => Array.isArray(key) && key[0] === prefix && (!match || match(key)),
    undefined,
    { revalidate: true }
  );
}

function handleEvent(evt: LiveEvent) {
  switch (evt.type) {
    case "job.enqueued":
    case "job.started":
    case "job.completed":
    case "job.failed":
    case "job.dead_lettered":
    case "job.unblocked":
    case "job.cancelled":
      revalidatePrefix("all-jobs");
      revalidatePrefix("queue-stats", (k) => k[1] === evt.queue_id);
      revalidatePrefix("queue-metrics", (k) => k[1] === evt.queue_id);
      revalidatePrefix("project-metrics");
      if (evt.job_id) {
        mutate(["job", evt.job_id]);
        revalidatePrefix("job-execs", (k) => k[1] === evt.job_id);
        revalidatePrefix("job-logs", (k) => k[1] === evt.job_id);
        revalidatePrefix("job-deps", (k) => k[1] === evt.job_id);
      }
      if (evt.type === "job.dead_lettered") revalidatePrefix("all-dlq");
      return;
    case "queue.paused":
    case "queue.resumed":
      revalidatePrefix("queues");
      if (evt.queue_id) {
        mutate(["queue", evt.queue_id]);
        revalidatePrefix("queue-stats", (k) => k[1] === evt.queue_id);
      }
      return;
    case "worker.online":
    case "worker.offline":
    case "worker.heartbeat":
      revalidatePrefix("workers");
      if (evt.worker_id) mutate(["worker", evt.worker_id]);
      revalidatePrefix("project-metrics");
      return;
  }
}

export function useLiveEvents(projectId: string | null): LiveStatus {
  const accessToken = useAuthStore((s) => s.accessToken);
  const [status, setStatus] = useState<LiveStatus>("connecting");
  const attempt = useRef(0);

  useEffect(() => {
    if (!projectId || !accessToken) {
      setStatus("offline");
      return;
    }

    let socket: WebSocket | null = null;
    let stopped = false;
    let reconnectTimer: ReturnType<typeof setTimeout> | null = null;

    function connect() {
      setStatus(attempt.current === 0 ? "connecting" : "reconnecting");
      const wsBase = API_BASE.replace(/^http/, "ws");
      const url = `${wsBase}/api/v1/projects/${projectId}/events?token=${encodeURIComponent(accessToken!)}`;
      socket = new WebSocket(url);

      socket.onopen = () => {
        attempt.current = 0;
      };

      socket.onmessage = (ev) => {
        let payload: { type: string } & Partial<LiveEvent>;
        try {
          payload = JSON.parse(ev.data);
        } catch {
          return;
        }
        if (payload.type === "stream.ready") {
          setStatus("live");
          return;
        }
        handleEvent(payload as LiveEvent);
      };

      socket.onclose = () => {
        if (stopped) return;
        setStatus("reconnecting");
        const delay = Math.min(RECONNECT_MAX_MS, RECONNECT_BASE_MS * 2 ** attempt.current);
        attempt.current += 1;
        reconnectTimer = setTimeout(connect, delay);
      };

      socket.onerror = () => {
        socket?.close();
      };
    }

    connect();

    return () => {
      stopped = true;
      if (reconnectTimer) clearTimeout(reconnectTimer);
      socket?.close();
    };
  }, [projectId, accessToken]);

  return status;
}
