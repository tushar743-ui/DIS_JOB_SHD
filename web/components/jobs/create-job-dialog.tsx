"use client";

import { useState } from "react";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { z } from "zod";
import { jobs as jobsApi, type Queue } from "@/lib/api";
import { reportError } from "@/lib/errors";
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
  payload: z
    .string()
    .refine((v) => {
      if (!v.trim()) return true;
      try { JSON.parse(v); return true; } catch { return false; }
    }, "Payload must be valid JSON"),
  priority: z.coerce.number().int().min(1, "Must be at least 1").max(10, "Must be 10 or less"),
  max_attempts: z.coerce.number().int().min(1, "Must be at least 1").max(25, "Must be 25 or less"),
  scheduled_at: z.string().optional(),
});

type Values = z.input<typeof schema>;

export function CreateJobDialog({
  queues, trigger, onCreated,
}: {
  queues: Queue[];
  trigger: React.ReactElement;
  onCreated: () => void;
}) {
  const [open, onOpenChange] = useState(false);

  const {
    register, handleSubmit, reset,
    formState: { errors, isSubmitting },
  } = useForm<Values>({
    resolver: zodResolver(schema),
    defaultValues: {
      queue_id: queues[0]?.id ?? "",
      type: "",
      payload: "{}",
      priority: 5,
      max_attempts: 3,
      scheduled_at: "",
    },
  });

  async function onSubmit(values: Values) {
    const parsed = schema.parse(values);
    try {
      await jobsApi.create(parsed.queue_id, {
        type: parsed.type,
        payload: parsed.payload.trim() ? JSON.parse(parsed.payload) : {},
        priority: parsed.priority,
        max_attempts: parsed.max_attempts,
        scheduled_at: parsed.scheduled_at ? new Date(parsed.scheduled_at).toISOString() : undefined,
      });
      reset();
      onCreated();
      onOpenChange(false);
    } catch (e) {
      reportError(e, "Failed to create job");
    }
  }

  return (
    <Dialog
      open={open}
      onOpenChange={(v) => {
        if (v) {
          reset({
            queue_id: queues[0]?.id ?? "",
            type: "",
            payload: "{}",
            priority: 5,
            max_attempts: 3,
            scheduled_at: "",
          });
        }
        onOpenChange(v);
      }}
    >
      <DialogTrigger render={trigger} />
      <DialogContent className="rounded-xl shadow-2xl sm:max-w-lg">
        <DialogHeader>
          <DialogTitle className="tracking-tight">Enqueue a job</DialogTitle>
          <DialogDescription>The job runs as soon as a worker with capacity claims it.</DialogDescription>
        </DialogHeader>

        <form onSubmit={handleSubmit(onSubmit)} className="space-y-4">
          <div className="grid grid-cols-2 gap-4">
            <div className="space-y-1.5">
              <Label htmlFor="j-queue">Queue</Label>
              <select
                id="j-queue"
                aria-label="Queue"
                className="h-9 w-full rounded-md border border-input bg-background px-3 text-sm focus-visible:ring-2 focus-visible:ring-primary"
                {...register("queue_id")}
              >
                {queues.map((q) => <option key={q.id} value={q.id}>{q.name}</option>)}
              </select>
              {errors.queue_id && <p className="text-xs text-destructive">{errors.queue_id.message}</p>}
            </div>

            <div className="space-y-1.5">
              <Label htmlFor="j-type">Job type</Label>
              <Input id="j-type" placeholder="send_email" className="rounded-md" aria-invalid={Boolean(errors.type)} {...register("type")} />
              {errors.type && <p className="text-xs text-destructive">{errors.type.message}</p>}
            </div>
          </div>

          <div className="space-y-1.5">
            <Label htmlFor="j-payload">Payload (JSON)</Label>
            <Textarea id="j-payload" rows={4} className="rounded-md font-mono text-xs" aria-invalid={Boolean(errors.payload)} {...register("payload")} />
            {errors.payload && <p className="text-xs text-destructive">{errors.payload.message}</p>}
          </div>

          <div className="grid grid-cols-3 gap-4">
            <div className="space-y-1.5">
              <Label htmlFor="j-prio">Priority</Label>
              <Input id="j-prio" type="number" min={1} max={10} className="rounded-md" aria-invalid={Boolean(errors.priority)} {...register("priority")} />
              {errors.priority && <p className="text-xs text-destructive">{errors.priority.message}</p>}
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="j-att">Max attempts</Label>
              <Input id="j-att" type="number" min={1} max={25} className="rounded-md" aria-invalid={Boolean(errors.max_attempts)} {...register("max_attempts")} />
              {errors.max_attempts && <p className="text-xs text-destructive">{errors.max_attempts.message}</p>}
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="j-when">Run at (optional)</Label>
              <Input id="j-when" type="datetime-local" className="rounded-md" {...register("scheduled_at")} />
            </div>
          </div>

          <DialogFooter>
            <Button type="button" variant="outline" className="rounded-lg" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <Button type="submit" className="rounded-lg" disabled={isSubmitting}>
              {isSubmitting ? "Enqueuing…" : "Enqueue job"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
