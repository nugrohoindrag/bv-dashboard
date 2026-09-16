// Verify Email (Website PRD §25): token dari tautan email → organization + trial workspace dibuat → auto login → /onboarding.
import { useEffect, useRef, useState } from "react";
import { Link, useNavigate, useSearchParams } from "react-router-dom";
import { BuildingVisionLogo } from "@buildingvision/ui/bv";
import { Alert, Button, Card, Icon, Skeleton } from "@/components/ui/primitives";
import { api, ApiError, tokenStore } from "@/lib/api";
import { useAuth } from "@/lib/auth";

export function VerifyEmailPage() {
  const [params] = useSearchParams();
  const token = params.get("token") ?? "";
  const nav = useNavigate();
  const { refreshPrincipal } = useAuth();
  const [state, setState] = useState<{ status: "working" | "done" | "error"; code?: string; message?: string }>({ status: "working" });
  const started = useRef(false);

  useEffect(() => {
    if (started.current) return;
    started.current = true;
    (async () => {
      if (!token) {
        setState({ status: "error", code: "MISSING", message: "This link is missing its verification code." });
        return;
      }
      try {
        const res = await api<{ access_token: string; next: string }>("public/signup/verify", { body: { token }, retry: false });
        tokenStore.set(res.access_token);
        await refreshPrincipal();
        setState({ status: "done" });
        window.setTimeout(() => nav(res.next || "/onboarding", { replace: true }), 900);
      } catch (err) {
        if (err instanceof ApiError) setState({ status: "error", code: err.code, message: err.message });
        else setState({ status: "error", message: "Something went wrong. Please try again." });
      }
    })();
  }, [token, nav, refreshPrincipal]);

  return (
    <div className="flex min-h-screen items-center justify-center bg-background px-4">
      <Card className="w-full max-w-md p-8">
        <BuildingVisionLogo height={36} />
        <div className="mt-6">
          {state.status === "working" && (
            <div className="space-y-3">
              <h1 className="text-h2 font-bold text-on-surface">Setting up your workspace</h1>
              <p className="text-sm text-on-surface-variant">Confirming your email and preparing your trial. This only takes a moment.</p>
              <Skeleton className="h-3 w-3/4" />
              <Skeleton className="h-3 w-1/2" />
            </div>
          )}
          {state.status === "done" && (
            <div className="space-y-3">
              <div className="flex h-12 w-12 items-center justify-center rounded-full bg-success-container text-on-success-container"><Icon name="check" size={26} /></div>
              <h1 className="text-h2 font-bold text-on-surface">You are all set</h1>
              <p className="text-sm text-on-surface-variant">Your email is confirmed and your free trial has started. Taking you to onboarding.</p>
            </div>
          )}
          {state.status === "error" && (
            <div className="space-y-4">
              <h1 className="text-h2 font-bold text-on-surface">{state.code === "SIGNUP_ALREADY_VERIFIED" ? "Already verified" : "This link did not work"}</h1>
              <Alert variant={state.code === "SIGNUP_ALREADY_VERIFIED" ? "info" : "warning"}>{state.message}</Alert>
              <div className="flex gap-2">
                {state.code === "SIGNUP_ALREADY_VERIFIED" ? (
                  <Button onClick={() => nav("/login")}>Log in</Button>
                ) : (
                  <Button onClick={() => nav("/signup")}>Back to sign up</Button>
                )}
                <Link to="/login" className="self-center text-sm text-primary underline">Log in</Link>
              </div>
            </div>
          )}
        </div>
      </Card>
    </div>
  );
}

export default VerifyEmailPage;
