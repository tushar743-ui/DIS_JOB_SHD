"use client";

import { useEffect, useState, useCallback } from "react";
import Link from "next/link";
import { queues, type Queue, type CreateQueueInput } from "@/lib/api";
import { useAuthStore } from "@/lib/auth-store";
import { StatusBadge } from "@/components/status-badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import {
  Dialog, DialogContent, DialogHeader, DialogTitle, DialogTrigger,
} from "@/components/ui/dialog";
import { Plus, PauseCircle, PlayCircle, Trash } from "@phosphor-icons/react";
import { fmtDate } from "@/lib/status";

export default function QueuesPage() {
  const projectId = useAuthStore((s) => s.projectId);
  const [list, setList] = useState<Queue[]>([]);
  const [loading, setLoading] = useState(true);
  const [dialogOpen, setDialogOpen] = useState(false);

  const load = useCallback(async () => {
    if (!projectId) return;
    try {
      const data = await queues.list(projectId);
      setList(data);
    } finally {
      setLoading(false);
    }
  }, [projectId]);

  useEffect(() => { load(); }, [load]);

  async function togglePause(q: Queue) {
    if (q.paused) {
      await queues.resume(q.id);
    } else {
      await queues.pause(q.id);
    }
    load();
  }

  async function remove(id: string) {
    if (!confirm("Delete this queue and all its jobs? This cannot be undone.")) return;
    await queues.delete(id);
    load();
  }

  return (
    <div className="p-6 max-w-5xl mx-auto space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-lg font-semibold">Queues</h1>
        {projectId && (
          <Dialog open={dialogOpen} onOpenChange={setDialogOpen}>
            <DialogTrigger asChild>
              <Button size="sm" className="bg-amber-500 hover:bg-amber-400 text-black gap-1.5">
                <Plus size={14} weight="bold" /> New queue
              </Button>
            </DialogTrigger>
            <DialogContent>
              <DialogHeader>
                <DialogTitle>Create queue</DialogTitle>
              </DialogHeader>
              <CreateQueueForm
                projectId={projectId}
                onCreated={() => { setDialogOpen(false); load(); }}
              />
            </DialogContent>
          </Dialog>
        )}
      </div>

      {loading ? (
        <div className="space-y-2">
          {[...Array(4)].map((_, i) => <Skeleton key={i} className="h-16 rounded-lg" />)}
        </div>
      ) : (
        <div className="border border-border rounded-lg divide-y divide-border overflow-hidden">
          {list.length === 0 && (
            <div className="px-4 py-12 text-center text-sm text-muted-foreground">
              No queues. Create your first queue to start scheduling jobs.
            </div>
          )}
          {list.map((q) => (
            <div key={q.id} className="px-4 py-3.5 flex items-center gap-4 hover:bg-accent/30 transition-colors">
              <div className="flex-1 min-w-0">
                <div className="flex items-center gap-2">
                  <Link
                    href={`/dashboard/queues/${q.id}`}
                    className="text-sm font-medium hover:text-amber-400 transition-colors truncate"
                  >
                    {q.name}
                  </Link>
                  {q.paused && <StatusBadge status="cancelled" />}
                </div>
                <p className="text-xs text-muted-foreground mt-0.5 font-mono">
                  {q.id.slice(0, 8)} · priority {q.priority} · concurrency {q.concurrency_limit} · {fmtDate(q.created_at)}
                </p>
              </div>
              <div className="flex items-center gap-1">
                <button
                  onClick={() => togglePause(q)}
                  className="p-1.5 rounded-md text-muted-foreground hover:text-foreground hover:bg-accent transition-colors"
                  title={q.paused ? "Resume" : "Pause"}
                >
                  {q.paused ? <PlayCircle size={15} /> : <PauseCircle size={15} />}
                </button>
                <button
                  onClick={() => remove(q.id)}
                  className="p-1.5 rounded-md text-muted-foreground hover:text-destructive hover:bg-destructive/10 transition-colors"
                  title="Delete queue"
                >
                  <Trash size={15} />
                </button>
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

function CreateQueueForm({ projectId, onCreated }: { projectId: string; onCreated: () => void }) {
  const [form, setForm] = useState<CreateQueueInput>({
    name: "", priority: 5, concurrency_limit: 10,
  });
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setLoading(true);
    setError("");
    try {
      await queues.create(projectId, form);
      onCreated();
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : "Failed");
    } finally {
      setLoading(false);
    }
  }

  return (
    <form onSubmit={submit} className="space-y-4 pt-2">
      <div className="space-y-1.5">
        <Label>Name</Label>
        <Input value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} placeholder="email-notifications" required />
      </div>
      <div className="grid grid-cols-2 gap-3">
        <div className="space-y-1.5">
          <Label>Priority (1-10)</Label>
          <Input type="number" min={1} max={10} value={form.priority}
            onChange={(e) => setForm({ ...form, priority: +e.target.value })} />
        </div>
        <div className="space-y-1.5">
          <Label>Concurrency</Label>
          <Input type="number" min={1} value={form.concurrency_limit}
            onChange={(e) => setForm({ ...form, concurrency_limit: +e.target.value })} />
        </div>
      </div>
      {error && <p className="text-sm text-destructive">{error}</p>}
      <Button type="submit" className="w-full bg-amber-500 hover:bg-amber-400 text-black" disabled={loading}>
        {loading ? "Creating..." : "Create queue"}
      </Button>
    </form>
  );
}
