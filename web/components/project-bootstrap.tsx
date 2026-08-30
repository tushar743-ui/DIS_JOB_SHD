"use client";

import { useEffect } from "react";
import { usePathname, useRouter } from "next/navigation";
import { orgs, projects } from "@/lib/api";
import { useAuthStore } from "@/lib/auth-store";

async function findWorkspace() {
  const orgList = await orgs.list();
  if (!orgList.length) return null;
  const lists = await Promise.all(orgList.map((org) => projects.list(org.id).catch(() => [])));
  for (let i = 0; i < orgList.length; i++) {
    if (lists[i].length) return { projectId: lists[i][0].id, orgId: orgList[i].id };
  }
  return null;
}

export function ProjectBootstrap() {
  const projectId = useAuthStore((s) => s.projectId);
  const accessToken = useAuthStore((s) => s.accessToken);
  const setProject = useAuthStore((s) => s.setProject);
  const router = useRouter();
  const pathname = usePathname();

  useEffect(() => {
    if (projectId || !accessToken) return;
    let cancelled = false;

    findWorkspace()
      .then((found) => {
        if (cancelled) return;
        if (found) setProject(found.projectId, found.orgId);
        else if (pathname !== "/settings") router.replace("/settings");
      })
      .catch(() => {});

    return () => {
      cancelled = true;
    };
  }, [projectId, accessToken, setProject, router, pathname]);

  return null;
}
