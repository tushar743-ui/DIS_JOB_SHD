"use client";

import { useCallback, useState } from "react";
import { motion, AnimatePresence } from "framer-motion";
import { DonutChart, type DonutChartSegment } from "@/components/ui/donut-chart";
import { stateSpec } from "@/components/job-state-badge";
import { cn } from "@/lib/utils";

const LIFECYCLE = [
  "queued", "scheduled", "claimed", "running",
  "retrying", "completed", "failed", "dead", "cancelled",
] as const;

const RING_COLOR: Record<string, string> = {
  queued: "hsl(258 90% 66%)",
  scheduled: "hsl(38 92% 50%)",
  claimed: "hsl(190 88% 42%)",
  running: "hsl(213 94% 58%)",
  retrying: "hsl(25 92% 55%)",
  completed: "hsl(160 84% 39%)",
  failed: "hsl(347 84% 56%)",
  dead: "hsl(350 68% 38%)",
  cancelled: "hsl(215 16% 52%)",
};

const FALLBACK_COLOR = "hsl(215 16% 52%)";

export interface JobStateSlice extends DonutChartSegment {
  state: string;
}

export function buildJobStateSlices(totals: Record<string, number>): JobStateSlice[] {
  const known = new Set<string>(LIFECYCLE);
  const order = [...LIFECYCLE, ...Object.keys(totals).filter((s) => !known.has(s))];

  return order
    .map((state) => ({
      state,
      label: stateSpec(state).label,
      value: totals[state] ?? 0,
      color: RING_COLOR[state] ?? FALLBACK_COLOR,
    }))
    .filter((slice) => slice.value > 0);
}

export function JobStatesDonut({ data, total }: { data: JobStateSlice[]; total: number }) {
  const [hoveredState, setHoveredState] = useState<string | null>(null);

  const handleSegmentHover = useCallback(
    (segment: DonutChartSegment | null) =>
      setHoveredState((segment as JobStateSlice | null)?.state ?? null),
    []
  );

  const active = data.find((slice) => slice.state === hoveredState) ?? null;
  const displayValue = active?.value ?? total;
  const displayLabel = active?.label ?? "Total jobs";
  const displayPercentage = active && total ? (active.value / total) * 100 : 100;

  return (
    <div className="flex flex-col items-center">
      <DonutChart
        data={data}
        totalValue={total}
        size={196}
        strokeWidth={20}
        gapDegrees={2.5}
        animationDuration={1.1}
        animationDelayPerSegment={0.06}
        activeLabel={active?.label ?? null}
        onSegmentHover={handleSegmentHover}
        trackColor="hsl(var(--border) / 0.35)"
        ariaLabel={`Job states: ${data.map((d) => `${d.label} ${d.value}`).join(", ")}`}
        centerContent={
          <AnimatePresence mode="wait">
            <motion.div
              key={displayLabel}
              initial={{ opacity: 0, scale: 0.94 }}
              animate={{ opacity: 1, scale: 1 }}
              exit={{ opacity: 0, scale: 0.94 }}
              transition={{ duration: 0.18, ease: "circOut" }}
              className="flex flex-col items-center justify-center text-center"
            >
              <p className="text-[28px] font-bold leading-none tabular-nums tracking-tight text-foreground">
                {displayValue.toLocaleString()}
              </p>
              <p className="mt-1.5 max-w-[110px] truncate text-[10px] font-medium uppercase tracking-[0.14em] text-muted-foreground">
                {displayLabel}
              </p>
              {active && (
                <p
                  className="mt-1 font-mono text-xs font-semibold tabular-nums"
                  style={{ color: active.color }}
                >
                  {displayPercentage.toFixed(1)}%
                </p>
              )}
            </motion.div>
          </AnimatePresence>
        }
      />

      <ul className="mt-5 w-full space-y-px border-t border-border pt-3">
        {data.map((slice, index) => {
          const isActive = hoveredState === slice.state;
          return (
            <motion.li
              key={slice.state}
              initial={{ opacity: 0, x: -12 }}
              animate={{ opacity: 1, x: 0 }}
              transition={{ delay: 0.45 + index * 0.05, duration: 0.35 }}
              onMouseEnter={() => setHoveredState(slice.state)}
              onMouseLeave={() => setHoveredState(null)}
              className={cn(
                "flex cursor-default items-center gap-2.5 rounded-md px-2 py-1.5 transition-colors duration-200",
                isActive ? "bg-muted" : "hover:bg-muted/50"
              )}
            >
              <span
                className="size-2 shrink-0 rounded-full transition-transform duration-200"
                style={{
                  background: slice.color,
                  boxShadow: isActive ? `0 0 0 3px color-mix(in oklab, ${slice.color} 22%, transparent)` : "none",
                }}
                aria-hidden="true"
              />
              <span className="flex-1 truncate text-xs font-medium text-foreground">
                {slice.label}
              </span>
              <span className="font-mono text-xs font-semibold tabular-nums text-foreground">
                {slice.value.toLocaleString()}
              </span>
              <span className="w-11 text-right font-mono text-[11px] tabular-nums text-muted-foreground">
                {total ? ((slice.value / total) * 100).toFixed(1) : "0.0"}%
              </span>
            </motion.li>
          );
        })}
      </ul>
    </div>
  );
}
