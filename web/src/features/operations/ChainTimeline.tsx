// Rantai pekerjaan lintas tim (PRD P1 v2.1 §5.15 P1-XMW-04; Roadmap v2.1 §17 contoh 1): seluruh Task / Finding / WO / Incident
// yang lahir dari Service Request — team, status, SLA. SR baru Resolved setelah semua langkah selesai (P1-XMW-02).
import { Link } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/primitives";
import { Icon } from "@buildingvision/ui";
import { SLAStatusBadge, StatusBadge, objectTypeLabel, workTypeLabel } from "@/components/bv/badges";
import type { ObjectType } from "@/lib/status-map";
import { AsyncState, RelativeTime } from "@/components/bv/common";
import { api } from "@/lib/api";
import { cn } from "@/lib/utils";
import type { ChainView } from "@/api/types";

const OBJECT_ICON: Record<string, string> = { task: "task_alt", work_order: "construction", finding: "report_problem", incident: "emergency_home" };

/** Label tujuan Task tindak lanjut (P1-XMW-03 / P2-XTW-01). */
export const followUpPurposeLabel: Record<string, string> = {
  final_inspection: "Inspeksi akhir",
  re_clean: "Re-clean",
  security_verification: "Verifikasi security",
  inspection: "Inspeksi",
  follow_up: "Tindak lanjut",
};

const DOMAIN_LABEL: Record<string, string> = { engineering: "Engineering", security: "Security", housekeeping: "Housekeeping", tenant_relation: "Tenant Relation", management: "Management" };

export function useServiceRequestChain(srId?: string) {
  // prefix "one" → ikut di-invalidate setelah aksi apa pun (useAction)
  return useQuery({ queryKey: ["one", "service-request-chain", srId], enabled: !!srId, queryFn: ({ signal }) => api<ChainView>(`service-requests/${srId}/chain`, { signal }), staleTime: 10_000 });
}

export function ServiceRequestChainCard({ srId }: { srId: string }) {
  const chain = useServiceRequestChain(srId);
  return (
    <Card>
      <CardHeader>
        <CardTitle>Rantai pekerjaan lintas tim</CardTitle>
        {chain.data && chain.data.items.length > 0 && (
          <span className="text-xs text-on-surface-variant">
            {chain.data.items.length} langkah · {chain.data.team_count} team · {chain.data.open_count > 0 ? <span className="font-semibold text-warning-text">{chain.data.open_count} belum selesai</span> : <span className="font-semibold text-success-text">semua selesai</span>}
          </span>
        )}
      </CardHeader>
      <CardContent>
        <AsyncState query={chain}>
          {(cv) =>
            cv.items.length === 0 ? (
              <p className="py-2 text-sm text-on-surface-variant">Belum ada Task atau Work Order dari request ini.</p>
            ) : (
              <div className="space-y-1">
                <ol className="relative space-y-2" aria-label="Rantai pekerjaan">
                  {cv.items.map((it) => (
                    <li key={it.id} className="flex items-start gap-3" style={{ paddingLeft: `${Math.min(it.depth - 1, 5) * 16}px` }}>
                      <span className={cn("mt-0.5 flex h-7 w-7 shrink-0 items-center justify-center rounded-full", it.open ? "bg-warning-soft text-warning-text" : "bg-success-soft text-success-text")}>
                        <Icon name={OBJECT_ICON[it.object_type] ?? "radio_button_unchecked"} size={16} />
                      </span>
                      <div className="min-w-0 flex-1">
                        <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
                          <Link to={it.deep_link} className="font-mono text-sm font-semibold text-brand-600 hover:underline">{it.number}</Link>
                          <span className="text-xs text-on-surface-variant">{objectTypeLabel[it.object_type] ?? it.object_type}{it.follow_up_purpose ? ` · ${followUpPurposeLabel[it.follow_up_purpose] ?? it.follow_up_purpose}` : it.type ? ` · ${workTypeLabel[it.type] ?? it.type}` : ""}</span>
                          <StatusBadge objectType={it.object_type as ObjectType} status={it.status} />
                          {it.sla_status && <SLAStatusBadge status={it.sla_status} />}
                        </div>
                        <div className="truncate text-sm text-on-surface">{it.title}</div>
                        <div className="text-xs text-on-surface-variant">
                          {it.team_name ? <>{it.team_name}{it.team_domain ? ` (${DOMAIN_LABEL[it.team_domain] ?? it.team_domain})` : ""}</> : "Tanpa team"}
                          {it.assignee_name && <> · {it.assignee_name}</>}
                          {it.parent_label && <> · dari <span className="font-mono">{it.parent_label}</span></>}
                          {" · "}<RelativeTime value={it.created_at} />
                        </div>
                      </div>
                    </li>
                  ))}
                </ol>
                <p className="pt-2 text-xs text-on-surface-variant">Request otomatis Resolved setelah seluruh langkah di atas selesai; tenant hanya melihat status request.</p>
              </div>
            )
          }
        </AsyncState>
      </CardContent>
    </Card>
  );
}
