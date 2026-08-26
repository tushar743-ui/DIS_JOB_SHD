"use client";

import { create } from "zustand";
import { persist } from "zustand/middleware";
import { setAccessToken, setRefreshToken, onTokensRefreshed, onAuthFailure } from "./api";

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
        setRefreshToken(refresh);
        set((s) => {
          const sameUser = Boolean(s.user?.id) && s.user?.id === user?.id;
          return sameUser
            ? { accessToken: access, refreshToken: refresh, user }
            : { accessToken: access, refreshToken: refresh, user, projectId: null, orgId: null };
        });
      },
      setProject: (projectId, orgId) => set({ projectId, orgId }),
      clear: () => {
        setAccessToken(null);
        setRefreshToken(null);
        set({ accessToken: null, refreshToken: null, user: null, projectId: null, orgId: null });
      },
    }),
    {
      name: "djq-auth",
      onRehydrateStorage: () => (state) => {
        if (state?.accessToken) setAccessToken(state.accessToken);
        if (state?.refreshToken) setRefreshToken(state.refreshToken);
      },
    }
  )
);

onTokensRefreshed((access, refresh) => {
  useAuthStore.setState({ accessToken: access, refreshToken: refresh });
});

onAuthFailure(() => {
  if (!useAuthStore.getState().accessToken) return;
  useAuthStore.getState().clear();
});
