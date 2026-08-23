import { Sidebar } from "@/components/sidebar";
import { ProjectBootstrap } from "@/components/project-bootstrap";
import { AuthGuard } from "@/components/auth-guard";

export default function DashboardLayout({ children }: { children: React.ReactNode }) {
  return (
    <AuthGuard>
      <div className="flex min-h-dvh">
        <ProjectBootstrap />
        <Sidebar />
        <main className="flex-1 overflow-auto bg-background">
          {children}
        </main>
      </div>
    </AuthGuard>
  );
}
