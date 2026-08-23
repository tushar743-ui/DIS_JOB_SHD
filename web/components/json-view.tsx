"use client";

import { useState } from "react";
import { Check, ChevronDown, ChevronRight, Copy } from "lucide-react";
import { cn } from "@/lib/utils";

function Leaf({ value }: { value: unknown }) {
  if (typeof value === "string") return <span className="text-state-completed">&quot;{value}&quot;</span>;
  if (typeof value === "number") return <span className="text-state-running">{value}</span>;
  if (typeof value === "boolean") return <span className="text-state-retrying">{String(value)}</span>;
  if (value === null) return <span className="text-muted-foreground">null</span>;
  return <span>{String(value)}</span>;
}

function Node({ name, value, depth }: { name?: string; value: unknown; depth: number }) {
  const [open, setOpen] = useState(depth < 2);
  const isObj = value !== null && typeof value === "object";

  if (!isObj) {
    return (
      <div style={{ paddingLeft: depth * 14 }} className="whitespace-nowrap leading-5">
        {name !== undefined && <span className="text-muted-foreground">&quot;{name}&quot;: </span>}
        <Leaf value={value} />
      </div>
    );
  }

  const entries = Array.isArray(value)
    ? value.map((v, i) => [String(i), v] as const)
    : Object.entries(value as Record<string, unknown>);
  const summary = Array.isArray(value)
    ? `[… ${entries.length} item${entries.length === 1 ? "" : "s"} ]`
    : `{… ${entries.length} key${entries.length === 1 ? "" : "s"} }`;

  return (
    <div style={{ paddingLeft: depth * 14 }} className="whitespace-nowrap leading-5">
      <button
        onClick={() => setOpen(!open)}
        aria-label={open ? `Collapse ${name ?? "root"}` : `Expand ${name ?? "root"}`}
        aria-expanded={open}
        className="inline-flex items-center gap-1 rounded hover:text-foreground focus-visible:ring-2 focus-visible:ring-primary"
      >
        {open ? (
          <ChevronDown className="size-3 text-muted-foreground" />
        ) : (
          <ChevronRight className="size-3 text-muted-foreground" />
        )}
        {name !== undefined && <span className="text-muted-foreground">&quot;{name}&quot;:</span>}
        {!open && <span className="italic text-muted-foreground">{summary}</span>}
        {open && <span className="text-muted-foreground">{Array.isArray(value) ? "[" : "{"}</span>}
      </button>
      {open && (
        <>
          {entries.map(([k, v]) => (
            <Node key={k} name={k} value={v} depth={depth + 1} />
          ))}
          <div style={{ paddingLeft: depth * 14 }} className="text-muted-foreground">
            {Array.isArray(value) ? "]" : "}"}
          </div>
        </>
      )}
    </div>
  );
}

export function JsonView({
  title,
  value,
  className,
}: {
  title: string;
  value: unknown;
  className?: string;
}) {
  const [copied, setCopied] = useState(false);
  const raw = (() => {
    try { return JSON.stringify(value, null, 2); } catch { return String(value); }
  })();

  async function copy() {
    await navigator.clipboard.writeText(raw);
    setCopied(true);
    setTimeout(() => setCopied(false), 1500);
  }

  return (
    <section className={cn("min-w-0", className)}>
      <h2 className="mb-2 text-sm font-semibold tracking-tight">{title}</h2>
      <div className="overflow-hidden rounded-lg border border-border">
        <div className="flex h-8 items-center justify-end bg-muted/50 px-2">
          <button
            onClick={copy}
            aria-label={`Copy ${title} as JSON`}
            className="grid size-6 place-items-center rounded text-muted-foreground transition-colors hover:bg-accent hover:text-foreground focus-visible:ring-2 focus-visible:ring-primary"
          >
            {copied ? <Check className="size-3.5 text-state-completed" /> : <Copy className="size-3.5" />}
          </button>
        </div>
        <div className="max-h-72 overflow-auto p-3 font-mono text-xs scrollbar-thin">
          {value == null ? (
            <span className="text-muted-foreground">–</span>
          ) : (
            <Node value={value} depth={0} />
          )}
        </div>
      </div>
    </section>
  );
}
