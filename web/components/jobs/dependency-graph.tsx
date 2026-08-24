"use client";

import { useRef } from "react";
import Link from "next/link";
import { GitBranch, Lock } from "lucide-react";
import { AnimatedBeam } from "@/components/ui/animated-beam";
import { StateDot, stateSpec } from "@/components/job-state-badge";
import { EmptyState } from "@/components/states";
import { useJobDependencies } from "@/hooks/use-data";
import type { DependencyEdge } from "@/lib/api";
import { cn } from "@/lib/utils";

type NodeRef = { current: HTMLElement | null };

function useNodeRefs() {
  const map = useRef(new Map<string, NodeRef>());
  return (id: string): NodeRef => {
    if (!map.current.has(id)) map.current.set(id, { current: null });
    return map.current.get(id)!;
  };
}

function tokenColor(status: string) {
  return `var(${stateSpec(status).token})`;
}

function Node({
  edge, nodeRef, blocked, align,
}: {
  edge: DependencyEdge;
  nodeRef: NodeRef;
  blocked?: boolean;
  align: "left" | "right";
}) {
  return (
    <Link
      ref={nodeRef as React.Ref<HTMLAnchorElement>}
      href={`/jobs/${edge.job_id}`}
      className={cn(
        "relative z-10 flex w-44 items-center gap-2 rounded-lg border bg-card px-3 py-2 shadow-sm transition-colors hover:border-primary",
        blocked ? "border-state-failed/40" : "border-border",
        align === "right" && "flex-row-reverse text-right"
      )}
    >
      <StateDot state={edge.status} />
      <span className="min-w-0 flex-1">
        <span className="block truncate text-xs font-medium">{edge.type}</span>
        <span className="block truncate font-mono text-[10px] text-muted-foreground">{edge.job_id.slice(0, 8)}</span>
      </span>
      {blocked && <Lock className="size-3 shrink-0 text-state-failed" aria-hidden="true" />}
    </Link>
  );
}

export function DependencyGraph({ jobId, jobType, jobStatus }: { jobId: string; jobType: string; jobStatus: string }) {
  const { data } = useJobDependencies(jobId);
  const containerRef = useRef<HTMLDivElement>(null);
  const centerRef = useRef<HTMLDivElement>(null);
  const getNodeRef = useNodeRefs();

  if (!data) return null;

  const { depends_on: upstream, dependents: downstream, blocked_by: blockedBy, satisfied } = data;
  if (upstream.length === 0 && downstream.length === 0) {
    return (
      <EmptyState
        icon={GitBranch}
        title="No dependencies"
        description="This job doesn't depend on other jobs and nothing depends on it."
      />
    );
  }

  const rowHeight = 56;
  const height = Math.max(upstream.length, downstream.length, 1) * rowHeight + 24;

  return (
    <div>
      {!satisfied && (
        <p className="mb-4 flex items-center gap-1.5 text-xs font-medium text-state-failed">
          <Lock className="size-3.5" aria-hidden="true" />
          Blocked on {blockedBy.length} upstream job{blockedBy.length === 1 ? "" : "s"}
        </p>
      )}

      <div ref={containerRef} className="relative flex items-center justify-between" style={{ height }}>
        <div className="flex flex-col justify-center gap-3">
          {upstream.map((edge) => (
            <Node key={edge.job_id} edge={edge} nodeRef={getNodeRef(edge.job_id)} align="left" blocked={blockedBy.includes(edge.job_id)} />
          ))}
        </div>

        <div
          ref={centerRef}
          className="relative z-10 flex w-48 flex-col items-center gap-1 rounded-lg border-2 border-primary bg-card px-3 py-2.5 text-center shadow-md"
        >
          <StateDot state={jobStatus} />
          <span className="truncate text-xs font-semibold">{jobType}</span>
          <span className="font-mono text-[10px] text-muted-foreground">{jobId.slice(0, 8)}</span>
        </div>

        <div className="flex flex-col justify-center gap-3">
          {downstream.map((edge) => (
            <Node key={edge.job_id} edge={edge} nodeRef={getNodeRef(edge.job_id)} align="right" />
          ))}
        </div>

        {upstream.map((edge, i) => (
          <AnimatedBeam
            key={`up-${edge.job_id}`}
            containerRef={containerRef}
            fromRef={getNodeRef(edge.job_id)}
            toRef={centerRef}
            curvature={(i - (upstream.length - 1) / 2) * 30}
            duration={4 + i * 0.4}
            gradientStartColor={tokenColor(edge.status)}
            gradientStopColor={tokenColor(jobStatus)}
          />
        ))}
        {downstream.map((edge, i) => (
          <AnimatedBeam
            key={`down-${edge.job_id}`}
            containerRef={containerRef}
            fromRef={centerRef}
            toRef={getNodeRef(edge.job_id)}
            curvature={(i - (downstream.length - 1) / 2) * 30}
            duration={4 + i * 0.4}
            gradientStartColor={tokenColor(jobStatus)}
            gradientStopColor={tokenColor(edge.status)}
          />
        ))}
      </div>
    </div>
  );
}
