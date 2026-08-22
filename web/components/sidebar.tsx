"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { usePathname } from "next/navigation";
import { cn } from "@/lib/utils";
import {
  Gauge,
  Queue,
  Briefcase,
  Robot,
  Skull,
  ChartLine,
  Gear,
  SignOut,
  MoonStars,
  Sun,
} from "@phosphor-icons/react";
import { useTheme } from "next-themes";
import { useAuthStore } from "@/lib/auth-store";

const NAV = [
  { href: "/dashboard",         label: "Overview",   icon: Gauge },
  { href: "/dashboard/queues",  label: "Queues",     icon: Queue },
  { href: "/dashboard/jobs",    label: "Jobs",       icon: Briefcase },
  { href: "/dashboard/workers", label: "Workers",    icon: Robot },
  { href: "/dashboard/dlq",     label: "Dead Letter",icon: Skull },
  { href: "/dashboard/metrics", label: "Metrics",    icon: ChartLine },
  { href: "/dashboard/settings",label: "Settings",   icon: Gear },
];

export function Sidebar() {
  const path = usePathname();
  const { theme, setTheme } = useTheme();
  const { user, clear } = useAuthStore();
  const [mounted, setMounted] = useState(false);

  useEffect(() => { setMounted(true); }, []);

  return (
    <aside className="flex flex-col w-56 shrink-0 border-r border-border bg-card min-h-dvh sticky top-0">
      <div className="h-14 px-4 flex items-center border-b border-border gap-2">
        <span className="w-7 h-7 rounded-md bg-amber-500 flex items-center justify-center text-black font-bold text-sm font-mono">
          DJQ
        </span>
        <span className="text-sm font-semibold tracking-tight">dis-job-queue</span>
      </div>

      <nav className="flex-1 p-3 space-y-0.5 overflow-y-auto scrollbar-thin">
        {NAV.map(({ href, label, icon: Icon }) => {
          const active = path === href || (href !== "/dashboard" && path.startsWith(href));
          return (
            <Link
              key={href}
              href={href}
              className={cn(
                "flex items-center gap-2.5 px-3 py-2 rounded-md text-sm transition-colors",
                active
                  ? "bg-amber-500/10 text-amber-400 font-medium"
                  : "text-muted-foreground hover:text-foreground hover:bg-accent"
              )}
            >
              <Icon size={16} weight={active ? "fill" : "regular"} />
              {label}
            </Link>
          );
        })}
      </nav>

      <div className="p-3 border-t border-border space-y-1">
        <button
          onClick={() => setTheme(theme === "dark" ? "light" : "dark")}
          className="w-full flex items-center gap-2.5 px-3 py-2 rounded-md text-sm text-muted-foreground hover:text-foreground hover:bg-accent transition-colors"
        >
          {mounted && (theme === "dark" ? <Sun size={16} /> : <MoonStars size={16} />)}
          {mounted ? (theme === "dark" ? "Light mode" : "Dark mode") : "Theme"}
        </button>
        {user && (
          <div className="px-3 py-2">
            <p className="text-xs text-foreground font-medium truncate">{user.name}</p>
            <p className="text-xs text-muted-foreground truncate">{user.email}</p>
          </div>
        )}
        <button
          onClick={clear}
          className="w-full flex items-center gap-2.5 px-3 py-2 rounded-md text-sm text-muted-foreground hover:text-destructive hover:bg-destructive/10 transition-colors"
        >
          <SignOut size={16} />
          Sign out
        </button>
      </div>
    </aside>
  );
}
