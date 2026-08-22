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
        set({ accessToken: access, refreshToken: refresh, user });
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
        // Both halves must be restored: without the refresh token the client
        // cannot renew a session that outlived the 15m access token.
        if (state?.accessToken) setAccessToken(state.accessToken);
        if (state?.refreshToken) setRefreshToken(state.refreshToken);
      },
    }
  )
);

// Persist rotated tokens so a reload picks up the newest pair rather than the
// one that was already revoked server-side.
onTokensRefreshed((access, refresh) => {
  useAuthStore.setState({ accessToken: access, refreshToken: refresh });
});

// Clearing the token is enough to move the user out: AuthGuard watches it and
// routes to /login once the store no longer holds a session.
onAuthFailure(() => {
  if (!useAuthStore.getState().accessToken) return;
  useAuthStore.getState().clear();
});
