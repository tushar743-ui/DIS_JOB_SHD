"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";
import { auth } from "@/lib/api";
import { useAuthStore } from "@/lib/auth-store";

export function useDemoSession(destination = "/dashboard") {
  const router = useRouter();
  const setAuth = useAuthStore((s) => s.setAuth);
  const [starting, setStarting] = useState(false);
  const [error, setError] = useState("");

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
      router.push(destination);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Could not start the demo");
      setStarting(false);
    }
  }

  return { start, starting, error };
}
