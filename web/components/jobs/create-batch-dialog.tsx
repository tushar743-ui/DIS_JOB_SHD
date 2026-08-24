"use client";

import { useState } from "react";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { z } from "zod";
import { jobs as jobsApi, type Queue } from "@/lib/api";
import { reportError } from "@/lib/errors";
import { useHandledJobTypes } from "@/hooks/use-data";
import {
  Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription, DialogFooter, DialogTrigger,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";

const schema = z.object({
  queue_id: z.string().min(1, "Pick a queue"),
  type: z.string().min(1, "Job type is required").max(80, "Keep it under 80 characters"),
  count: z.coerce.number().int().min(2, "Batches need at least 2 jobs").max(1000, "Maximum 1000 jobs per batch"),
  payload: z
    .string()
    .refine((v) => {
      if (!v.trim()) return true;
      try { JSON.parse(v); return true; } catch { return false; }
    }, "Payload must be valid JSON"),
  priority: z.coerce.number().int().min(1, "Must be at least 1").max(10, "Must be 10 or less"),
  max_attempts: z.coerce.number().int().min(1, "Must be at least 1").max(25, "Must be 25 or less"),
});

type Values = z.input<typeof schema>;

const DEFAULTS: Values = {
  queue_id: "", type: "", count: 10, payload: "{}", priority: 5, max_attempts: 3,
};

export function CreateBatchDialog({
  queues, trigger, onCreated,
}: {
  queues: Queue[];
  trigger: React.ReactElement;
  onCreated: () => void;
}) {
  const [open, onOpenChange] = useState(false);
  const { data: handledTypes } = useHandledJobTypes(queues[0]?.project_id ?? null);

  const {
    register, handleSubmit, reset,
    formState: { errors, isSubmitting },
  } = useForm<Values>({
    resolver: zodResolver(schema),
    defaultValues: { ...DEFAULTS, queue_id: queues[0]?.id ?? "" },
  });

  async function onSubmit(values: Values) {
    const parsed = schema.parse(values);
    const payload = parsed.payload.trim() ? JSON.parse(parsed.payload) : {};
    try {
      const result = await jobsApi.createBatch(
        parsed.queue_id,
        Array.from({ length: parsed.count }, () => ({
          type: parsed.type,
          payload,
          priority: parsed.priority,
          max_attempts: parsed.max_attempts,
        }))
      );
      reset({ ...DEFAULTS, queue_id: parsed.queue_id });
      onCreated();
      onOpenChange(false);
      if (result.skipped > 0) {
        reportError(new Error(`${result.skipped} job(s) skipped as duplicates`), "Batch partially created");
      }
    } catch (e) {
      reportError(e, "Failed to create batch");
    }
  }

  return (
    <Dialog
      open={open}
      onOpenChange={(v) => {
        if (v) reset({ ...DEFAULTS, queue_id: queues[0]?.id ?? "" });
        onOpenChange(v);
      }}
    >
      <DialogTrigger render={trigger} />
      <DialogContent className="rounded-xl shadow-2xl sm:max-w-lg">
        <DialogHeader>
          <DialogTitle className="tracking-tight">Enqueue a batch</DialogTitle>
          <DialogDescription>Creates several jobs of the same type and payload at once.</DialogDescription>
        </DialogHeader>

        <form onSubmit={handleSubmit(onSubmit)} className="space-y-4">
          <div className="grid grid-cols-2 gap-4">
            <div className="space-y-1.5">
              <Label htmlFor="b-queue">Queue</Label>
              <select
                id="b-queue"
                aria-label="Queue"
                className="h-9 w-full rounded-md border border-input bg-background px-3 text-sm focus-visible:ring-2 focus-visible:ring-primary"
                {...register("queue_id")}
              >
                {queues.map((q) => <option key={q.id} value={q.id}>{q.name}</option>)}
              </select>
              {errors.queue_id && <p className="text-xs text-destructive">{errors.queue_id.message}</p>}
            </div>

            <div className="space-y-1.5">
              <Label htmlFor="b-type">Job type</Label>
              <Input
                id="b-type"
                list="b-type-options"
                placeholder="send_email"
                className="rounded-md"
                aria-invalid={Boolean(errors.type)}
                {...register("type")}
              />
              <datalist id="b-type-options">
                {(handledTypes ?? []).map((t) => <option key={t} value={t} />)}
              </datalist>
              {errors.type && <p className="text-xs text-destructive">{errors.type.message}</p>}
            </div>
          </div>

          <div className="space-y-1.5">
            <Label htmlFor="b-payload">Payload (JSON, applied to every job)</Label>
            <Textarea id="b-payload" rows={4} className="rounded-md font-mono text-xs" aria-invalid={Boolean(errors.payload)} {...register("payload")} />
            {errors.payload && <p className="text-xs text-destructive">{errors.payload.message}</p>}
          </div>

          <div className="grid grid-cols-3 gap-4">
            <div className="space-y-1.5">
              <Label htmlFor="b-count">Job count</Label>
              <Input id="b-count" type="number" min={2} max={1000} className="rounded-md" aria-invalid={Boolean(errors.count)} {...register("count")} />
              {errors.count && <p className="text-xs text-destructive">{errors.count.message}</p>}
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="b-prio">Priority</Label>
              <Input id="b-prio" type="number" min={1} max={10} className="rounded-md" aria-invalid={Boolean(errors.priority)} {...register("priority")} />
              {errors.priority && <p className="text-xs text-destructive">{errors.priority.message}</p>}
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="b-att">Max attempts</Label>
              <Input id="b-att" type="number" min={1} max={25} className="rounded-md" aria-invalid={Boolean(errors.max_attempts)} {...register("max_attempts")} />
              {errors.max_attempts && <p className="text-xs text-destructive">{errors.max_attempts.message}</p>}
            </div>
          </div>

          <DialogFooter>
            <Button type="button" variant="outline" className="rounded-lg" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <Button type="submit" className="rounded-lg" disabled={isSubmitting}>
              {isSubmitting ? "Enqueuing…" : "Enqueue batch"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
