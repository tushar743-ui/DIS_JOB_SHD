"use client";

import { create } from "zustand";
import { persist } from "zustand/middleware";

interface UIState {
  sidebarCollapsed: boolean;
  toggleSidebar: () => void;
  setSidebarCollapsed: (v: boolean) => void;
  activeQueueId: string | null;
  setActiveQueue: (id: string | null) => void;
  commandOpen: boolean;
  setCommandOpen: (v: boolean) => void;
}

export const useUIStore = create<UIState>()(
  persist(
    (set) => ({
      sidebarCollapsed: false,
      toggleSidebar: () => set((s) => ({ sidebarCollapsed: !s.sidebarCollapsed })),
      setSidebarCollapsed: (v) => set({ sidebarCollapsed: v }),
      activeQueueId: null,
      setActiveQueue: (id) => set({ activeQueueId: id }),
      commandOpen: false,
      setCommandOpen: (v) => set({ commandOpen: v }),
    }),
    {
      name: "djq-ui",
      partialize: (s) => ({
        sidebarCollapsed: s.sidebarCollapsed,
        activeQueueId: s.activeQueueId,
      }),
    }
  )
);
