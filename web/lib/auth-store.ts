"use client";

import { create } from "zustand";
import { persist } from "zustand/middleware";
import { setAccessToken } from "./api";

interface AuthState {
  accessToken: string | null;
  refreshToken: string | null;
  user: { id: string; email: string; name: string } | null;
  projectId: string | null;
  orgId: string | null;
  setAuth: (access: string, refresh: string, user: AuthState["user"]) => void;
  setProject: (projectId: string, orgId: string) => void;
  clear: () => void;
}

export const useAuthStore = create<AuthState>()(
  persist(
    (set) => ({
      accessToken: null,
      refreshToken: null,
      user: null,
      projectId: null,
      orgId: null,
      setAuth: (access, refresh, user) => {
        setAccessToken(access);
        set({ accessToken: access, refreshToken: refresh, user });
      },
      setProject: (projectId, orgId) => set({ projectId, orgId }),
      clear: () => {
        setAccessToken(null);
        set({ accessToken: null, refreshToken: null, user: null, projectId: null, orgId: null });
      },
    }),
    {
      name: "djq-auth",
      onRehydrateStorage: () => (state) => {
        if (state?.accessToken) setAccessToken(state.accessToken);
      },
    }
  )
);
