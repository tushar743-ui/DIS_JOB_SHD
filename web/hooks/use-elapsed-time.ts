"use client";

import { useSyncExternalStore } from "react";

export function formatElapsed(seconds: number): string {
  if (seconds < 0) return "0s";
  if (seconds < 60) return `${seconds}s`;
  const m = Math.floor(seconds / 60);
  if (m < 60) {
    const s = seconds % 60;
    return s ? `${m}m ${s}s` : `${m}m`;
  }
  const h = Math.floor(m / 60);
  const rm = m % 60;
  if (h < 24) return rm ? `${h}h ${rm}m` : `${h}h`;
  const d = Math.floor(h / 24);
  return `${d}d ${h % 24}h`;
}

const listeners = new Set<() => void>();
let tickHandle: ReturnType<typeof setInterval> | null = null;
let current = Date.now();

function subscribe(cb: () => void) {
  listeners.add(cb);
  if (!tickHandle) {
    current = Date.now();
    tickHandle = setInterval(() => {
      current = Date.now();
      for (const l of listeners) l();
    }, 1000);
  }
  return () => {
    listeners.delete(cb);
    if (listeners.size === 0 && tickHandle) {
      clearInterval(tickHandle);
      tickHandle = null;
    }
  };
}

function getSnapshot() {
  return current;
}

function getServerSnapshot() {
  return 0;
}

export function useNow(): number {
  return useSyncExternalStore(subscribe, getSnapshot, getServerSnapshot);
}

export function useElapsedTime(startedAt?: string | null, stoppedAt?: string | null): string {
  const now = useNow();
  if (!startedAt) return "-";
  const start = new Date(startedAt).getTime();
  if (Number.isNaN(start)) return "-";
  if (!now) return "-";
  const end = stoppedAt ? new Date(stoppedAt).getTime() : now;
  return formatElapsed(Math.floor((end - start) / 1000));
}

export function useCountdown(target?: string | null): string {
  const now = useNow();
  if (!target) return "-";
  const t = new Date(target).getTime();
  if (Number.isNaN(t)) return "-";
  if (!now) return "-";
  const diff = Math.floor((t - now) / 1000);
  return diff <= 0 ? "due now" : `in ${formatElapsed(diff)}`;
}
