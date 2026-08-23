"use client";

import { useEffect } from "react";
import { orgs, projects } from "@/lib/api";
import { useAuthStore } from "@/lib/auth-store";

export function ProjectBootstrap() {
  const projectId = useAuthStore((s) => s.projectId);
  const accessToken = useAuthStore((s) => s.accessToken);
  const setProject = useAuthStore((s) => s.setProject);

  useEffect(() => {
    if (projectId || !accessToken) return;
    const bootstrap = async () => {
      const orgList = await orgs.list();
      const orgId = orgList[0]?.id ?? (await orgs.create("Personal")).id;
      const projectList = await projects.list(orgId);
      const id = projectList[0]?.id ?? (await projects.create(orgId, "Default")).id;
      setProject(id, orgId);
    };
    bootstrap().catch(() => {});
  }, [projectId, accessToken, setProject]);

  return null;
}
