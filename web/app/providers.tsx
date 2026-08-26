"use client";

import { SWRConfig } from "swr";
import { ThemeProvider } from "@/components/theme-provider";
import { TooltipProvider } from "@/components/ui/tooltip";

export function Providers({ children }: { children: React.ReactNode }) {
  return (
    <ThemeProvider attribute="class" defaultTheme="dark" enableSystem>
      <SWRConfig
        value={{
          revalidateOnFocus: true,
          focusThrottleInterval: 30000,
          revalidateIfStale: true,
          dedupingInterval: 2500,
          keepPreviousData: true,
          shouldRetryOnError: false,
        }}
      >
        <TooltipProvider>{children}</TooltipProvider>
      </SWRConfig>
    </ThemeProvider>
  );
}
