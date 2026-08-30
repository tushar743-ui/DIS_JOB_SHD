"use client";

import { useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { preload } from "swr";
import { auth, system } from "@/lib/api";
import { useAuthStore } from "@/lib/auth-store";

export function useDemoSession(destination = "/dashboard") {
  const router = useRouter();
  const setAuth = useAuthStore((s) => s.setAuth);
  const setProject = useAuthStore((s) => s.setProject);
  const [starting, setStarting] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    router.prefetch(destination);
    preload("features", () => system.features()).catch(() => {});
  }, [router, destination]);

  async function start() {
    setError("");
    setStarting(true);
    try {
      const res = await auth.demo();
      setAuth(res.access_token, res.refresh_token, {
        id: res.user_id,
        email: res.email,
        name: res.name,
      });
      if (res.project_id && res.org_id) setProject(res.project_id, res.org_id);
      router.push(destination);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Could not start the demo");
      setStarting(false);
    }
  }

  return { start, starting, error };
}
