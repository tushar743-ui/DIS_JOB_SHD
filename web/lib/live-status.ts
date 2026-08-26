"use client";

import { useSyncExternalStore } from "react";

export type LiveStatus = "connecting" | "live" | "reconnecting" | "offline";

let current: LiveStatus = "connecting";
const listeners = new Set<() => void>();

export function setLiveStatus(next: LiveStatus) {
  if (next === current) return;
  current = next;
  for (const l of listeners) l();
}

function subscribe(cb: () => void) {
  listeners.add(cb);
  return () => listeners.delete(cb);
}

export function useLiveStatus(): LiveStatus {
  return useSyncExternalStore(
    subscribe,
    () => current,
    () => "connecting" as LiveStatus
  );
}

export function useIsLive(): boolean {
  return useLiveStatus() === "live";
}
