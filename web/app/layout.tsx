import type { Metadata } from "next";
import { Open_Sans, Geist_Mono } from "next/font/google";
import "./globals.css";
import { ThemeProvider } from "@/components/theme-provider";
import { Toaster } from "@/components/ui/toast";

// next/font self-hosts the files, so no <link> to fonts.googleapis.com is
// needed and there is no render-blocking round trip. The variable font covers
// the full 300–800 range in both styles.
const openSans = Open_Sans({
  variable: "--font-open-sans",
  subsets: ["latin"],
  style: ["normal", "italic"],
  display: "swap",
});

const geistMono = Geist_Mono({
  variable: "--font-geist-mono",
  subsets: ["latin"],
});

export const metadata: Metadata = {
  title: "DJQ | Distributed Job Queue",
  description: "Production-grade distributed job scheduling platform",
};

export default function RootLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  return (
    // The font variables live on <html>: Tailwind's preflight sets font-family
    // there, so a variable scoped to <body> would be undefined at that point and
    // the whole declaration would fall back to the system stack.
    <html lang="en" className={`${openSans.variable} ${geistMono.variable}`} suppressHydrationWarning>
      <body className="font-sans antialiased">
        <ThemeProvider attribute="class" defaultTheme="dark" enableSystem>
          {children}
          <Toaster />
        </ThemeProvider>
      </body>
    </html>
  );
}
