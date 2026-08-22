import { Sidebar } from "@/components/sidebar";
import { ProjectBootstrap } from "@/components/project-bootstrap";

export default function DashboardLayout({ children }: { children: React.ReactNode }) {
  return (
    <div className="flex min-h-dvh">
      <ProjectBootstrap />
      <Sidebar />
      <main className="flex-1 overflow-auto bg-background">
        {children}
      </main>
    </div>
  );
}
