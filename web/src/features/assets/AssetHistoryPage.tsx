// Asset History (PRD §13.4): pilih aset → riwayat gabungan (WO/Task/PM/activity) — juga tersedia di detail aset.
import { useState } from "react";
import { Link } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { useQuery } from "@tanstack/react-query";
import { PageHeader } from "@/components/shell/AppShell";
import { AssetPicker } from "@/components/bv/pickers";
import { AsyncState, EmptyState } from "@/components/bv/common";
import { StatusBadge, objectTypeLabel } from "@/components/bv/badges";
import { itemLink } from "@/components/bv/cards";
import { api } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { fmtDateTime } from "@/lib/format";

interface HistoryItem { kind: string; occurred_at: string; object_type: string; object_id: string; label: string; title: string; status: string; actor_name: string }

export default function AssetHistoryPage() {
  const { t } = useTranslation();
  const { propertyId } = useAuth();
  const [assetId, setAssetId] = useState<string | null>(null);
  const history = useQuery({ queryKey: ["asset-history", assetId], enabled: !!assetId, queryFn: () => api<{ data: HistoryItem[] }>(`assets/${assetId}/history`, { query: { limit: 300 } }).then((r) => r.data) });
  return (
    <div>
      <PageHeader title={`${t("nav.assets")} · ${t("nav.history")}`} subtitle="Riwayat Work Order, Task, jadwal PM, dan perubahan aset.">
        <div className="flex items-center gap-2">
          <AssetPicker propertyId={propertyId} value={assetId} onChange={setAssetId} className="w-96" />
          {assetId && <Link to={`/assets/${assetId}`} className="text-sm text-brand-600 hover:underline">Buka detail aset →</Link>}
        </div>
      </PageHeader>
      {!assetId ? <EmptyState message="Pilih aset untuk melihat riwayat." /> : (
        <AsyncState query={history}>
          {(items) => items.length === 0 ? <EmptyState message="Belum ada riwayat untuk aset ini." /> : (
            <ol className="relative ml-2 space-y-4 border-l border-border pl-5">
              {items.map((h, i) => (
                <li key={i} className="relative">
                  <span className="absolute -left-[26px] top-1.5 h-2.5 w-2.5 rounded-full border-2 border-card bg-brand-600" />
                  <div className="flex items-center justify-between gap-2 text-xs text-muted-foreground"><span>{objectTypeLabel[h.object_type] ?? h.kind} · {h.actor_name || "System"}</span><span className="tnum">{fmtDateTime(h.occurred_at)}</span></div>
                  {h.kind === "activity" ? <div className="text-body">{h.label} {h.status && <span className="text-muted-foreground">{h.status} → {h.title}</span>}</div> : (
                    <div className="flex items-center gap-2"><Link to={itemLink(h.object_type, h.object_id)} className="hover:underline"><span className="font-mono text-[13px] font-semibold">{h.label}</span> {h.title}</Link><StatusBadge objectType={h.object_type === "maintenance_schedule" ? "maintenance_schedule" : h.object_type === "task" ? "task" : "work_order"} status={h.status} /></div>
                  )}
                </li>
              ))}
            </ol>
          )}
        </AsyncState>
      )}
    </div>
  );
}
