// Query hook Tenant Relation (dipisah dari file komponen agar file .tsx hanya mengekspor komponen).
import { useEffect, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { api, type ListResponse } from "@/lib/api";
import type { CommunicationEntry, TenantMessage, WhatsAppLog } from "./types";

/** Thread pesan tenant ↔ building management pada Service Request (refresh 30 detik). */
export function useTenantMessages(srId?: string, enabled = true) {
  return useQuery({ queryKey: ["sr-messages", srId], enabled: !!srId && enabled, queryFn: () => api<ListResponse<TenantMessage>>(`service-requests/${srId}/messages`).then((r) => r.data), refetchInterval: 30_000 });
}

/** Log komunikasi ke tenant per Service Request: in-app + status push, pesan, WhatsApp manual (P3-TRC-03). */
export function useSRCommunications(srId?: string, enabled = true) {
  return useQuery({ queryKey: ["sr-communications", srId], enabled: !!srId && enabled, queryFn: ({ signal }) => api<ListResponse<CommunicationEntry>>(`service-requests/${srId}/communications`, { signal }).then((r) => r.data), refetchInterval: 60_000 });
}

/** Riwayat WhatsApp manual untuk satu object (tenant_user, package, parking_permit, …). */
export function useWhatsAppLogs(objectType: string, objectId?: string | null, enabled = true) {
  return useQuery({
    queryKey: ["whatsapp-logs", objectType, objectId],
    enabled: !!objectId && enabled,
    queryFn: ({ signal }) => api<ListResponse<WhatsAppLog>>("whatsapp/logs", { query: { object_type: objectType, object_id: objectId }, signal }).then((r) => r.data),
  });
}

/** Waktu sekarang (ms) yang diperbarui tiap `intervalMs` — untuk status jadwal/kedaluwarsa tanpa memanggil Date.now() saat render. */
export function useNow(intervalMs = 60_000): number {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    if (!intervalMs) return;
    const t = setInterval(() => setNow(Date.now()), intervalMs);
    return () => clearInterval(t);
  }, [intervalMs]);
  return now;
}
