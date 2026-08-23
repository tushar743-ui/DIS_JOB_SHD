"use client";

import { AlertCircle, RefreshCw, type LucideIcon } from "lucide-react";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { Card } from "@/components/ui/card";

export function ErrorState({ message, onRetry }: { message?: string; onRetry?: () => void }) {
  return (
    <Alert variant="destructive" className="rounded-xl">
      <AlertCircle className="size-4" aria-hidden="true" />
      <AlertTitle>Something went wrong</AlertTitle>
      <AlertDescription className="flex flex-col items-start gap-3">
        <span>{message || "We could not load this data. The API may be unreachable."}</span>
        {onRetry && (
          <Button size="sm" variant="outline" onClick={onRetry} aria-label="Try again" className="rounded-lg">
            <RefreshCw className="size-3.5" aria-hidden="true" /> Try again
          </Button>
        )}
      </AlertDescription>
    </Alert>
  );
}

export function EmptyState({
  icon: Icon, title, description, action,
}: {
  icon: LucideIcon;
  title: string;
  description: string;
  action?: React.ReactNode;
}) {
  return (
    <div className="flex flex-col items-center justify-center gap-3 rounded-xl border border-dashed border-border px-6 py-16 text-center">
      <span className="grid size-11 place-items-center rounded-full bg-muted text-muted-foreground">
        <Icon className="size-5" aria-hidden="true" />
      </span>
      <div>
        <p className="text-sm font-semibold tracking-tight">{title}</p>
        <p className="mt-1 text-xs text-muted-foreground">{description}</p>
      </div>
      {action}
    </div>
  );
}

export function TableSkeleton({ rows = 5, cols = 6 }: { rows?: number; cols?: number }) {
  return (
    <div className="overflow-hidden rounded-xl border border-border">
      {Array.from({ length: rows }).map((_, r) => (
        <div key={r} className="flex h-12 items-center gap-4 border-b border-border px-4 last:border-0">
          {Array.from({ length: cols }).map((_, c) => (
            <Skeleton key={c} className="h-3 flex-1 rounded-md" />
          ))}
        </div>
      ))}
    </div>
  );
}

export function KpiSkeleton({ count = 4 }: { count?: number }) {
  return (
    <div className="grid gap-6 sm:grid-cols-2 xl:grid-cols-4">
      {Array.from({ length: count }).map((_, i) => (
        <Card key={i} className="rounded-xl p-5">
          <Skeleton className="h-3 w-24 rounded-md" />
          <Skeleton className="mt-3 h-8 w-20 rounded-md" />
          <Skeleton className="mt-3 h-3 w-28 rounded-md" />
        </Card>
      ))}
    </div>
  );
}

export function ChartSkeleton({ height = 220 }: { height?: number }) {
  return <Skeleton className="w-full rounded-xl" style={{ height }} />;
}
