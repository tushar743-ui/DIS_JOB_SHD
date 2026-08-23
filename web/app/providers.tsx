"use client";

import { SWRConfig } from "swr";
import { ThemeProvider } from "@/components/theme-provider";
import { TooltipProvider } from "@/components/ui/tooltip";

export function Providers({ children }: { children: React.ReactNode }) {
  return (
    <ThemeProvider attribute="class" defaultTheme="dark" enableSystem>
      <SWRConfig
        value={{
          refreshInterval: 2000,
          revalidateOnFocus: true,
          dedupingInterval: 1000,
          keepPreviousData: true,
          shouldRetryOnError: false,
        }}
      >
        <TooltipProvider>{children}</TooltipProvider>
      </SWRConfig>
    </ThemeProvider>
  );
}
