// Sync Conflicts (PRD §21 / OD-004, docs/conflict-rules.md): mutasi offline yang ditolak/konflik — supervisor meninjau & menandai sudah ditinjau.
import { useState } from "react";
import { Link } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { useQuery } from "@tanstack/react-query";
import { Button, Checkbox } from "@/components/ui/primitives";
import { AsyncState, RelativeTime, useToast } from "@/components/bv/common";
import { StatusBadge, objectTypeLabel } from "@/components/bv/badges";
import { useAction } from "@/api/hooks";
import { api } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { fmtDateTime } from "@/lib/format";
import { cn } from "@/lib/utils";
import type { SyncConflict } from "@/api/types";

const REASON: Record<string, string> = {
  INVALID_TRANSITION: "Transisi tidak valid — status server sudah berubah (C1)",
  OBJECT_TERMINAL: "Object sudah closed/cancelled di server (C2)",
  REASSIGNED: "Sudah ditugaskan ke orang lain (C3)",
  DUPLICATE_SESSION: "Sesi duplikat dari perangkat lain (C4)",
  SEQ_GAP: "Urutan mutasi tidak lengkap (C5)",
  VALIDATION_ERROR: "Data tidak valid (C6)",
  FORBIDDEN: "Tidak berizin (C7)",
  NOT_FOUND: "Object tidak ditemukan (C8)",
};

export default function SyncConflictsSection() {
  const { t } = useTranslation();
  const { propertyId, can } = useAuth();
  const toast = useToast();
  const [all, setAll] = useState(false);
  const list = useQuery({ queryKey: ["sync-conflicts", propertyId, all], queryFn: () => api<{ data: SyncConflict[] }>("sync/conflicts", { query: { property_id: propertyId ?? undefined, include_acknowledged: all || undefined } }).then((r) => r.data) });
  const ack = useAction<{ id: string }>((i) => `sync/conflicts/${i.id}/acknowledge`, { body: () => ({}), invalidate: ["sync-conflicts"] });
  return (
    <div className="space-y-3">
      <div className="flex items-center justify-between">
        <p className="text-sm text-muted-foreground">Evidence (foto/checklist) dari mutasi konflik tetap tersimpan di object. Tinjau lalu tandai.</p>
        <label className="flex items-center gap-2 text-sm"><Checkbox checked={all} onCheckedChange={(v) => setAll(!!v)} /> Tampilkan yang sudah ditinjau</label>
      </div>
      <AsyncState query={list}>
        {(items) => items.length === 0 ? <p className="py-10 text-center text-sm text-muted-foreground">Tidak ada konflik sinkronisasi. 👍</p> : (
          <ul className="divide-y divide-border rounded-lg border border-border">
            {items.map((c) => (
              <li key={c.client_mutation_id} className={cn("flex items-start justify-between gap-4 px-4 py-3", c.acknowledged_at && "opacity-60")}>
                <div className="min-w-0 space-y-1">
                  <div className="flex flex-wrap items-center gap-2 text-sm">
                    <span className="rounded-full bg-critical-soft px-2 py-0.5 text-xs font-medium text-critical-text">{c.reason_code ?? "CONFLICT"}</span>
                    <span className="text-muted-foreground">{objectTypeLabel[c.object_type] ?? c.object_type}</span>
                    <Link to={c.deep_link} className="hover:underline"><span className="font-mono text-[13px] font-semibold">{c.object_label}</span> {c.object_title}</Link>
                    <StatusBadge objectType={(c.object_type === "work_order" ? "work_order" : "task")} status={c.object_status} />
                  </div>
                  <div className="text-sm">Aksi <span className="font-mono text-xs">{c.action}</span> oleh <span className="font-medium">{c.worker_name}</span> · perangkat <span className="font-mono text-xs">{c.device_id}</span> · diterima <RelativeTime value={c.received_at} /></div>
                  <div className="text-xs text-muted-foreground">{REASON[c.reason_code ?? ""] ?? c.reason_code}{c.detail ? ` — ${c.detail}` : ""}</div>
                  {c.acknowledged_at && <div className="text-xs text-muted-foreground">Ditinjau {fmtDateTime(c.acknowledged_at)}</div>}
                </div>
                {!c.acknowledged_at && can("sync.conflicts.acknowledge") && <Button size="sm" variant="secondary" loading={ack.isPending} onClick={() => ack.mutateAsync({ id: c.client_mutation_id }).then(() => toast.success("Konflik ditandai ditinjau")).catch(toast.error)}>{t("action.acknowledge_conflict")}</Button>}
              </li>
            ))}
          </ul>
        )}
      </AsyncState>
    </div>
  );
}
