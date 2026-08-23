"use client";

import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { motion } from "framer-motion";
import {
  LayoutDashboard, BarChart3, List, Calendar, Layers, AlertTriangle,
  GitBranch, Cpu, Settings, BookOpen, ChevronLeft, ChevronRight, LogOut,
  type LucideIcon,
} from "lucide-react";
import { cn } from "@/lib/utils";
import { useUIStore } from "@/lib/ui-store";
import { useAuthStore } from "@/lib/auth-store";
import { useBackendHealth } from "@/hooks/use-data";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { Badge } from "@/components/ui/badge";

interface NavItem { href: string; label: string; icon: LucideIcon }
interface NavGroup { label: string; items: NavItem[] }

const NAV: NavGroup[] = [
  {
    label: "Overview",
    items: [
      { href: "/dashboard", label: "Dashboard", icon: LayoutDashboard },
      { href: "/analytics", label: "Analytics", icon: BarChart3 },
    ],
  },
  {
    label: "Jobs",
    items: [
      { href: "/jobs", label: "Job Explorer", icon: List },
      { href: "/jobs/scheduled", label: "Scheduled Jobs", icon: Calendar },
      { href: "/jobs/batch", label: "Batch Jobs", icon: Layers },
      { href: "/dlq", label: "Dead Letter Queue", icon: AlertTriangle },
    ],
  },
  { label: "Queues", items: [{ href: "/queues", label: "Queue Manager", icon: GitBranch }] },
  { label: "Workers", items: [{ href: "/workers", label: "Worker Monitor", icon: Cpu }] },
  {
    label: "System",
    items: [
      { href: "/settings", label: "Settings", icon: Settings },
      { href: "/docs", label: "API Docs", icon: BookOpen },
    ],
  },
];

function isActive(pathname: string, href: string) {
  if (href === "/jobs") return pathname === "/jobs";
  return pathname === href || pathname.startsWith(href + "/");
}

export function AppSidebar() {
  const pathname = usePathname();
  const router = useRouter();
  const collapsed = useUIStore((s) => s.sidebarCollapsed);
  const toggle = useUIStore((s) => s.toggleSidebar);
  const { user, clear } = useAuthStore();
  const { online } = useBackendHealth();

  function signOut() {
    clear();
    router.push("/login");
  }

  return (
    <motion.aside
      animate={{ width: collapsed ? 56 : 224 }}
      transition={{ duration: 0.2, ease: "easeOut" }}
      className="sticky top-0 z-30 flex h-dvh shrink-0 flex-col overflow-hidden border-r border-border bg-card shadow-lg"
    >
      <div className="flex h-14 items-center gap-2 border-b border-border px-3">
        <span className="grid size-7 shrink-0 place-items-center rounded-md bg-primary font-mono text-xs font-bold text-primary-foreground">
          JF
        </span>
        {!collapsed && (
          <>
            <span className="truncate text-sm font-semibold tracking-tight">JobFlow</span>
            <Tooltip>
              <TooltipTrigger
                render={
                  <span className="relative ml-auto flex size-2" aria-label={online ? "Backend online" : "Backend unreachable"}>
                    <span
                      className={cn(
                        "absolute inline-flex size-full rounded-full opacity-75",
                        online ? "animate-ping bg-state-completed" : "bg-destructive"
                      )}
                    />
                    <span className={cn("relative inline-flex size-2 rounded-full", online ? "bg-state-completed" : "bg-destructive")} />
                  </span>
                }
              />
              <TooltipContent side="right">{online ? "Backend reachable" : "Backend unreachable"}</TooltipContent>
            </Tooltip>
          </>
        )}
        <button
          onClick={toggle}
          aria-label={collapsed ? "Expand sidebar" : "Collapse sidebar"}
          className={cn(
            "grid size-6 place-items-center rounded-md text-muted-foreground transition-colors hover:bg-accent hover:text-foreground focus-visible:ring-2 focus-visible:ring-primary",
            collapsed ? "mx-auto" : "ml-1"
          )}
        >
          {collapsed ? <ChevronRight className="size-4" /> : <ChevronLeft className="size-4" />}
        </button>
      </div>

      <nav className="flex-1 overflow-y-auto scrollbar-thin px-2 py-3">
        {NAV.map((group) => (
          <div key={group.label} className="mb-4">
            {!collapsed && (
              <p className="mb-1 px-2 text-[10px] font-medium uppercase tracking-wide text-muted-foreground">
                {group.label}
              </p>
            )}
            <ul className="space-y-0.5">
              {group.items.map((item) => {
                const active = isActive(pathname, item.href);
                const link = (
                  <Link
                    href={item.href}
                    aria-label={item.label}
                    aria-current={active ? "page" : undefined}
                    className={cn(
                      "relative flex h-9 items-center gap-2.5 rounded-lg px-2.5 text-sm transition-colors focus-visible:ring-2 focus-visible:ring-primary",
                      active ? "text-foreground" : "text-muted-foreground hover:text-foreground",
                      collapsed && "justify-center px-0"
                    )}
                  >
                    {active && (
                      <motion.span
                        layoutId="nav-pill"
                        className="absolute inset-0 rounded-lg bg-accent"
                        transition={{ type: "spring", stiffness: 380, damping: 32 }}
                      />
                    )}
                    <item.icon className="relative size-4 shrink-0" aria-hidden="true" />
                    {!collapsed && <span className="relative truncate">{item.label}</span>}
                  </Link>
                );
                return (
                  <li key={item.href}>
                    {collapsed ? (
                      <Tooltip>
                        <TooltipTrigger render={link} />
                        <TooltipContent side="right">{item.label}</TooltipContent>
                      </Tooltip>
                    ) : (
                      link
                    )}
                  </li>
                );
              })}
            </ul>
          </div>
        ))}
      </nav>

      <div className="border-t border-border p-2">
        {collapsed ? (
          <Tooltip>
            <TooltipTrigger
              render={
                <button
                  onClick={signOut}
                  aria-label="Sign out"
                  className="grid h-9 w-full place-items-center rounded-lg text-muted-foreground transition-colors hover:bg-accent hover:text-foreground focus-visible:ring-2 focus-visible:ring-primary"
                >
                  <LogOut className="size-4" />
                </button>
              }
            />
            <TooltipContent side="right">Sign out</TooltipContent>
          </Tooltip>
        ) : (
          <div className="flex items-center gap-2 rounded-lg p-1.5">
            <span className="grid size-7 shrink-0 place-items-center rounded-full bg-accent text-xs font-semibold">
              {(user?.name ?? "?").charAt(0).toUpperCase()}
            </span>
            <div className="min-w-0 flex-1">
              <p className="truncate text-xs font-medium">{user?.name ?? "Signed out"}</p>
              <p className="truncate text-[10px] text-muted-foreground">{user?.email ?? "-"}</p>
            </div>
            <Badge variant="outline" className="shrink-0 rounded-full text-[9px]">Owner</Badge>
            <button
              onClick={signOut}
              aria-label="Sign out"
              className="grid size-7 shrink-0 place-items-center rounded-md text-muted-foreground transition-colors hover:bg-accent hover:text-foreground focus-visible:ring-2 focus-visible:ring-primary"
            >
              <LogOut className="size-3.5" />
            </button>
          </div>
        )}
      </div>
    </motion.aside>
  );
}
