"use client";

import { useState } from "react";
import { Check, Copy } from "lucide-react";
import { Card } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { BlurFade } from "@/components/ui/blur-fade";

const BASE = process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080";

interface Route { method: string; path: string; summary: string }
interface Section { title: string; routes: Route[] }

const SECTIONS: Section[] = [
  {
    title: "Auth",
    routes: [
      { method: "POST", path: "/auth/register", summary: "Create a user account" },
      { method: "POST", path: "/auth/login", summary: "Exchange credentials for a token pair" },
      { method: "POST", path: "/auth/refresh", summary: "Rotate a refresh token; the old one is revoked" },
      { method: "POST", path: "/auth/logout", summary: "Revoke the supplied refresh token" },
      { method: "GET", path: "/auth/me", summary: "Current user profile" },
    ],
  },
  {
    title: "Organizations & Projects",
    routes: [
      { method: "GET", path: "/orgs", summary: "List organizations you belong to" },
      { method: "POST", path: "/orgs", summary: "Create an organization" },
      { method: "GET", path: "/orgs/{orgID}/members", summary: "List members" },
      { method: "GET", path: "/orgs/{orgID}/projects", summary: "List projects in an org" },
      { method: "POST", path: "/projects/{projectID}/rotate-key", summary: "Rotate the project API key" },
    ],
  },
  {
    title: "Queues",
    routes: [
      { method: "GET", path: "/projects/{projectID}/queues", summary: "List queues" },
      { method: "POST", path: "/projects/{projectID}/queues", summary: "Create a queue" },
      { method: "PUT", path: "/queues/{queueID}", summary: "Update concurrency, priority, retry policy" },
      { method: "POST", path: "/queues/{queueID}/pause", summary: "Stop workers claiming from this queue" },
      { method: "POST", path: "/queues/{queueID}/resume", summary: "Resume claiming" },
      { method: "GET", path: "/queues/{queueID}/stats", summary: "Counts grouped by job status" },
    ],
  },
  {
    title: "Jobs",
    routes: [
      { method: "GET", path: "/queues/{queueID}/jobs", summary: "List jobs with limit, offset and status filters" },
      { method: "POST", path: "/queues/{queueID}/jobs", summary: "Enqueue immediate, delayed or cron jobs" },
      { method: "POST", path: "/queues/{queueID}/jobs/batch", summary: "Enqueue many jobs in one call" },
      { method: "GET", path: "/jobs/{jobID}", summary: "Fetch a single job" },
      { method: "DELETE", path: "/jobs/{jobID}", summary: "Cancel a job" },
      { method: "POST", path: "/jobs/{jobID}/retry", summary: "Re-enqueue with a reset attempt count" },
      { method: "GET", path: "/jobs/{jobID}/logs", summary: "Execution log lines" },
      { method: "GET", path: "/jobs/{jobID}/executions", summary: "Per-attempt execution records" },
    ],
  },
  {
    title: "Workers, DLQ & Metrics",
    routes: [
      { method: "GET", path: "/projects/{projectID}/workers", summary: "List registered workers" },
      { method: "GET", path: "/workers/{workerID}", summary: "Worker detail plus heartbeats" },
      { method: "GET", path: "/queues/{queueID}/dlq", summary: "Dead letter entries for a queue" },
      { method: "POST", path: "/dlq/{dlqID}/retry", summary: "Re-enqueue a dead-lettered job" },
      { method: "DELETE", path: "/dlq/{dlqID}", summary: "Discard a dead-lettered job" },
      { method: "GET", path: "/projects/{projectID}/metrics", summary: "Project-wide counts and active workers" },
      { method: "GET", path: "/queues/{queueID}/metrics", summary: "24h throughput and average duration" },
    ],
  },
];

const METHOD_TOKEN: Record<string, string> = {
  GET: "--state-running",
  POST: "--state-completed",
  PUT: "--state-scheduled",
  DELETE: "--state-failed",
};

function RouteRow({ route }: { route: Route }) {
  const [copied, setCopied] = useState(false);
  const full = `${BASE}/api/v1${route.path}`;

  async function copy() {
    await navigator.clipboard.writeText(full);
    setCopied(true);
    setTimeout(() => setCopied(false), 1500);
  }

  return (
    <li className="flex items-center gap-3 border-b border-border px-4 py-2.5 last:border-0">
      <Badge
        variant="outline"
        className="w-16 shrink-0 justify-center rounded-full font-mono text-[10px]"
        style={{
          borderColor: `hsl(var(${METHOD_TOKEN[route.method]}) / 0.4)`,
          color: `hsl(var(${METHOD_TOKEN[route.method]}))`,
        }}
      >
        {route.method}
      </Badge>
      <code className="min-w-0 shrink-0 font-mono text-xs">{route.path}</code>
      <span className="min-w-0 flex-1 truncate text-xs text-muted-foreground">{route.summary}</span>
      <button
        onClick={copy}
        aria-label={`Copy URL for ${route.method} ${route.path}`}
        className="grid size-7 shrink-0 place-items-center rounded-md text-muted-foreground transition-colors hover:bg-accent hover:text-foreground focus-visible:ring-2 focus-visible:ring-primary"
      >
        {copied ? <Check className="size-3.5 text-state-completed" /> : <Copy className="size-3.5" />}
      </button>
    </li>
  );
}

export default function DocsPage() {
  return (
    <div className="space-y-6">
      <Card className="rounded-xl p-5">
        <h2 className="text-sm font-semibold tracking-tight">Base URL</h2>
        <p className="mt-1 font-mono text-xs text-muted-foreground">{BASE}/api/v1</p>
        <p className="mt-3 text-xs text-muted-foreground">
          Every route below the auth group expects an <code className="font-mono">Authorization: Bearer &lt;access_token&gt;</code> header.
          Access tokens are short-lived; the client refreshes them automatically against <code className="font-mono">/auth/refresh</code>,
          which rotates the refresh token and revokes the previous one.
        </p>
      </Card>

      {SECTIONS.map((section, i) => (
        <BlurFade key={section.title} delay={0.05 * i}>
          <section>
            <h2 className="mb-2 text-sm font-semibold tracking-tight">{section.title}</h2>
            <ul className="overflow-hidden rounded-xl border border-border">
              {section.routes.map((r) => <RouteRow key={r.method + r.path} route={r} />)}
            </ul>
          </section>
        </BlurFade>
      ))}
    </div>
  );
}
