"use client";

import { useEffect, useMemo } from "react";
import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { useTheme } from "next-themes";
import { Moon, Sun, Bell, ExternalLink, Search, ChevronRight } from "lucide-react";
import { cn } from "@/lib/utils";
import { useUIStore } from "@/lib/ui-store";
import { useAuthStore } from "@/lib/auth-store";
import { useBackendHealth, useQueues, useAllJobs } from "@/hooks/use-data";
import {
  Command, CommandDialog, CommandInput, CommandList, CommandEmpty, CommandGroup, CommandItem,
} from "@/components/ui/command";
import { Badge } from "@/components/ui/badge";
import {
  DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuSeparator, DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";

const TITLES: Record<string, string> = {
  "/dashboard": "Dashboard",
  "/analytics": "Analytics",
  "/jobs": "Job Explorer",
  "/jobs/scheduled": "Scheduled Jobs",
  "/jobs/batch": "Batch Jobs",
  "/dlq": "Dead Letter Queue",
  "/queues": "Queue Manager",
  "/workers": "Worker Monitor",
  "/settings": "Settings",
  "/docs": "API Docs",
};

function useCrumbs(pathname: string) {
  return useMemo(() => {
    const parts = pathname.split("/").filter(Boolean);
    return parts.map((part, i) => ({
      label: TITLES["/" + parts.slice(0, i + 1).join("/")] ?? part,
      href: "/" + parts.slice(0, i + 1).join("/"),
      last: i === parts.length - 1,
    }));
  }, [pathname]);
}

export function TopBar() {
  const pathname = usePathname();
  const router = useRouter();
  const { theme, setTheme } = useTheme();
  const { online } = useBackendHealth();
  const projectId = useAuthStore((s) => s.projectId);
  const user = useAuthStore((s) => s.user);
  const open = useUIStore((s) => s.commandOpen);
  const setOpen = useUIStore((s) => s.setCommandOpen);

  const { data: queueList } = useQueues(projectId);
  const { data: jobList } = useAllJobs(open ? projectId : null);
  const crumbs = useCrumbs(pathname);
  const title = TITLES[pathname] ?? crumbs.at(-1)?.label ?? "JobFlow";

  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      if (e.key === "k" && (e.metaKey || e.ctrlKey)) {
        e.preventDefault();
        setOpen(!open);
      }
    }
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [open, setOpen]);

  function go(href: string) {
    setOpen(false);
    router.push(href);
  }

  return (
    <header className="sticky top-0 z-20 flex h-14 items-center gap-4 border-b border-border bg-background/80 px-6 backdrop-blur">
      <div className="min-w-0">
        <h1 className="truncate text-sm font-semibold tracking-tight">{title}</h1>
        <nav aria-label="Breadcrumb" className="flex items-center gap-1 text-[11px] text-muted-foreground">
          {crumbs.map((c) => (
            <span key={c.href} className="flex items-center gap-1">
              {c.last ? (
                <span className="truncate">{c.label}</span>
              ) : (
                <Link href={c.href} className="truncate hover:text-foreground">{c.label}</Link>
              )}
              {!c.last && <ChevronRight className="size-3" aria-hidden="true" />}
            </span>
          ))}
        </nav>
      </div>

      <button
        onClick={() => setOpen(true)}
        aria-label="Open search (Command K)"
        className="mx-auto hidden w-full max-w-sm items-center gap-2 rounded-lg border border-border bg-card px-3 py-1.5 text-xs text-muted-foreground transition-colors hover:bg-accent focus-visible:ring-2 focus-visible:ring-primary md:flex"
      >
        <Search className="size-3.5" aria-hidden="true" />
        <span>Search jobs and queues…</span>
        <kbd className="ml-auto rounded border border-border px-1.5 py-0.5 font-mono text-[10px]">⌘K</kbd>
      </button>

      <div className="ml-auto flex items-center gap-1.5">
        <Badge
          variant="outline"
          role="status"
          aria-live="polite"
          className={cn(
            "gap-1.5 rounded-full px-2.5 text-[10px] font-medium",
            online ? "border-state-completed/40 text-state-completed" : "border-destructive/40 text-destructive"
          )}
        >
          <span className="relative flex size-1.5">
            {online && <span className="absolute inline-flex size-full animate-ping rounded-full bg-current opacity-75" />}
            <span className="relative inline-flex size-1.5 rounded-full bg-current" />
          </span>
          {online ? "LIVE" : "OFFLINE"}
        </Badge>

        <button
          aria-label="Notifications"
          className="relative grid size-8 place-items-center rounded-lg text-muted-foreground transition-colors hover:bg-accent hover:text-foreground focus-visible:ring-2 focus-visible:ring-primary"
        >
          <Bell className="size-4" />
        </button>

        <button
          onClick={() => setTheme(theme === "dark" ? "light" : "dark")}
          aria-label="Toggle theme"
          className="grid size-8 place-items-center rounded-lg text-muted-foreground transition-colors hover:bg-accent hover:text-foreground focus-visible:ring-2 focus-visible:ring-primary"
        >
          <Sun className="size-4 rotate-0 scale-100 transition-transform duration-200 dark:-rotate-90 dark:scale-0" />
          <Moon className="absolute size-4 rotate-90 scale-0 transition-transform duration-200 dark:rotate-0 dark:scale-100" />
        </button>

        <a
          href="https://github.com/tushar743-ui/DIS_JOB_SHD"
          target="_blank"
          rel="noreferrer noopener"
          aria-label="GitHub repository"
          className="grid size-8 place-items-center rounded-lg text-muted-foreground transition-colors hover:bg-accent hover:text-foreground focus-visible:ring-2 focus-visible:ring-primary"
        >
          <ExternalLink className="size-4" />
        </a>

        <DropdownMenu>
          <DropdownMenuTrigger
            render={
              <button
                aria-label="Account menu"
                className="grid size-8 place-items-center rounded-full bg-accent text-xs font-semibold focus-visible:ring-2 focus-visible:ring-primary"
              >
                {(user?.name ?? "?").charAt(0).toUpperCase()}
              </button>
            }
          />
          <DropdownMenuContent align="end" className="w-48">
            <DropdownMenuItem onClick={() => router.push("/settings")}>Profile</DropdownMenuItem>
            <DropdownMenuItem onClick={() => router.push("/settings")}>Settings</DropdownMenuItem>
            <DropdownMenuSeparator />
            <DropdownMenuItem onClick={() => router.push("/login")}>Sign out</DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </div>

      <CommandDialog open={open} onOpenChange={setOpen}>
        <Command>
        <CommandInput placeholder="Search jobs, queues, pages…" />
        <CommandList>
          <CommandEmpty>No results found.</CommandEmpty>
          <CommandGroup heading="Pages">
            {Object.entries(TITLES).map(([href, label]) => (
              <CommandItem key={href} value={label} onSelect={() => go(href)}>{label}</CommandItem>
            ))}
          </CommandGroup>
          {queueList && queueList.length > 0 && (
            <CommandGroup heading="Queues">
              {queueList.map((q) => (
                <CommandItem key={q.id} value={`queue ${q.name}`} onSelect={() => go(`/queues/${q.id}`)}>
                  {q.name}
                </CommandItem>
              ))}
            </CommandGroup>
          )}
          {jobList && jobList.length > 0 && (
            <CommandGroup heading="Recent jobs">
              {jobList.slice(0, 8).map((j) => (
                <CommandItem key={j.id} value={`job ${j.type} ${j.id}`} onSelect={() => go(`/jobs/${j.id}`)}>
                  <span className="truncate">{j.type}</span>
                  <span className="ml-auto font-mono text-[10px] text-muted-foreground">{j.id.slice(0, 8)}</span>
                </CommandItem>
              ))}
            </CommandGroup>
          )}
        </CommandList>
        </Command>
      </CommandDialog>
    </header>
  );
}
