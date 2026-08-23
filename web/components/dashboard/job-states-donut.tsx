"use client";

import { useCallback, useState } from "react";
import { motion, AnimatePresence } from "framer-motion";
import { DonutChart, type DonutChartSegment } from "@/components/ui/donut-chart";
import { cn } from "@/lib/utils";

export interface JobStateSlice extends DonutChartSegment {
  state: string;
}

export function JobStatesDonut({ data, total }: { data: JobStateSlice[]; total: number }) {
  const [hoveredSegment, setHoveredSegment] = useState<string | null>(null);

  // Stable identity: the DonutChart effect depends on this prop.
  const handleSegmentHover = useCallback(
    (segment: DonutChartSegment | null) => setHoveredSegment(segment?.label ?? null),
    []
  );

  const activeSegment = data.find((segment) => segment.label === hoveredSegment);

  const displayValue = activeSegment?.value ?? total;
  const displayLabel = activeSegment?.label ?? "Total Jobs";
  const displayPercentage = activeSegment && total ? (activeSegment.value / total) * 100 : 100;

  return (
    <div className="flex flex-col items-center justify-center space-y-4">
      <div className="relative flex items-center justify-center">
        <DonutChart
          data={data}
          totalValue={total}
          size={200}
          strokeWidth={24}
          animationDuration={1.2}
          animationDelayPerSegment={0.05}
          highlightOnHover={true}
          onSegmentHover={handleSegmentHover}
          centerContent={
            <AnimatePresence mode="wait">
              <motion.div
                key={displayLabel}
                initial={{ opacity: 0, scale: 0.9 }}
                animate={{ opacity: 1, scale: 1 }}
                exit={{ opacity: 0, scale: 0.9 }}
                transition={{ duration: 0.2, ease: "circOut" }}
                className="flex flex-col items-center justify-center text-center"
              >
                <p className="max-w-[120px] truncate text-xs font-medium text-muted-foreground">
                  {displayLabel}
                </p>
                <p className="text-3xl font-bold tabular-nums text-foreground">
                  {displayValue.toLocaleString()}
                </p>
                {activeSegment && (
                  <p className="text-sm font-medium tabular-nums text-muted-foreground">
                    [{displayPercentage.toFixed(0)}%]
                  </p>
                )}
              </motion.div>
            </AnimatePresence>
          }
        />
      </div>

      <div className="flex w-full flex-col space-y-1 border-t border-border pt-4">
        {data.map((segment, index) => (
          <motion.div
            key={segment.state}
            initial={{ opacity: 0, x: -20 }}
            animate={{ opacity: 1, x: 0 }}
            transition={{ delay: 1.2 + index * 0.1, duration: 0.4 }}
            className={cn(
              "flex cursor-pointer items-center justify-between rounded-md p-1.5 transition-all duration-200",
              hoveredSegment === segment.label && "bg-muted"
            )}
            onMouseEnter={() => setHoveredSegment(segment.label)}
            onMouseLeave={() => setHoveredSegment(null)}
          >
            <div className="flex items-center space-x-3">
              <span
                className="size-2.5 shrink-0 rounded-full"
                style={{ backgroundColor: segment.color }}
                aria-hidden="true"
              />
              <span className="text-xs font-medium text-foreground">{segment.label}</span>
            </div>
            <div className="flex items-center gap-3">
              <span className="font-mono text-xs font-semibold tabular-nums text-foreground">
                {segment.value}
              </span>
              <span className="w-10 text-right text-xs text-muted-foreground">
                {total ? ((segment.value / total) * 100).toFixed(1) : "0.0"}%
              </span>
            </div>
          </motion.div>
        ))}
      </div>
    </div>
  );
}
