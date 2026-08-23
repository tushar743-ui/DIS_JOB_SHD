import { AppSidebar } from "@/components/layout/app-sidebar";
import { TopBar } from "@/components/layout/top-bar";
import { PageWrapper } from "@/components/layout/page-wrapper";
import { ProjectBootstrap } from "@/components/project-bootstrap";
import { AuthGuard } from "@/components/auth-guard";

export default function DashboardLayout({ children }: { children: React.ReactNode }) {
  return (
    <AuthGuard>
      <div className="flex min-h-dvh">
        <ProjectBootstrap />
        <AppSidebar />
        <main className="flex min-w-0 flex-1 flex-col bg-background">
          <TopBar />
          <PageWrapper>{children}</PageWrapper>
        </main>
      </div>
    </AuthGuard>
  );
}
