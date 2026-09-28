// Hook data Workforce (PRD P2 v2.1 §8). Shift per domain (D-P2-05): /security/* dan /housekeeping/* dengan permission
// {domain}.shifts.*. Daftar staf untuk roster/serah terima memakai /users bila boleh (iam.users.view); supervisor domain
// tanpa iam.* memakai anggota team domain (GET /{domain}/staff, {domain}.shifts.view).
import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { useAll } from "@/api/hooks";
import { api, type ListResponse } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import type { DomainStaff, ShiftDefinition, ShiftDomain, User } from "@/api/types";

/** Definisi shift domain (aktif & nonaktif) untuk property (kosong = semua property yang diizinkan). */
export function useShifts(domain: ShiftDomain, propertyId?: string | null, opts: { enabled?: boolean } = {}) {
  const { can } = useAuth();
  return useQuery({
    queryKey: ["shifts", domain, propertyId ?? null],
    enabled: can(`${domain}.shifts.view`) && opts.enabled !== false,
    queryFn: ({ signal }) => api<ListResponse<ShiftDefinition>>(`${domain}/shifts`, { query: { property_id: propertyId ?? undefined }, signal }).then((r) => r.data),
    staleTime: 60_000,
  });
}

export interface StaffOption {
  id: string;
  name: string;
  hint?: string;
}

/** Staf yang dapat dipilih (roster, penerima serah terima, assignee default route). */
export function useStaffOptions(domain: ShiftDomain, propertyId?: string | null, teamId?: string | null) {
  const { can } = useAuth();
  const viaUsers = can("iam.users.view");
  const viaDomain = !viaUsers && can(`${domain}.shifts.view`);
  const users = useAll<User>("users", { property_id: propertyId ?? undefined, team_id: teamId ?? undefined, is_active: true }, { enabled: viaUsers });
  const staff = useAll<DomainStaff>(`${domain}/staff`, { property_id: propertyId ?? undefined, team_id: teamId ?? undefined }, { enabled: viaDomain });
  const options = useMemo<StaffOption[]>(() => {
    if (viaUsers) return (users.data ?? []).map((u) => ({ id: u.id, name: u.full_name, hint: u.teams.map((t) => t.team_name).filter(Boolean).join(", ") || u.roles.map((r) => r.role_name).filter(Boolean).join(", ") }));
    return (staff.data ?? []).map((s) => ({ id: s.user_id, name: s.full_name, hint: s.team_names.join(", ") }));
  }, [viaUsers, users.data, staff.data]);
  return { options, isLoading: viaUsers ? users.isLoading : staff.isLoading, available: viaUsers || viaDomain };
}
