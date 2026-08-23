"use client";

import * as React from "react";
import { useRouter } from "next/navigation";
import type { JobRow } from "@/hooks/use-data";
import { JobStateBadge } from "@/components/job-state-badge";
import { fmtRelative } from "@/lib/status";
import { Badge } from "@/components/ui/badge";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";

const TABS = [
  { value: "all", label: "All jobs", match: () => true },
  { value: "running", label: "Running", match: (j: JobRow) => j.status === "running" || j.status === "claimed" },
  { value: "queued", label: "Queued", match: (j: JobRow) => j.status === "queued" || j.status === "scheduled" },
  { value: "failed", label: "Failed", match: (j: JobRow) => j.status === "failed" || j.status === "dead" },
];

const LIMIT = 10;

export function RecentJobsTable({ jobs }: { jobs: JobRow[] }) {
  const router = useRouter();
  const [tab, setTab] = React.useState("all");

  const counts = React.useMemo(
    () =>
      Object.fromEntries(
        TABS.map((t) => [t.value, jobs.filter(t.match).length])
      ) as Record<string, number>,
    [jobs]
  );

  return (
    <Tabs
      value={tab}
      onValueChange={(v) => v && setTab(v)}
      className="w-full flex-col justify-start gap-6"
    >
      <div className="flex items-center justify-between">
        <Select value={tab} onValueChange={(v) => v && setTab(v)}>
          <SelectTrigger className="flex w-40 @4xl/main:hidden" size="sm" aria-label="Select a view">
            <SelectValue placeholder="All jobs" />
          </SelectTrigger>
          <SelectContent>
            {TABS.map((t) => (
              <SelectItem key={t.value} value={t.value}>
                {t.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <TabsList className="hidden @4xl/main:flex">
          {TABS.map((t) => (
            <TabsTrigger key={t.value} value={t.value} className="gap-1.5">
              {t.label}
              {counts[t.value] > 0 && (
                <Badge variant="secondary" className="px-1.5 tabular-nums">
                  {counts[t.value]}
                </Badge>
              )}
            </TabsTrigger>
          ))}
        </TabsList>
        <span className="text-xs text-muted-foreground">
          showing {Math.min(LIMIT, counts[tab] ?? 0)} of {counts[tab] ?? 0}
        </span>
      </div>

      {TABS.map((t) => {
        const rows = jobs.filter(t.match).slice(0, LIMIT);
        return (
          <TabsContent key={t.value} value={t.value}>
            <div className="overflow-hidden rounded-lg border">
              <Table>
                <TableHeader className="bg-muted sticky top-0 z-10">
                  <TableRow>
                    <TableHead>Type</TableHead>
                    <TableHead>Queue</TableHead>
                    <TableHead>Status</TableHead>
                    <TableHead className="text-right">Attempts</TableHead>
                    <TableHead className="text-right">Created</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {rows.length === 0 ? (
                    <TableRow>
                      <TableCell colSpan={5} className="h-24 text-center text-muted-foreground">
                        No jobs in this view.
                      </TableCell>
                    </TableRow>
                  ) : (
                    rows.map((job) => (
                      <TableRow
                        key={job.id}
                        onClick={() => router.push(`/jobs/${job.id}`)}
                        className="cursor-pointer"
                      >
                        <TableCell className="font-medium">{job.type}</TableCell>
                        <TableCell className="text-muted-foreground">{job.queue_name}</TableCell>
                        <TableCell>
                          <JobStateBadge state={job.status} />
                        </TableCell>
                        <TableCell className="text-right tabular-nums">
                          {job.attempt_count}/{job.max_attempts}
                        </TableCell>
                        <TableCell className="text-right text-muted-foreground">
                          {fmtRelative(job.created_at)}
                        </TableCell>
                      </TableRow>
                    ))
                  )}
                </TableBody>
              </Table>
            </div>
          </TabsContent>
        );
      })}
    </Tabs>
  );
}
