"use client";

import { useMemo, useRef, useState } from "react";
import { useRouter } from "next/navigation";
import {
  flexRender, getCoreRowModel, useReactTable, type ColumnDef, type RowSelectionState,
} from "@tanstack/react-table";
import { Inbox, MoreVertical, RefreshCw, X, Plus } from "lucide-react";
import { useAuthStore } from "@/lib/auth-store";
import { useAllJobs, useQueues, type JobRow } from "@/hooks/use-data";
import { jobs as jobsApi } from "@/lib/api";
import { JobStateBadge, StateDot } from "@/components/job-state-badge";
import { formatElapsed, useElapsedTime } from "@/hooks/use-elapsed-time";
import { EmptyState, ErrorState, TableSkeleton } from "@/components/states";
import { NoActiveWorkerBanner } from "@/components/no-worker-banner";
import { reportError } from "@/lib/errors";
import { canCancel, canRetry, applyJobAction, type JobAction } from "@/lib/job-actions";
import { CreateJobDialog } from "@/components/jobs/create-job-dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Badge } from "@/components/ui/badge";
import { Checkbox } from "@/components/ui/checkbox";
import {
  DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import {
  Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
} from "@/components/ui/select";
import { fmtDate, fmtRelative } from "@/lib/status";
import { cn } from "@/lib/utils";

const STATES = ["all", "queued", "scheduled", "claimed", "running", "completed", "failed", "cancelled", "dead"];
const PAGE_SIZES = [25, 50, 100];

function argsText(payload: unknown): string {
  if (payload == null) return "-";
  try {
    return typeof payload === "string" ? payload : JSON.stringify(payload);
  } catch {
    return "-";
  }
}

function LiveDuration({ startedAt }: { startedAt?: string | null }) {
  return <>{useElapsedTime(startedAt, null)}</>;
}

function StaticDuration({ startedAt, stoppedAt }: { startedAt?: string | null; stoppedAt?: string | null }) {
  if (!startedAt) return <>-</>;
  const start = new Date(startedAt).getTime();
  if (Number.isNaN(start)) return <>-</>;
  const end = stoppedAt ? new Date(stoppedAt).getTime() : start;
  return <>{formatElapsed(Math.floor((end - start) / 1000))}</>;
}

function DurationCell({ job }: { job: JobRow }) {
  const live = job.status === "running" || job.status === "claimed";
  return (
    <span className="font-mono text-xs tabular-nums">
      {live ? (
        <LiveDuration startedAt={job.updated_at} />
      ) : (
        <StaticDuration startedAt={job.created_at} stoppedAt={job.completed_at} />
      )}
    </span>
  );
}

export default function JobExplorerPage() {
  const router = useRouter();
  const projectId = useAuthStore((s) => s.projectId);
  const { data: queueList } = useQueues(projectId);

  const [state, setState] = useState("all");
  const [queueId, setQueueId] = useState("all");
  const [search, setSearch] = useState("");
  const [pageSize, setPageSize] = useState(25);
  const [cursor, setCursor] = useState(0);
  const [selection, setSelection] = useState<RowSelectionState>({});

  const { data, error, isLoading, mutate } = useAllJobs(
    projectId,
    state === "all" ? undefined : state
  );

  const filtered = useMemo(() => {
    let rows = data ?? [];
    if (queueId !== "all") rows = rows.filter((j) => j.queue_id === queueId);
    if (search.trim()) {
      const q = search.toLowerCase();
      rows = rows.filter(
        (j) => j.type.toLowerCase().includes(q) || j.id.includes(q) || j.tags?.some((t) => t.includes(q))
      );
    }
    return rows;
  }, [data, queueId, search]);

  const page = useMemo(() => filtered.slice(cursor, cursor + pageSize), [filtered, cursor, pageSize]);

  const runActionRef = useRef<(action: JobAction, targets: JobRow[]) => void>(() => {});

  const columns = useMemo<ColumnDef<JobRow>[]>(
    () => [
      {
        id: "select",
        header: ({ table }) => (
          <Checkbox
            aria-label="Select all rows on this page"
            checked={table.getIsAllPageRowsSelected()}
            onCheckedChange={(v) => table.toggleAllPageRowsSelected(Boolean(v))}
          />
        ),
        cell: ({ row }) => (
          <div onClick={(e) => e.stopPropagation()} className="flex items-center">
            <Checkbox
              aria-label={`Select job ${row.original.type}`}
              checked={row.getIsSelected()}
              onCheckedChange={(v) => row.toggleSelected(Boolean(v))}
            />
          </div>
        ),
        size: 32,
      },
      {
        id: "dot",
        header: "",
        cell: ({ row }) => <StateDot state={row.original.status} />,
        size: 24,
      },
      {
        accessorKey: "type",
        header: "Kind",
        cell: ({ row }) => <span className="font-medium">{row.original.type}</span>,
      },
      {
        id: "args",
        header: "Args",
        cell: ({ row }) => {
          const full = argsText(row.original.payload);
          return (
            <span title={full} className="block max-w-[220px] truncate font-mono text-xs text-muted-foreground">
              {full.length > 40 ? full.slice(0, 40) + "…" : full}
            </span>
          );
        },
      },
      {
        accessorKey: "queue_name",
        header: "Queue",
        cell: ({ row }) => (
          <Badge variant="outline" className="rounded-full text-[10px]">{row.original.queue_name}</Badge>
        ),
      },
      {
        accessorKey: "status",
        header: "State",
        cell: ({ row }) => <JobStateBadge state={row.original.status} />,
      },
      {
        id: "attempt",
        header: "Attempt",
        cell: ({ row }) => (
          <span
            className={cn(
              "font-mono text-xs tabular-nums",
              row.original.attempt_count > 1 && "text-state-retrying"
            )}
          >
            {row.original.attempt_count}/{row.original.max_attempts}
          </span>
        ),
      },
      {
        id: "duration",
        header: "Duration",
        cell: ({ row }) => <DurationCell job={row.original} />,
      },
      {
        accessorKey: "created_at",
        header: "Created",
        cell: ({ row }) => (
          <span title={fmtDate(row.original.created_at)} className="text-xs text-muted-foreground">
            {fmtRelative(row.original.created_at)}
          </span>
        ),
      },
      {
        id: "actions",
        header: "",
        cell: ({ row }) => (
          <DropdownMenu>
            <DropdownMenuTrigger
              render={
                <button
                  aria-label={`Actions for ${row.original.type}`}
                  onClick={(e) => e.stopPropagation()}
                  className="grid size-7 place-items-center rounded-md text-muted-foreground hover:bg-accent hover:text-foreground focus-visible:ring-2 focus-visible:ring-primary"
                >
                  <MoreVertical className="size-3.5" />
                </button>
              }
            />
            <DropdownMenuContent align="end">
              <DropdownMenuItem onClick={() => router.push(`/jobs/${row.original.id}`)}>View details</DropdownMenuItem>
              <DropdownMenuItem
                disabled={!canRetry(row.original.status)}
                onClick={() => runActionRef.current("retry", [row.original])}
              >
                Retry
              </DropdownMenuItem>
              <DropdownMenuItem
                disabled={!canCancel(row.original.status)}
                onClick={() => runActionRef.current("cancel", [row.original])}
              >
                Cancel
              </DropdownMenuItem>
              <DropdownMenuItem
                onClick={() => runActionRef.current("delete", [row.original])}
              >
                Delete
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        ),
        size: 40,
      },
    ],
    [router]
  );

  const table = useReactTable({
    data: page,
    columns,
    state: { rowSelection: selection },
    onRowSelectionChange: setSelection,
    getCoreRowModel: getCoreRowModel(),
    getRowId: (r) => r.id,
  });

  const selectedIds = Object.keys(selection).filter((k) => selection[k]);

  const selectedJobs = (data ?? []).filter((j) => selection[j.id]);
  const retryable = selectedJobs.filter((j) => canRetry(j.status));
  const cancellable = selectedJobs.filter((j) => canCancel(j.status));

  async function runAction(action: JobAction, targets: JobRow[]) {
    if (!targets.length) {
      reportError(new Error(`No selected job is eligible to ${action}`), `Nothing to ${action}`);
      return;
    }
    const ids = new Set(targets.map((j) => j.id));
    const optimistic = applyJobAction<JobRow>(action, ids);
    const fn =
      action === "retry" ? jobsApi.retry : action === "cancel" ? jobsApi.cancel : jobsApi.remove;
    try {
      await mutate(
        async (current?: JobRow[]) => {
          const results = await Promise.allSettled(targets.map((j) => fn(j.id)));
          const failures = results.filter((r) => r.status === "rejected").length;
          if (failures) throw new Error(`${failures} of ${targets.length} failed`);
          return optimistic(current);
        },
        { optimisticData: optimistic, rollbackOnError: true, revalidate: true }
      );
    } catch (e) {
      reportError(e, targets.length > 1 ? `Bulk ${action} incomplete` : `Failed to ${action} job`);
    }
  }

  runActionRef.current = runAction;

  async function bulk(action: JobAction) {
    const targets =
      action === "retry" ? retryable : action === "cancel" ? cancellable : selectedJobs;
    setSelection({});
    await runAction(action, targets);
  }

  if (error) return <ErrorState message={error.message} onRetry={() => mutate()} />;

  return (
    <div className="space-y-4">
      <NoActiveWorkerBanner />

      <div className="flex flex-wrap items-center gap-2">
        <Select value={state} onValueChange={(v) => { if (v) { setState(v); setCursor(0); } }}>
          <SelectTrigger className="w-36 rounded-lg" aria-label="Filter by state">
            <SelectValue placeholder="State">
              {(v: string) => (v === "all" ? "All states" : v)}
            </SelectValue>
          </SelectTrigger>
          <SelectContent>
            {STATES.map((s) => <SelectItem key={s} value={s}>{s === "all" ? "All states" : s}</SelectItem>)}
          </SelectContent>
        </Select>

        <Select value={queueId} onValueChange={(v) => { if (v) { setQueueId(v); setCursor(0); } }}>
          <SelectTrigger className="w-44 rounded-lg" aria-label="Filter by queue">
            <SelectValue placeholder="Queue">
              {(v: string) => (v === "all" ? "All queues" : queueList?.find((q) => q.id === v)?.name ?? "Queue")}
            </SelectValue>
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">All queues</SelectItem>
            {(queueList ?? []).map((q) => <SelectItem key={q.id} value={q.id}>{q.name}</SelectItem>)}
          </SelectContent>
        </Select>

        <Input
          value={search}
          onChange={(e) => { setSearch(e.target.value); setCursor(0); }}
          placeholder="Search by kind, ID, tag…"
          aria-label="Search jobs"
          className="w-60 rounded-md font-mono text-xs"
        />

        {selectedIds.length > 0 && (
          <DropdownMenu>
            <DropdownMenuTrigger
              render={
                <Button size="sm" variant="outline" className="rounded-lg">
                  Bulk actions ({selectedIds.length})
                </Button>
              }
            />
            <DropdownMenuContent align="start">
              <DropdownMenuItem disabled={!retryable.length} onClick={() => bulk("retry")}>
                Retry selected ({retryable.length})
              </DropdownMenuItem>
              <DropdownMenuItem disabled={!cancellable.length} onClick={() => bulk("cancel")}>
                Cancel selected ({cancellable.length})
              </DropdownMenuItem>
              <DropdownMenuItem onClick={() => bulk("delete")}>
                Delete selected ({selectedJobs.length})
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        )}

        <Button size="sm" variant="ghost" onClick={() => mutate()} aria-label="Refresh" className="ml-auto rounded-lg">
          <RefreshCw className="size-3.5" aria-hidden="true" />
        </Button>
        <CreateJobDialog
          queues={queueList ?? []}
          onCreated={() => mutate()}
          trigger={
            <Button size="sm" className="rounded-lg" disabled={!queueList?.length} aria-label="Enqueue a new job">
              <Plus className="size-3.5" aria-hidden="true" /> New Job
            </Button>
          }
        />
      </div>

      {(state !== "all" || queueId !== "all" || search) && (
        <div className="flex flex-wrap items-center gap-1.5">
          {state !== "all" && (
            <Badge variant="outline" className="gap-1 rounded-full text-[10px]">
              state: {state}
              <button onClick={() => setState("all")} aria-label="Clear state filter"><X className="size-2.5" /></button>
            </Badge>
          )}
          {queueId !== "all" && (
            <Badge variant="outline" className="gap-1 rounded-full text-[10px]">
              queue: {queueList?.find((q) => q.id === queueId)?.name ?? queueId}
              <button onClick={() => setQueueId("all")} aria-label="Clear queue filter"><X className="size-2.5" /></button>
            </Badge>
          )}
          {search && (
            <Badge variant="outline" className="gap-1 rounded-full text-[10px]">
              search: {search}
              <button onClick={() => setSearch("")} aria-label="Clear search"><X className="size-2.5" /></button>
            </Badge>
          )}
        </div>
      )}

      {isLoading && !data ? (
        <TableSkeleton rows={6} cols={8} />
      ) : page.length === 0 ? (
        <EmptyState
          icon={Inbox}
          title="No jobs found"
          description="Adjust your filters or enqueue a job to see it here."
          action={<Button size="sm" className="rounded-lg" onClick={() => { setState("all"); setQueueId("all"); setSearch(""); }}>Clear filters</Button>}
        />
      ) : (
        <div className="overflow-hidden rounded-xl border border-border">
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                {table.getHeaderGroups().map((hg) => (
                  <tr key={hg.id} className="border-b border-border bg-muted/50 text-[10px] uppercase tracking-wide text-muted-foreground">
                    {hg.headers.map((h) => (
                      <th key={h.id} className="h-9 px-3 text-left font-medium">
                        {h.isPlaceholder ? null : flexRender(h.column.columnDef.header, h.getContext())}
                      </th>
                    ))}
                  </tr>
                ))}
              </thead>
              <tbody>
                {table.getRowModel().rows.map((row) => (
                  <tr
                    key={row.id}
                    onClick={() => router.push(`/jobs/${row.original.id}`)}
                    onMouseEnter={() => router.prefetch(`/jobs/${row.original.id}`)}
                    className="h-12 cursor-pointer border-b border-border transition-colors last:border-0 hover:bg-accent/40"
                  >
                    {row.getVisibleCells().map((cell) => (
                      <td key={cell.id} className="px-3">
                        {flexRender(cell.column.columnDef.cell, cell.getContext())}
                      </td>
                    ))}
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      )}

      <div className="flex flex-wrap items-center gap-3 text-xs text-muted-foreground">
        <span>
          Showing {filtered.length ? cursor + 1 : 0}–{Math.min(cursor + pageSize, filtered.length)} of{" "}
          {filtered.length.toLocaleString()} jobs
        </span>
        <Select value={String(pageSize)} onValueChange={(v) => { if (v) { setPageSize(Number(v)); setCursor(0); } }}>
          <SelectTrigger className="h-7 w-20 rounded-lg text-xs" aria-label="Rows per page">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {PAGE_SIZES.map((n) => <SelectItem key={n} value={String(n)}>{n}</SelectItem>)}
          </SelectContent>
        </Select>
        <div className="ml-auto flex gap-2">
          <Button
            size="sm" variant="outline" className="rounded-lg"
            disabled={cursor === 0}
            onClick={() => setCursor(Math.max(0, cursor - pageSize))}
          >
            Previous
          </Button>
          <Button
            size="sm" variant="outline" className="rounded-lg"
            disabled={cursor + pageSize >= filtered.length}
            onClick={() => setCursor(cursor + pageSize)}
          >
            Next
          </Button>
        </div>
      </div>

    </div>
  );
}
