"use client";

import { useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { orgs, projects, type Org, type Project } from "@/lib/api";
import { useAuthStore } from "@/lib/auth-store";
import { errMessage } from "@/lib/errors";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import { AlertCircle } from "lucide-react";
import { CopyIcon } from "@phosphor-icons/react/dist/csr/Copy";
import { CheckIcon } from "@phosphor-icons/react/dist/csr/Check";

export default function SettingsPage() {
  const router = useRouter();
  const { user, projectId, orgId, setProject } = useAuthStore();
  const accessToken = useAuthStore((s) => s.accessToken);
  const [orgList, setOrgList] = useState<Org[]>([]);
  const [projectList, setProjectList] = useState<Project[]>([]);
  const [copied, setCopied] = useState(false);
  const [orgsLoaded, setOrgsLoaded] = useState(false);
  const [projectsFor, setProjectsFor] = useState<string | null>(null);

  const [orgChoice, setOrgChoice] = useState<string | null>(null);
  const [projectChoice, setProjectChoice] = useState<string | null>(null);
  const selectedOrg = orgChoice ?? orgId ?? orgList[0]?.id ?? "";
  const selectedProject = projectChoice ?? projectId ?? "";

  const [orgNameInput, setOrgNameInput] = useState<string | null>(null);
  const orgName = orgNameInput ?? (user?.name ? `${user.name}'s Workspace` : "");
  const [projectName, setProjectName] = useState("");
  const [creating, setCreating] = useState(false);
  const [createError, setCreateError] = useState("");

  const loading = Boolean(accessToken) && !orgsLoaded;
  const projectsLoaded = projectsFor === selectedOrg;
  const needsOrg = orgsLoaded && orgList.length === 0;
  const needsProject = !needsOrg && Boolean(selectedOrg) && projectsLoaded && projectList.length === 0;
  const onboarding = needsOrg || needsProject;

  useEffect(() => {
    if (!accessToken) return;
    orgs.list().then(setOrgList).catch(() => {}).finally(() => setOrgsLoaded(true));
  }, [accessToken]);

  useEffect(() => {
    if (!selectedOrg || !accessToken) return;
    let stale = false;
    projects
      .list(selectedOrg)
      .then((list) => {
        if (!stale) setProjectList(list);
      })
      .catch(() => {
        if (!stale) setProjectList([]);
      })
      .finally(() => {
        if (!stale) setProjectsFor(selectedOrg);
      });
    return () => {
      stale = true;
    };
  }, [selectedOrg, accessToken]);

  function save() {
    if (selectedOrg && selectedProject) {
      setProject(selectedProject, selectedOrg);
      router.push("/dashboard");
    }
  }

  async function createWorkspace() {
    const project = projectName.trim();
    const org = orgName.trim();
    if (!project || (needsOrg && !org)) return;

    setCreating(true);
    setCreateError("");
    try {
      const targetOrg = needsOrg ? (await orgs.create(org)).id : selectedOrg;
      const created = await projects.create(targetOrg, project);
      setProject(created.id, targetOrg);
      router.push("/dashboard");
    } catch (e) {
      const msg = errMessage(e, "Could not create the workspace") || "Could not create the workspace";
      setCreateError(
        /already exists/i.test(msg)
          ? `"${org}" is already taken. Try a different organization name.`
          : msg
      );
      setCreating(false);
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
        <h2 className="text-xs uppercase tracking-wide text-muted-foreground mb-3">
          {onboarding ? "Create your workspace" : "Active project"}
        </h2>
        <div className="border border-border rounded-lg p-4 space-y-4 bg-card">
          {loading ? (
            <Skeleton className="h-9 w-full" />
          ) : onboarding ? (
            <>
              <p className="text-xs text-muted-foreground">
                {needsOrg
                  ? "Name your organization and its first project. Queues, jobs and workers all live inside a project."
                  : "This organization has no projects yet. Name its first one to get started."}
              </p>

              {createError && (
                <Alert variant="destructive" className="rounded-lg">
                  <AlertCircle className="size-4" aria-hidden="true" />
                  <AlertDescription>{createError}</AlertDescription>
                </Alert>
              )}

              {needsOrg && (
                <div className="space-y-1.5">
                  <Label htmlFor="new-org">Organization name</Label>
                  <Input
                    id="new-org"
                    value={orgName}
                    onChange={(e) => setOrgNameInput(e.target.value)}
                    placeholder="Acme Inc"
                    autoComplete="organization"
                  />
                  <p className="text-[11px] text-muted-foreground">
                    Must be unique across JobFlow. Pick something specific to you.
                  </p>
                </div>
              )}

              <div className="space-y-1.5">
                <Label htmlFor="new-project">Project name</Label>
                <Input
                  id="new-project"
                  value={projectName}
                  onChange={(e) => setProjectName(e.target.value)}
                  placeholder="Production"
                />
              </div>

              <Button
                onClick={createWorkspace}
                disabled={creating || !projectName.trim() || (needsOrg && !orgName.trim())}
                className="w-full bg-amber-500 hover:bg-amber-400 text-black"
              >
                {creating ? "Creating…" : "Save and continue"}
              </Button>
            </>
          ) : (
            <>
              <div className="space-y-1.5">
                <Label htmlFor="org">Organization</Label>
                <select
                  id="org"
                  value={selectedOrg}
                  onChange={(e) => { setOrgChoice(e.target.value); setProjectChoice(""); }}
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
                    onChange={(e) => setProjectChoice(e.target.value)}
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
                      {copied ? <CheckIcon size={15} className="text-emerald-400" /> : <CopyIcon size={15} />}
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
