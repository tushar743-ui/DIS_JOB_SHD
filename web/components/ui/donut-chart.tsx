"use client";

import * as React from "react";
import { cn } from "@/lib/utils";
import { motion, AnimatePresence } from "framer-motion";

export interface DonutChartSegment {
  value: number;
  color: string;
  label: string;
  // eslint-disable-next-line @typescript-eslint/no-explicit-any -- keeps the upstream component's open data shape
  [key: string]: any;
}

interface DonutChartProps extends React.HTMLAttributes<HTMLDivElement> {
  data: DonutChartSegment[];
  totalValue?: number;
  size?: number;
  strokeWidth?: number;
  animationDuration?: number;
  animationDelayPerSegment?: number;
  highlightOnHover?: boolean;
  centerContent?: React.ReactNode;
  gapDegrees?: number;
  gradient?: boolean;
  strokeLinecap?: "butt" | "round";
  trackColor?: string;
  activeLabel?: string | null;
  onSegmentHover?: (segment: DonutChartSegment | null) => void;
  ariaLabel?: string;
}

const DonutChart = React.forwardRef<HTMLDivElement, DonutChartProps>(
  (
    {
      data,
      totalValue: propTotalValue,
      size = 200,
      strokeWidth = 20,
      animationDuration = 1,
      animationDelayPerSegment = 0.05,
      highlightOnHover = true,
      centerContent,
      gapDegrees = 2,
      gradient = true,
      strokeLinecap = "butt",
      trackColor = "hsl(var(--border) / 0.5)",
      activeLabel,
      onSegmentHover,
      ariaLabel,
      className,
      ...props
    },
    ref
  ) => {
    const [hoveredSegment, setHoveredSegment] =
      React.useState<DonutChartSegment | null>(null);

    const internalTotalValue = React.useMemo(
      () =>
        propTotalValue || data.reduce((sum, segment) => sum + segment.value, 0),
      [data, propTotalValue]
    );

    const radius = size / 2 - strokeWidth / 2;
    const circumference = 2 * Math.PI * radius;
    const drawn = data.filter((segment) => segment.value > 0);
    const gap = drawn.length > 1 ? (gapDegrees / 360) * circumference : 0;
    const gradientId = React.useId();
    let cumulativePercentage = 0;

    const hoverCallback = React.useRef(onSegmentHover);
    hoverCallback.current = onSegmentHover;
    React.useEffect(() => {
      hoverCallback.current?.(hoveredSegment);
    }, [hoveredSegment]);

    const highlighted = activeLabel ?? hoveredSegment?.label ?? null;

    return (
      <div
        ref={ref}
        className={cn("relative flex items-center justify-center", className)}
        style={{ width: size, height: size }}
        onMouseLeave={() => setHoveredSegment(null)}
        {...props}
      >
        <svg
          width={size}
          height={size}
          viewBox={`0 0 ${size} ${size}`}
          className="overflow-visible -rotate-90"
          role={ariaLabel ? "img" : "presentation"}
          aria-label={ariaLabel}
        >
          {gradient && (
            <defs>
              {drawn.map((segment, index) => (
                <linearGradient
                  key={`${gradientId}-${segment.label || index}`}
                  id={`${gradientId}-${index}`}
                  gradientUnits="userSpaceOnUse"
                  x1="0"
                  y1="0"
                  x2={size}
                  y2={size}
                >
                  <stop
                    offset="0%"
                    stopColor={`color-mix(in oklab, ${segment.color} 84%, black)`}
                  />
                  <stop offset="55%" stopColor={segment.color} />
                  <stop
                    offset="100%"
                    stopColor={`color-mix(in oklab, ${segment.color} 82%, white)`}
                  />
                </linearGradient>
              ))}
            </defs>
          )}

          <circle
            cx={size / 2}
            cy={size / 2}
            r={radius}
            fill="transparent"
            stroke={trackColor}
            strokeWidth={strokeWidth}
          />

          <AnimatePresence>
            {drawn.map((segment, index) => {
              const percentage =
                internalTotalValue === 0
                  ? 0
                  : (segment.value / internalTotalValue) * 100;

              const arcLength = Math.max((percentage / 100) * circumference - gap, 1.5);
              const strokeDasharray = `${arcLength} ${circumference}`;
              const strokeDashoffset =
                (cumulativePercentage / 100) * circumference + gap / 2;

              const isActive = highlighted === segment.label;
              const isDimmed = highlighted !== null && !isActive;

              cumulativePercentage += percentage;

              return (
                <motion.circle
                  key={segment.label || index}
                  cx={size / 2}
                  cy={size / 2}
                  r={radius}
                  fill="transparent"
                  stroke={gradient ? `url(#${gradientId}-${index})` : segment.color}
                  strokeWidth={strokeWidth}
                  strokeDasharray={strokeDasharray}
                  strokeLinecap={strokeLinecap}
                  initial={{ opacity: 0, strokeDashoffset: circumference }}
                  animate={{
                    opacity: isDimmed ? 0.32 : 1,
                    strokeDashoffset: -strokeDashoffset,
                  }}
                  transition={{
                    opacity: { duration: 0.25, delay: index * animationDelayPerSegment },
                    strokeDashoffset: {
                      duration: animationDuration,
                      delay: index * animationDelayPerSegment,
                      ease: [0.16, 1, 0.3, 1],
                    },
                  }}
                  className={cn(
                    "origin-center",
                    highlightOnHover && "cursor-pointer"
                  )}
                  style={{
                    filter: isActive
                      ? `drop-shadow(0 0 10px color-mix(in oklab, ${segment.color} 60%, transparent))`
                      : "none",
                    transform: isActive ? "scale(1.035)" : "scale(1)",
                    transition: "filter 0.2s ease-out, transform 0.2s ease-out",
                  }}
                  onMouseEnter={() =>
                    highlightOnHover && setHoveredSegment(segment)
                  }
                />
              );
            })}
          </AnimatePresence>
        </svg>

        {centerContent && (
          <div
            className="absolute flex flex-col items-center justify-center pointer-events-none"
            style={{
              width: size - strokeWidth * 2.5,
              height: size - strokeWidth * 2.5,
            }}
          >
            {centerContent}
          </div>
        )}
      </div>
    );
  }
);

DonutChart.displayName = "DonutChart";

export { DonutChart };
