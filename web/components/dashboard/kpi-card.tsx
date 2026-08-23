"use client";

import { motion } from "framer-motion";
import { ArrowDown, ArrowUp, type LucideIcon } from "lucide-react";
import { Area, AreaChart, ResponsiveContainer } from "recharts";
import { Card } from "@/components/ui/card";
import { NumberTicker } from "@/components/ui/number-ticker";
import { Ripple } from "@/components/ui/ripple";
import { cn } from "@/lib/utils";

export interface KpiProps {
  label: string;
  value: number;
  suffix?: string;
  decimals?: number;
  icon: LucideIcon;
  trend?: { value: number; label: string };
  spark?: number[];
  accent?: string;
  ripple?: boolean;
  index?: number;
}

export function KpiCard({
  label, value, suffix, decimals = 0, icon: Icon, trend, spark, accent, ripple, index = 0,
}: KpiProps) {
  const data = (spark ?? []).map((v, i) => ({ i, v }));
  const up = (trend?.value ?? 0) >= 0;
  const color = accent ?? "var(--color-primary)";

  return (
    <motion.div
      initial={{ opacity: 0, scale: 0.97 }}
      animate={{ opacity: 1, scale: 1 }}
      transition={{ duration: 0.2, delay: index * 0.05 }}
    >
      <Card className="relative isolate overflow-hidden rounded-xl p-5 transition-shadow hover:shadow-md">
        {ripple && (
          <div className="pointer-events-none absolute inset-0 -z-10 opacity-[0.12]">
            <Ripple mainCircleSize={100} numCircles={5} />
          </div>
        )}
        <div className="flex items-start justify-between gap-2">
          <p className="text-xs font-medium text-muted-foreground">{label}</p>
          <Icon className="size-4 shrink-0" style={{ color }} aria-hidden="true" />
        </div>

        <p className="mt-2 flex items-baseline gap-0.5 text-3xl font-bold tracking-tight">
          <NumberTicker value={value} decimalPlaces={decimals} className="tabular-nums" />
          {suffix && <span className="text-lg font-semibold text-muted-foreground">{suffix}</span>}
        </p>

        <div className="mt-2 flex items-end justify-between gap-3">
          {trend ? (
            <p className={cn("flex items-center gap-1 text-[11px]", up ? "text-state-completed" : "text-destructive")}>
              {up ? <ArrowUp className="size-3" aria-hidden="true" /> : <ArrowDown className="size-3" aria-hidden="true" />}
              <span className="font-medium">{Math.abs(trend.value).toFixed(1)}%</span>
              <span className="text-muted-foreground">{trend.label}</span>
            </p>
          ) : (
            <span className="text-[11px] text-muted-foreground">live</span>
          )}

          {data.length > 1 && (
            <div className="h-8 w-20 shrink-0" aria-hidden="true">
              <ResponsiveContainer width="100%" height="100%">
                <AreaChart data={data} margin={{ top: 2, right: 0, bottom: 0, left: 0 }}>
                  <defs>
                    <linearGradient id={`spark-${label.replace(/\s/g, "")}`} x1="0" y1="0" x2="0" y2="1">
                      <stop offset="0%" stopColor={color} stopOpacity={0.5} />
                      <stop offset="100%" stopColor={color} stopOpacity={0} />
                    </linearGradient>
                  </defs>
                  <Area
                    type="monotone"
                    dataKey="v"
                    stroke={color}
                    strokeWidth={1.5}
                    fill={`url(#spark-${label.replace(/\s/g, "")})`}
                    isAnimationActive={false}
                  />
                </AreaChart>
              </ResponsiveContainer>
            </div>
          )}
        </div>
      </Card>
    </motion.div>
  );
}
