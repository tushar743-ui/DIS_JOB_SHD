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
    orgs.list()
      .then((orgList) => {
        if (!orgList.length) return;
        return projects.list(orgList[0].id).then((projectList) => {
          if (projectList.length > 0) setProject(projectList[0].id, orgList[0].id);
        });
      })
      .catch(() => {});
  }, [projectId, accessToken, setProject]);

  return null;
}
