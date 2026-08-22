"use client";

import { useEffect, useState } from "react";
import { orgs, projects, type Org, type Project } from "@/lib/api";
import { useAuthStore } from "@/lib/auth-store";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import { Eye, EyeSlash, Copy, Check } from "@phosphor-icons/react";

export default function SettingsPage() {
  const { user, projectId, orgId, setProject } = useAuthStore();
  const accessToken = useAuthStore((s) => s.accessToken);
  const [orgList, setOrgList] = useState<Org[]>([]);
  const [projectList, setProjectList] = useState<Project[]>([]);
  const [selectedOrg, setSelectedOrg] = useState(orgId ?? "");
  const [selectedProject, setSelectedProject] = useState(projectId ?? "");
  const [loading, setLoading] = useState(true);
  const [copied, setCopied] = useState(false);

  // Sync dropdowns after Zustand hydrates from localStorage
  useEffect(() => {
    if (orgId && !selectedOrg) setSelectedOrg(orgId);
  }, [orgId]); // eslint-disable-line react-hooks/exhaustive-deps
  useEffect(() => {
    if (projectId && !selectedProject) setSelectedProject(projectId);
  }, [projectId]); // eslint-disable-line react-hooks/exhaustive-deps

  // Wait for auth token before hitting the API (Zustand hydrates async)
  useEffect(() => {
    if (!accessToken) { setLoading(false); return; }
    orgs.list().then(setOrgList).catch(() => {}).finally(() => setLoading(false));
  }, [accessToken]);

  useEffect(() => {
    if (!selectedOrg || !accessToken) return;
    projects.list(selectedOrg).then(setProjectList).catch(() => {});
  }, [selectedOrg, accessToken]);

  function save() {
    if (selectedOrg && selectedProject) {
      setProject(selectedProject, selectedOrg);
    }
  }

  async function copyId() {
    if (!selectedProject) return;
    await navigator.clipboard.writeText(selectedProject);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  }

  return (
    <div className="p-6 max-w-xl mx-auto space-y-8">
      <h1 className="text-lg font-semibold">Settings</h1>

      <section>
        <h2 className="text-xs uppercase tracking-wide text-muted-foreground mb-3">Account</h2>
        <div className="border border-border rounded-lg p-4 space-y-3 bg-card">
          <div>
            <Label className="text-xs">Name</Label>
            <p className="text-sm mt-0.5">{user?.name ?? "–"}</p>
          </div>
          <div>
            <Label className="text-xs">Email</Label>
            <p className="text-sm mt-0.5">{user?.email ?? "–"}</p>
          </div>
        </div>
      </section>

      <section>
        <h2 className="text-xs uppercase tracking-wide text-muted-foreground mb-3">Active project</h2>
        <div className="border border-border rounded-lg p-4 space-y-4 bg-card">
          {loading ? (
            <Skeleton className="h-9 w-full" />
          ) : (
            <>
              <div className="space-y-1.5">
                <Label htmlFor="org">Organization</Label>
                <select
                  id="org"
                  value={selectedOrg}
                  onChange={(e) => { setSelectedOrg(e.target.value); setSelectedProject(""); }}
                  className="w-full h-9 px-3 rounded-md border border-input bg-background text-sm"
                >
                  <option value="">Select organization</option>
                  {orgList.map((o) => (
                    <option key={o.id} value={o.id}>{o.name}</option>
                  ))}
                </select>
              </div>

              {selectedOrg && (
                <div className="space-y-1.5">
                  <Label htmlFor="project">Project</Label>
                  <select
                    id="project"
                    value={selectedProject}
                    onChange={(e) => setSelectedProject(e.target.value)}
                    className="w-full h-9 px-3 rounded-md border border-input bg-background text-sm"
                  >
                    <option value="">Select project</option>
                    {projectList.map((p) => (
                      <option key={p.id} value={p.id}>{p.name}</option>
                    ))}
                  </select>
                </div>
              )}

              {selectedProject && (
                <div className="space-y-1.5">
                  <Label className="text-xs">Project ID</Label>
                  <div className="flex items-center gap-2">
                    <Input value={selectedProject} readOnly className="font-mono text-xs h-8" />
                    <button onClick={copyId} className="text-muted-foreground hover:text-foreground transition-colors p-1">
                      {copied ? <Check size={15} className="text-emerald-400" /> : <Copy size={15} />}
                    </button>
                  </div>
                </div>
              )}

              <Button
                onClick={save}
                disabled={!selectedOrg || !selectedProject}
                className="w-full bg-amber-500 hover:bg-amber-400 text-black"
              >
                Save project
              </Button>
            </>
          )}
        </div>
      </section>
    </div>
  );
}
