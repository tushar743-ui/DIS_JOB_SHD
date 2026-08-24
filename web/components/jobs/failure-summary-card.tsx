"use client";

import { useState } from "react";
import { Sparkles, RefreshCw, AlertTriangle } from "lucide-react";
import { useFeatures, useFailureSummary } from "@/hooks/use-data";
import { failureSummary as failureSummaryApi } from "@/lib/api";
import { reportError } from "@/lib/errors";
import { toast } from "@/components/ui/toast";
import { Card } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { fmtRelative } from "@/lib/status";
import { cn } from "@/lib/utils";

const CATEGORY_LABEL: Record<string, string> = {
  timeout: "Timeout",
  dependency_failure: "Dependency Failure",
  invalid_payload: "Invalid Payload",
  permission: "Permission",
  rate_limit: "Rate Limit",
  resource_exhaustion: "Resource Exhaustion",
  logic_error: "Logic Error",
  infrastructure: "Infrastructure",
  unknown: "Unknown",
};

const CONFIDENCE_CLASS: Record<string, string> = {
  high: "bg-emerald-500/15 text-emerald-400 border-emerald-500/20",
  medium: "bg-amber-500/15 text-amber-400 border-amber-500/20",
  low: "bg-zinc-500/15 text-zinc-400 border-zinc-500/20",
};

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div>
      <p className="t-label text-muted-foreground">{label}</p>
      <p className="mt-1 text-sm leading-relaxed">{children}</p>
    </div>
  );
}

export function FailureSummaryCard({ jobId, jobStatus }: { jobId: string; jobStatus: string }) {
  const { data: features } = useFeatures();
  const { data: summary, mutate } = useFailureSummary(jobId);
  const [generating, setGenerating] = useState(false);

  const eligible = jobStatus === "failed" || jobStatus === "dead";
  if (!features?.ai_failure_summaries || !eligible) return null;

  async function generate() {
    setGenerating(true);
    const previousUpdatedAt = summary?.updated_at;
    try {
      const result = await failureSummaryApi.generate(jobId);
      mutate(result, false);
      if (previousUpdatedAt && result.updated_at === previousUpdatedAt) {
        toast.add({
          title: "Already up to date",
          description: "This job hasn't failed again with new evidence, so the cached diagnosis was returned instead of paying for a new one.",
          type: "info",
        });
      }
    } catch (e) {
      reportError(e, "Failed to generate AI summary");
    } finally {
      setGenerating(false);
    }
  }

  return (
    <div className="mt-8 border-t border-border pt-6">
    <Card className="rounded-xl p-6">
      <div className="flex items-center justify-between gap-3">
        <h2 className="flex items-center gap-1.5 text-sm font-semibold tracking-tight">
          <Sparkles className="size-3.5 text-primary" aria-hidden="true" />
          AI Failure Summary
        </h2>
        {summary && (
          <Button
            size="sm" variant="outline" className="rounded-lg"
            disabled={generating} onClick={generate}
            aria-label={summary.stale ? "Regenerate summary" : "Regenerate summary with latest evidence"}
          >
            <RefreshCw className={cn("size-3.5", generating && "animate-spin")} aria-hidden="true" />
            {generating ? "Regenerating…" : "Regenerate"}
          </Button>
        )}
      </div>

      {!summary ? (
        <div className="mt-4 flex flex-col items-start gap-3">
          <p className="text-sm text-muted-foreground">
            Diagnose this failure with an AI-generated summary of the terminal error, execution history, and logs.
          </p>
          <Button size="sm" className="rounded-lg" disabled={generating} onClick={generate}>
            <Sparkles className={cn("size-3.5", generating && "animate-pulse")} aria-hidden="true" />
            {generating ? "Generating…" : "Generate summary"}
          </Button>
        </div>
      ) : (
        <div className="mt-4 space-y-4">
          {summary.stale && (
            <p className="flex items-center gap-1.5 text-xs font-medium text-state-scheduled">
              <AlertTriangle className="size-3.5" aria-hidden="true" />
              This job has failed again since this summary was generated - it may be out of date.
            </p>
          )}

          <div className="flex flex-wrap items-center gap-2">
            <Badge variant="outline" className="rounded-full px-2.5 py-1">
              {CATEGORY_LABEL[summary.category] ?? summary.category}
            </Badge>
            <Badge
              variant="outline"
              className={cn("rounded-full px-2.5 py-1", CONFIDENCE_CLASS[summary.confidence])}
            >
              {summary.confidence} confidence
            </Badge>
            {summary.is_transient && (
              <Badge variant="outline" className="rounded-full border-state-retrying/30 px-2.5 py-1 text-state-retrying">
                Likely transient
              </Badge>
            )}
          </div>

          <Field label="Summary">{summary.summary}</Field>
          <Field label="Likely cause">{summary.likely_cause}</Field>
          <Field label="Suggested action">{summary.suggested_action}</Field>

          <p className="t-meta text-muted-foreground">
            {summary.model} · {summary.input_tokens + summary.output_tokens} tokens · generated {fmtRelative(summary.created_at)}
          </p>
        </div>
      )}
    </Card>
    </div>
  );
}
