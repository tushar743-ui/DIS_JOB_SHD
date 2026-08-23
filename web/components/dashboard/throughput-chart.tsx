"use client";

import { CartesianGrid, ComposedChart, Line, XAxis, YAxis } from "recharts";
import { ChartConfig, ChartContainer, ChartTooltip } from "@/components/ui/chart";

export interface ThroughputPoint {
  t: string;
  completed: number;
  failed: number;
}

const chartConfig = {
  completed: { label: "Completed", color: "hsl(var(--state-completed))" },
  failed: { label: "Failed", color: "hsl(var(--state-failed))" },
} satisfies ChartConfig;

interface TooltipProps {
  active?: boolean;
  payload?: Array<{ payload: ThroughputPoint }>;
}

function ThroughputTooltip({ active, payload }: TooltipProps) {
  if (!active || !payload?.length) return null;
  const point = payload[0].payload;
  const total = point.completed + point.failed;
  const rate = total ? (point.completed / total) * 100 : 100;

  return (
    <div className="rounded-lg border border-border bg-popover p-3 shadow-lg">
      <div className="mb-1.5 text-[11px] text-muted-foreground">{point.t}</div>
      <div className="grid gap-1 text-xs">
        <div className="flex items-center justify-between gap-6">
          <span className="flex items-center gap-1.5">
            <span className="size-2 rounded-full bg-state-completed" aria-hidden="true" />
            Completed
          </span>
          <span className="font-mono font-medium tabular-nums">{point.completed.toLocaleString()}</span>
        </div>
        <div className="flex items-center justify-between gap-6">
          <span className="flex items-center gap-1.5">
            <span className="size-2 rounded-full bg-state-failed" aria-hidden="true" />
            Failed
          </span>
          <span className="font-mono font-medium tabular-nums">{point.failed.toLocaleString()}</span>
        </div>
        <div className="mt-0.5 border-t border-border pt-1 text-[11px] text-muted-foreground">
          Success rate <span className="font-medium text-foreground">{rate.toFixed(1)}%</span>
        </div>
      </div>
    </div>
  );
}

export function ThroughputChart({ data }: { data: ThroughputPoint[] }) {
  const peak = Math.max(...data.map((d) => d.completed), 0);

  return (
    <ChartContainer config={chartConfig} className="h-[220px] w-full">
      <ComposedChart data={data} margin={{ top: 12, right: 8, left: -12, bottom: 4 }}>
        <defs>
          <pattern id="throughputDotGrid" x="0" y="0" width="20" height="20" patternUnits="userSpaceOnUse">
            <circle cx="10" cy="10" r="1" fill="hsl(var(--input))" fillOpacity="0.35" />
          </pattern>
          <filter id="throughputGlow" x="-100%" y="-100%" width="300%" height="300%">
            <feDropShadow dx="0" dy="4" stdDeviation="14" floodColor="hsl(var(--state-completed))" floodOpacity="0.35" />
          </filter>
          <filter id="throughputDotShadow" x="-50%" y="-50%" width="200%" height="200%">
            <feDropShadow dx="0" dy="2" stdDeviation="2" floodOpacity="0.35" />
          </filter>
        </defs>

        <rect x="0" y="0" width="100%" height="100%" fill="url(#throughputDotGrid)" style={{ pointerEvents: "none" }} />

        <CartesianGrid strokeDasharray="4 8" stroke="hsl(var(--input))" horizontal vertical={false} />

        <XAxis
          dataKey="t"
          axisLine={false}
          tickLine={false}
          tick={{ fontSize: 10, fill: "hsl(var(--muted-foreground))" }}
          tickMargin={10}
          interval="preserveStartEnd"
          minTickGap={24}
        />
        <YAxis
          axisLine={false}
          tickLine={false}
          tick={{ fontSize: 10, fill: "hsl(var(--muted-foreground))" }}
          tickMargin={8}
          width={44}
          allowDecimals={false}
        />

        <ChartTooltip
          content={<ThroughputTooltip />}
          cursor={{ strokeDasharray: "3 3", stroke: "hsl(var(--muted-foreground))", strokeOpacity: 0.5 }}
        />

        <Line
          type="monotone"
          dataKey="completed"
          stroke="hsl(var(--state-completed))"
          strokeWidth={2}
          filter="url(#throughputGlow)"
          dot={(props) => {
            const { cx, cy, payload, index } = props;
            if (peak > 0 && payload.completed === peak) {
              return (
                <circle
                  key={`peak-${index}`}
                  cx={cx}
                  cy={cy}
                  r={5}
                  fill="hsl(var(--state-completed))"
                  stroke="hsl(var(--background))"
                  strokeWidth={2}
                  filter="url(#throughputDotShadow)"
                />
              );
            }
            return <g key={`dot-${index}`} />;
          }}
          activeDot={{
            r: 5,
            fill: "hsl(var(--state-completed))",
            stroke: "hsl(var(--background))",
            strokeWidth: 2,
          }}
        />
        <Line
          type="monotone"
          dataKey="failed"
          stroke="hsl(var(--state-failed))"
          strokeWidth={1.5}
          strokeDasharray="5 4"
          dot={false}
          activeDot={{
            r: 4,
            fill: "hsl(var(--state-failed))",
            stroke: "hsl(var(--background))",
            strokeWidth: 2,
          }}
        />
      </ComposedChart>
    </ChartContainer>
  );
}
