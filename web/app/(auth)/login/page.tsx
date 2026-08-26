"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { z } from "zod";
import { AlertCircle, Eye, EyeOff, Play } from "lucide-react";
import { auth } from "@/lib/api";
import { useAuthStore } from "@/lib/auth-store";
import { useFeatures } from "@/hooks/use-data";
import { useDemoSession } from "@/hooks/use-demo-session";
import { AuthShell } from "@/components/auth/auth-shell";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { ShimmerButton } from "@/components/ui/shimmer-button";

const schema = z.object({
  email: z.string().min(1, "Email is required").email("Enter a valid email address"),
  password: z.string().min(1, "Password is required"),
});

type Values = z.infer<typeof schema>;

export default function LoginPage() {
  const router = useRouter();
  const setAuth = useAuthStore((s) => s.setAuth);
  const [formError, setFormError] = useState("");
  const [reveal, setReveal] = useState(false);
  const { data: features } = useFeatures();
  const demo = useDemoSession();

  const {
    register, handleSubmit, formState: { errors, isSubmitting },
  } = useForm<Values>({ resolver: zodResolver(schema), defaultValues: { email: "", password: "" } });

  useEffect(() => {
    router.prefetch("/dashboard");
  }, [router]);

  async function onSubmit(values: Values) {
    setFormError("");
    try {
      const res = await auth.login(values.email, values.password);
      setAuth(res.access_token, res.refresh_token, { id: res.user_id, email: values.email, name: res.name });
      router.push("/dashboard");
    } catch (e) {
      setFormError(e instanceof Error ? e.message : "Sign in failed");
    }
  }

  return (
    <AuthShell
      title="Sign in"
      subtitle="JobFlow operations console"
      footer={<>No account? <Link href="/register" className="text-primary hover:underline">Create one</Link></>}
    >
      {(formError || demo.error) && (
        <Alert variant="destructive" className="mb-4 rounded-lg">
          <AlertCircle className="size-4" aria-hidden="true" />
          <AlertDescription>{formError || demo.error}</AlertDescription>
        </Alert>
      )}

      <form onSubmit={handleSubmit(onSubmit)} className="space-y-4">
        <div className="space-y-1.5">
          <Label htmlFor="email">Email</Label>
          <Input
            id="email" type="email" autoComplete="email" placeholder="you@example.com"
            className="rounded-md" aria-invalid={Boolean(errors.email)} {...register("email")}
          />
          {errors.email && <p className="text-xs text-destructive">{errors.email.message}</p>}
        </div>

        <div className="space-y-1.5">
          <Label htmlFor="password">Password</Label>
          <div className="relative">
            <Input
              id="password" type={reveal ? "text" : "password"} autoComplete="current-password"
              placeholder="••••••••" className="rounded-md pr-10"
              aria-invalid={Boolean(errors.password)} {...register("password")}
            />
            <button
              type="button"
              onClick={() => setReveal(!reveal)}
              aria-label={reveal ? "Hide password" : "Show password"}
              className="absolute inset-y-0 right-0 grid w-10 place-items-center text-muted-foreground hover:text-foreground focus-visible:ring-2 focus-visible:ring-primary"
            >
              {reveal ? <EyeOff className="size-4" /> : <Eye className="size-4" />}
            </button>
          </div>
          {errors.password && <p className="text-xs text-destructive">{errors.password.message}</p>}
        </div>

        <ShimmerButton type="submit" disabled={isSubmitting || demo.starting} className="h-11 w-full text-sm">
          {isSubmitting ? "Signing in…" : "Sign In"}
        </ShimmerButton>
      </form>

      {features?.demo_login && (
        <div className="mt-6">
          <div className="flex items-center gap-3" aria-hidden="true">
            <span className="h-px flex-1 bg-border" />
            <span className="text-xs uppercase tracking-wider text-muted-foreground">or</span>
            <span className="h-px flex-1 bg-border" />
          </div>

          <Button
            type="button"
            variant="outline"
            onClick={demo.start}
            disabled={isSubmitting || demo.starting}
            className="mt-4 h-11 w-full text-sm"
          >
            <Play className="size-4" aria-hidden="true" />
            {demo.starting ? "Starting demo…" : "Live demo"}
          </Button>

          <p className="mt-2.5 text-center text-xs text-muted-foreground">
            Opens a shared workspace with jobs running continuously. No account needed.
          </p>
        </div>
      )}
    </AuthShell>
  );
}
