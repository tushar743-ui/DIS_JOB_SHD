"use client";

import { TrendingDown, TrendingUp } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import {
  Card,
  CardAction,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";

export interface SectionCard {
  label: string;
  value: string;
  delta?: number;
  deltaSuffix?: string;
  headline: string;
  detail: string;
}

function DeltaBadge({ delta, suffix }: { delta: number; suffix: string }) {
  const up = delta >= 0;
  const Icon = up ? TrendingUp : TrendingDown;
  return (
    <Badge variant="outline">
      <Icon className="size-3" aria-hidden="true" />
      {up ? "+" : ""}
      {delta.toFixed(1)}
      {suffix}
    </Badge>
  );
}

export function SectionCards({ cards }: { cards: SectionCard[] }) {
  return (
    <div className="grid grid-cols-1 gap-4 *:data-[slot=card]:bg-linear-to-t *:data-[slot=card]:from-primary/5 *:data-[slot=card]:to-card *:data-[slot=card]:shadow-xs @xl/main:grid-cols-2 @5xl/main:grid-cols-4 dark:*:data-[slot=card]:bg-card">
      {cards.map((card) => {
        const up = (card.delta ?? 0) >= 0;
        const Icon = up ? TrendingUp : TrendingDown;
        return (
          <Card key={card.label} className="@container/card">
            <CardHeader>
              <CardDescription>{card.label}</CardDescription>
              <CardTitle className="text-2xl font-semibold tabular-nums @[250px]/card:text-3xl">
                {card.value}
              </CardTitle>
              {card.delta !== undefined && (
                <CardAction>
                  <DeltaBadge delta={card.delta} suffix={card.deltaSuffix ?? "%"} />
                </CardAction>
              )}
            </CardHeader>
            <CardFooter className="flex-col items-start gap-1.5 text-sm">
              <div className="line-clamp-1 flex gap-2 font-medium">
                {card.headline}
                {card.delta !== undefined && <Icon className="size-4" aria-hidden="true" />}
              </div>
              <div className="text-muted-foreground">{card.detail}</div>
            </CardFooter>
          </Card>
        );
      })}
    </div>
  );
}
