"use client";

import { useMemo } from "react";
import { TrendingDown, TrendingUp } from "lucide-react";
import { CartesianGrid, ComposedChart, Line, ReferenceLine, XAxis, YAxis } from "recharts";
import { ChartConfig, ChartContainer, ChartTooltip } from "@/components/ui/chart";
import { cn } from "@/lib/utils";

export interface ThroughputPoint {
  t: string;
  completed: number;
  failed: number;
  enqueued?: number;
}

const ACCENT = "var(--color-purple-500, oklch(0.627 0.265 303.9))";

const chartConfig = {
  value: {
    label: "Completed",
    color: ACCENT,
  },
} satisfies ChartConfig;

interface TooltipProps {
  active?: boolean;
  payload?: Array<{ payload: { date: string; value: number; failed: number } }>;
}

const CustomTooltip = ({ active, payload }: TooltipProps) => {
  if (active && payload && payload.length) {
    const data = payload[0].payload;
    const finished = data.value + data.failed;
    const rate = finished ? (data.value / finished) * 100 : 100;
    return (
      <div className="bg-popover border border-border rounded-lg p-3 shadow-lg">
        <div className="text-sm text-muted-foreground mb-1">{data.date}</div>
        <div className="flex items-center gap-2">
          <div className="text-base font-bold">{data.value.toLocaleString()}</div>
          <div className="text-[11px] text-emerald-600">{rate.toFixed(1)}% success</div>
        </div>
      </div>
    );
  }
  return null;
};

export function ThroughputChart({ data, height }: { data: ThroughputPoint[]; height?: number }) {
  const series = useMemo(
    () => data.map((p) => ({ date: p.t, value: p.completed, failed: p.failed })),
    [data]
  );

  const values = series.map((d) => d.value);
  const totalCompleted = values.reduce((a, b) => a + b, 0);
  const totalFailed = series.reduce((a, d) => a + d.failed, 0);
  const highValue = values.length ? Math.max(...values) : 0;
  const lowValue = values.length ? Math.min(...values) : 0;
  const first = values[0] ?? 0;
  const last = values[values.length - 1] ?? 0;
  const change = first ? ((last - first) / first) * 100 : 0;
  const rising = change >= 0;
  const Trend = rising ? TrendingUp : TrendingDown;
  const peakLabel = series.find((d) => d.value === highValue)?.date;

  return (
    <div className="flex flex-col items-stretch gap-5">
      <div className="mb-5">
        <h2 className="text-base text-muted-foreground font-medium mb-1">Jobs Completed</h2>
        <div className="flex flex-wrap items-baseline gap-1.5 sm:gap-3.5">
          <span className="text-4xl font-bold tabular-nums">{totalCompleted.toLocaleString()}</span>
          <div className={cn("flex items-center gap-1", rising ? "text-emerald-600" : "text-red-600")}>
            <Trend className="w-4 h-4" />
            <span className="font-medium tabular-nums">
              {rising ? "+" : ""}
              {change.toFixed(1)}%
            </span>
            <span className="text-muted-foreground font-normal">Last 24 hours</span>
          </div>
        </div>
      </div>

      <div className="grow">
        <div className="flex items-center justify-between flex-wrap gap-2.5 text-sm mb-2.5">
          <div className="flex items-center gap-6">
            <div className="flex items-center gap-2">
              <span className="text-muted-foreground">Failed:</span>
              <span className="font-semibold tabular-nums">{totalFailed.toLocaleString()}</span>
            </div>
          </div>

          <div className="flex items-center gap-6 text-muted-foreground">
            <span>
              High: <span className="text-sky-600 font-medium tabular-nums">{highValue.toLocaleString()}</span>
            </span>
            <span>
              Low: <span className="text-yellow-600 font-medium tabular-nums">{lowValue.toLocaleString()}</span>
            </span>
            <span>
              Change:{" "}
              <span className={cn("font-medium tabular-nums", rising ? "text-emerald-600" : "text-red-600")}>
                {rising ? "+" : ""}
                {change.toFixed(1)}%
              </span>
            </span>
          </div>
        </div>

        <ChartContainer
          config={chartConfig}
          className="w-full [&_.recharts-curve.recharts-tooltip-cursor]:stroke-initial"
          style={{ height: height ?? 384 }}
        >
          <ComposedChart data={series} margin={{ top: 20, right: 10, left: 5, bottom: 20 }}>
            <defs>
              <pattern id="dotGrid" x="0" y="0" width="20" height="20" patternUnits="userSpaceOnUse">
                <circle cx="10" cy="10" r="1" fill="var(--input)" fillOpacity="0.3" />
              </pattern>
              <filter id="dotShadow" x="-50%" y="-50%" width="200%" height="200%">
                <feDropShadow dx="2" dy="3" stdDeviation="3" floodColor="rgba(0,0,0,0.8)" />
              </filter>
              <filter id="lineShadow" x="-100%" y="-100%" width="300%" height="300%">
                <feDropShadow dx="4" dy="6" stdDeviation="25" floodColor="rgba(59, 130, 246, 0.9)" />
              </filter>
            </defs>

            <rect x="0" y="0" width="100%" height="100%" fill="url(#dotGrid)" style={{ pointerEvents: "none" }} />

            <CartesianGrid
              strokeDasharray="4 8"
              stroke="var(--input)"
              strokeOpacity={1}
              horizontal={true}
              vertical={false}
            />

            {peakLabel && highValue > 0 && (
              <ReferenceLine x={peakLabel} stroke={chartConfig.value.color} strokeDasharray="4 4" strokeWidth={1} />
            )}

            <XAxis
              dataKey="date"
              axisLine={false}
              tickLine={false}
              tick={{ fontSize: 12, fill: chartConfig.value.color }}
              tickMargin={15}
              interval="preserveStartEnd"
              tickCount={5}
            />

            <YAxis
              axisLine={false}
              tickLine={false}
              tick={{ fontSize: 12, fill: chartConfig.value.color }}
              tickFormatter={(value) => value.toLocaleString()}
              tickMargin={15}
              allowDecimals={false}
            />

            <ChartTooltip
              content={<CustomTooltip />}
              cursor={{ strokeDasharray: "3 3", stroke: "var(--muted-foreground)", strokeOpacity: 0.5 }}
            />

            <Line
              type="monotone"
              dataKey="value"
              stroke={chartConfig.value.color}
              strokeWidth={2}
              filter="url(#lineShadow)"
              dot={(props) => {
                const { cx, cy, payload, index } = props;
                if (payload.value === highValue || payload.value === lowValue) {
                  return (
                    <circle
                      key={`dot-${index}`}
                      cx={cx}
                      cy={cy}
                      r={6}
                      fill={chartConfig.value.color}
                      stroke="white"
                      strokeWidth={2}
                      filter="url(#dotShadow)"
                    />
                  );
                }
                return <g key={`dot-${index}`} />;
              }}
              activeDot={{
                r: 6,
                fill: chartConfig.value.color,
                stroke: "white",
                strokeWidth: 2,
                filter: "url(#dotShadow)",
              }}
            />
          </ComposedChart>
        </ChartContainer>
      </div>
    </div>
  );
}
