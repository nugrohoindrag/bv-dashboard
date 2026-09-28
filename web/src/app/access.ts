// Konteks akses UI (permission + capability + admin_internal) untuk menu & route guard (PRD P0 §21).
import { useMemo } from "react";
import { useAuth } from "@/lib/auth";
import { useProfile } from "@/lib/profile";
import type { AccessContext } from "./navigation";

export function useAccessContext(): AccessContext & { capabilityLoading: boolean } {
  const { can, principal } = useAuth();
  const prof = useProfile();
  return useMemo(
    () => ({ can: (p: string) => can(p), hasCapability: (c: string) => prof.has(c), isInternalAdmin: !!principal?.is_internal_admin, isPlatformAdmin: !!principal?.is_platform_admin, capabilityLoading: prof.loading }),
    [can, prof, principal],
  );
}
