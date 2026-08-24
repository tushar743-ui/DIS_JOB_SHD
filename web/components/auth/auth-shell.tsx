"use client";

import { AnimatedGridPattern } from "@/components/ui/animated-grid-pattern";
import { BorderBeam } from "@/components/ui/border-beam";
import { cn } from "@/lib/utils";

export function AuthShell({
  title, subtitle, children, footer,
}: {
  title: string;
  subtitle: string;
  children: React.ReactNode;
  footer: React.ReactNode;
}) {
  return (
    <div className="relative grid min-h-dvh place-items-center overflow-hidden bg-background px-4">
      <AnimatedGridPattern
        numSquares={30}
        maxOpacity={0.1}
        duration={3}
        className={cn(
          "pointer-events-none absolute inset-0 h-full w-full skew-y-12",
          "[mask-image:radial-gradient(500px_circle_at_center,white,transparent)]"
        )}
      />

      <div className="relative w-full max-w-[480px]">
        <div className="relative overflow-hidden rounded-xl border border-border bg-card p-10 shadow-xl">
          <BorderBeam size={220} duration={10} colorFrom="var(--primary)" colorTo="transparent" />

          <div className="mb-8 text-center">
            <span className="mx-auto mb-5 grid size-12 place-items-center rounded-lg bg-primary font-mono text-base font-bold text-primary-foreground">
              JF
            </span>
            <h1 className="text-2xl font-semibold tracking-tight">{title}</h1>
            <p className="mt-1.5 text-sm text-muted-foreground">{subtitle}</p>
          </div>

          {children}
        </div>
        <div className="mt-6 text-center text-sm text-muted-foreground">{footer}</div>
      </div>
    </div>
  );
}
