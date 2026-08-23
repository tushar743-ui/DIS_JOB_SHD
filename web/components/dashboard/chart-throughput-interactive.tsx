"use client";

import * as React from "react";
import { Area, AreaChart, CartesianGrid, XAxis } from "recharts";

import { useIsMobile } from "@/hooks/use-mobile";
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import {
  ChartContainer,
  ChartTooltip,
  ChartTooltipContent,
  type ChartConfig,
} from "@/components/ui/chart";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";

export interface ThroughputPoint {
  hour: string;
  completed: number;
  failed: number;
}

const chartConfig = {
  completed: { label: "Completed", color: "var(--state-completed)" },
  failed: { label: "Failed", color: "var(--state-failed)" },
} satisfies ChartConfig;

const RANGES = [
  { value: "24h", hours: 24, long: "Last 24 hours", short: "24h" },
  { value: "12h", hours: 12, long: "Last 12 hours", short: "12h" },
  { value: "6h", hours: 6, long: "Last 6 hours", short: "6h" },
];

function formatHour(value: string) {
  return new Date(value).toLocaleTimeString("en-US", {
    hour: "2-digit",
    minute: "2-digit",
    hour12: false,
  });
}

export function ChartThroughputInteractive({
  data,
  queueName,
}: {
  data: ThroughputPoint[];
  queueName?: string;
}) {
  const isMobile = useIsMobile();
  const [chosenRange, setChosenRange] = React.useState<string | null>(null);
  const timeRange = chosenRange ?? (isMobile ? "6h" : "24h");
  const setTimeRange = setChosenRange;

  const active = RANGES.find((r) => r.value === timeRange) ?? RANGES[0];
  const filteredData = React.useMemo(
    () => data.slice(-active.hours),
    [data, active.hours]
  );

  const totals = React.useMemo(
    () =>
      filteredData.reduce(
        (acc, p) => ({
          completed: acc.completed + p.completed,
          failed: acc.failed + p.failed,
        }),
        { completed: 0, failed: 0 }
      ),
    [filteredData]
  );

  return (
    <Card className="@container/card">
      <CardHeader>
        <CardTitle>Throughput</CardTitle>
        <CardDescription>
          <span className="hidden @[540px]/card:block">
            {totals.completed.toLocaleString()} completed · {totals.failed.toLocaleString()} failed
            {queueName ? ` · ${queueName}` : ""}
          </span>
          <span className="@[540px]/card:hidden">{active.short}</span>
        </CardDescription>
        <CardAction>
          <ToggleGroup
            multiple={false}
            value={timeRange ? [timeRange] : []}
            onValueChange={(value) => {
              setTimeRange(value[0] ?? "24h");
            }}
            variant="outline"
            className="hidden *:data-[slot=toggle-group-item]:px-4! @[767px]/card:flex"
          >
            {RANGES.map((r) => (
              <ToggleGroupItem key={r.value} value={r.value}>
                {r.long}
              </ToggleGroupItem>
            ))}
          </ToggleGroup>
          <Select
            value={timeRange}
            onValueChange={(value) => {
              if (value !== null) {
                setTimeRange(value);
              }
            }}
          >
            <SelectTrigger
              className="flex w-40 **:data-[slot=select-value]:block **:data-[slot=select-value]:truncate @[767px]/card:hidden"
              size="sm"
              aria-label="Select a time range"
            >
              <SelectValue placeholder="Last 24 hours" />
            </SelectTrigger>
            <SelectContent className="rounded-xl">
              {RANGES.map((r) => (
                <SelectItem key={r.value} value={r.value} className="rounded-lg">
                  {r.long}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </CardAction>
      </CardHeader>
      <CardContent className="px-2 pt-4 sm:px-6 sm:pt-6">
        <ChartContainer config={chartConfig} className="aspect-auto h-[250px] w-full">
          <AreaChart data={filteredData}>
            <defs>
              <linearGradient id="fillCompleted" x1="0" y1="0" x2="0" y2="1">
                <stop offset="5%" stopColor="var(--color-completed)" stopOpacity={1.0} />
                <stop offset="95%" stopColor="var(--color-completed)" stopOpacity={0.1} />
              </linearGradient>
              <linearGradient id="fillFailed" x1="0" y1="0" x2="0" y2="1">
                <stop offset="5%" stopColor="var(--color-failed)" stopOpacity={0.8} />
                <stop offset="95%" stopColor="var(--color-failed)" stopOpacity={0.1} />
              </linearGradient>
            </defs>
            <CartesianGrid vertical={false} />
            <XAxis
              dataKey="hour"
              tickLine={false}
              axisLine={false}
              tickMargin={8}
              minTickGap={32}
              tickFormatter={formatHour}
            />
            <ChartTooltip
              cursor={false}
              content={
                <ChartTooltipContent
                  labelFormatter={(value) => formatHour(String(value))}
                  indicator="dot"
                />
              }
            />
            <Area
              dataKey="failed"
              type="natural"
              fill="url(#fillFailed)"
              stroke="var(--color-failed)"
              stackId="a"
            />
            <Area
              dataKey="completed"
              type="natural"
              fill="url(#fillCompleted)"
              stroke="var(--color-completed)"
              stackId="a"
            />
          </AreaChart>
        </ChartContainer>
      </CardContent>
    </Card>
  );
}
