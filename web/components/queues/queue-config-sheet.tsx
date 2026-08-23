"use client";

import { useState } from "react";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { z } from "zod";
import { queues } from "@/lib/api";
import { reportError } from "@/lib/errors";
import {
  Sheet, SheetContent, SheetDescription, SheetFooter, SheetHeader, SheetTitle, SheetTrigger,
} from "@/components/ui/sheet";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Button } from "@/components/ui/button";
import { Switch } from "@/components/ui/switch";
import { Separator } from "@/components/ui/separator";
import { ShimmerButton } from "@/components/ui/shimmer-button";
import type { Queue } from "@/lib/api";

const schema = z.object({
  name: z.string().min(1, "Queue name is required").max(64, "Keep it under 64 characters"),
  description: z.string().max(200, "Keep it under 200 characters").optional(),
  concurrency_limit: z.coerce.number().int().min(1, "Must be at least 1").max(1000, "Must be 1000 or less"),
  priority: z.coerce.number().int().min(1, "Must be at least 1").max(10, "Must be 10 or less"),
  rate_limit_enabled: z.boolean(),
  rate_limit_per_minute: z.coerce.number().int().min(1).max(100000).optional(),
});

type FormValues = z.input<typeof schema>;

export function QueueConfigSheet({
  queue, projectId, trigger, onSaved,
}: {
  queue: Queue | null;
  projectId: string | null;
  trigger: React.ReactElement;
  onSaved: () => void;
}) {
  const [open, onOpenChange] = useState(false);

  const editing = Boolean(queue);

  const {
    register, handleSubmit, watch, reset,
    formState: { errors, isSubmitting, isDirty },
  } = useForm<FormValues>({
    resolver: zodResolver(schema),
    values: {
      name: queue?.name ?? "",
      description: queue?.description ?? "",
      concurrency_limit: queue?.concurrency_limit ?? 10,
      priority: queue?.priority ?? 5,
      rate_limit_enabled: false,
      rate_limit_per_minute: 600,
    },
  });

  const rateOn = watch("rate_limit_enabled");

  async function onSubmit(values: FormValues) {
    const parsed = schema.parse(values);
    try {
      if (editing && queue) {
        await queues.update(queue.id, {
          name: parsed.name,
          description: parsed.description,
          concurrency_limit: parsed.concurrency_limit,
          priority: parsed.priority,
        });
      } else if (projectId) {
        await queues.create(projectId, {
          name: parsed.name,
          description: parsed.description,
          concurrency_limit: parsed.concurrency_limit,
          priority: parsed.priority,
        });
      }
      reset(values);
      onSaved();
      onOpenChange(false);
    } catch (e) {
      reportError(e, editing ? "Failed to update queue" : "Failed to create queue");
    }
  }

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetTrigger render={trigger} />
      <SheetContent side="right" className="w-full shadow-2xl sm:max-w-[480px]">
        <SheetHeader>
          <SheetTitle className="flex items-center gap-2 tracking-tight">
            {editing ? "Configure queue" : "Create queue"}
            {isDirty && (
              <span className="size-2 rounded-full bg-state-retrying" aria-label="Unsaved changes" />
            )}
          </SheetTitle>
          <SheetDescription>
            {editing ? "Update throughput and retry behaviour for this queue." : "Queues group jobs and control concurrency."}
          </SheetDescription>
        </SheetHeader>

        <form onSubmit={handleSubmit(onSubmit)} className="flex min-h-0 flex-1 flex-col">
          <div className="flex-1 space-y-4 overflow-y-auto px-4 scrollbar-thin">
            <div className="space-y-1.5">
              <Label htmlFor="q-name">Queue name</Label>
              <Input id="q-name" {...register("name")} disabled={editing} className="rounded-md" aria-invalid={Boolean(errors.name)} />
              {errors.name && <p className="text-xs text-destructive">{errors.name.message}</p>}
            </div>

            <div className="space-y-1.5">
              <Label htmlFor="q-desc">Description</Label>
              <Input id="q-desc" {...register("description")} className="rounded-md" aria-invalid={Boolean(errors.description)} />
              {errors.description && <p className="text-xs text-destructive">{errors.description.message}</p>}
            </div>

            <div className="grid grid-cols-2 gap-4">
              <div className="space-y-1.5">
                <Label htmlFor="q-conc">Max concurrency</Label>
                <Input id="q-conc" type="number" min={1} max={1000} {...register("concurrency_limit")} className="rounded-md" aria-invalid={Boolean(errors.concurrency_limit)} />
                {errors.concurrency_limit && <p className="text-xs text-destructive">{errors.concurrency_limit.message}</p>}
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="q-prio">Priority (1–10)</Label>
                <Input id="q-prio" type="number" min={1} max={10} {...register("priority")} className="rounded-md" aria-invalid={Boolean(errors.priority)} />
                {errors.priority && <p className="text-xs text-destructive">{errors.priority.message}</p>}
              </div>
            </div>

            <Separator />

            <div>
              <p className="mb-3 text-xs font-medium uppercase tracking-wide text-muted-foreground">Rate limiting</p>
              <div className="flex items-center justify-between">
                <Label htmlFor="q-rate">Enabled</Label>
                <Switch id="q-rate" {...register("rate_limit_enabled")} />
              </div>
              {rateOn && (
                <div className="mt-3 space-y-1.5">
                  <Label htmlFor="q-rpm">Max per minute</Label>
                  <Input id="q-rpm" type="number" min={1} {...register("rate_limit_per_minute")} className="rounded-md" />
                </div>
              )}
            </div>
          </div>

          <SheetFooter className="flex-row justify-end gap-2">
            <Button type="button" variant="outline" className="rounded-lg" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <ShimmerButton type="submit" disabled={isSubmitting} className="h-9 px-4 text-sm">
              {isSubmitting ? "Saving…" : "Save Changes"}
            </ShimmerButton>
          </SheetFooter>
        </form>
      </SheetContent>
    </Sheet>
  );
}
