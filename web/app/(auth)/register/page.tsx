"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { z } from "zod";
import { AlertCircle, Eye, EyeOff } from "lucide-react";
import { auth } from "@/lib/api";
import { useAuthStore } from "@/lib/auth-store";
import { AuthShell } from "@/components/auth/auth-shell";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Checkbox } from "@/components/ui/checkbox";
import { ShimmerButton } from "@/components/ui/shimmer-button";

const schema = z
  .object({
    name: z.string().min(1, "Full name is required").max(80, "Keep it under 80 characters"),
    email: z.string().min(1, "Email is required").email("Enter a valid email address"),
    password: z.string().min(8, "Use at least 8 characters").max(72, "Keep it under 72 characters"),
    confirm: z.string().min(1, "Confirm your password"),
    terms: z.literal(true, { message: "You must accept the Terms of Service" }),
  })
  .refine((v) => v.password === v.confirm, {
    path: ["confirm"],
    message: "Passwords do not match",
  });

type Values = z.input<typeof schema>;

export default function RegisterPage() {
  const router = useRouter();
  const setAuth = useAuthStore((s) => s.setAuth);
  const [formError, setFormError] = useState("");
  const [reveal, setReveal] = useState(false);

  const {
    register, handleSubmit, setValue, watch,
    formState: { errors, isSubmitting },
  } = useForm<Values>({
    resolver: zodResolver(schema),
    defaultValues: { name: "", email: "", password: "", confirm: "", terms: false as unknown as true },
  });

  const terms = watch("terms");

  useEffect(() => {
    router.prefetch("/dashboard");
  }, [router]);

  async function onSubmit(values: Values) {
    setFormError("");
    try {
      const res = await auth.register(values.email, values.password, values.name);
      setAuth(res.access_token, res.refresh_token, {
        id: res.user_id,
        email: values.email,
        name: values.name,
      });
      router.push("/dashboard");
    } catch (e) {
      setFormError(e instanceof Error ? e.message : "Could not create the account");
    }
  }

  return (
    <AuthShell
      title="Create account"
      subtitle="Start scheduling jobs in minutes"
      footer={<>Already registered? <Link href="/login" className="text-primary hover:underline">Sign in</Link></>}
    >
      {formError && (
        <Alert variant="destructive" className="mb-4 rounded-lg">
          <AlertCircle className="size-4" aria-hidden="true" />
          <AlertDescription>{formError}</AlertDescription>
        </Alert>
      )}

      <form onSubmit={handleSubmit(onSubmit)} className="space-y-4">
        <div className="space-y-1.5">
          <Label htmlFor="name">Full name</Label>
          <Input id="name" autoComplete="name" className="rounded-md" aria-invalid={Boolean(errors.name)} {...register("name")} />
          {errors.name && <p className="text-xs text-destructive">{errors.name.message}</p>}
        </div>

        <div className="space-y-1.5">
          <Label htmlFor="email">Email</Label>
          <Input id="email" type="email" autoComplete="email" placeholder="you@example.com" className="rounded-md" aria-invalid={Boolean(errors.email)} {...register("email")} />
          {errors.email && <p className="text-xs text-destructive">{errors.email.message}</p>}
        </div>

        <div className="space-y-1.5">
          <Label htmlFor="password">Password</Label>
          <div className="relative">
            <Input
              id="password" type={reveal ? "text" : "password"} autoComplete="new-password"
              className="rounded-md pr-10" aria-invalid={Boolean(errors.password)} {...register("password")}
            />
            <button
              type="button" onClick={() => setReveal(!reveal)}
              aria-label={reveal ? "Hide password" : "Show password"}
              className="absolute inset-y-0 right-0 grid w-10 place-items-center text-muted-foreground hover:text-foreground focus-visible:ring-2 focus-visible:ring-primary"
            >
              {reveal ? <EyeOff className="size-4" /> : <Eye className="size-4" />}
            </button>
          </div>
          {errors.password && <p className="text-xs text-destructive">{errors.password.message}</p>}
        </div>

        <div className="space-y-1.5">
          <Label htmlFor="confirm">Confirm password</Label>
          <Input id="confirm" type={reveal ? "text" : "password"} autoComplete="new-password" className="rounded-md" aria-invalid={Boolean(errors.confirm)} {...register("confirm")} />
          {errors.confirm && <p className="text-xs text-destructive">{errors.confirm.message}</p>}
        </div>

        <div className="flex items-start gap-2">
          <Checkbox
            id="terms"
            checked={Boolean(terms)}
            onCheckedChange={(v) => setValue("terms", Boolean(v) as true, { shouldValidate: true })}
            aria-label="Accept the Terms of Service"
          />
          <Label htmlFor="terms" className="text-xs font-normal leading-5 text-muted-foreground">
            I agree to the Terms of Service
          </Label>
        </div>
        {errors.terms && <p className="text-xs text-destructive">{errors.terms.message}</p>}

        <ShimmerButton type="submit" disabled={isSubmitting} className="h-10 w-full text-sm">
          {isSubmitting ? "Creating…" : "Create Account"}
        </ShimmerButton>
      </form>
    </AuthShell>
  );
}
