// Service Requests (PRD §18): daftar dengan SLA, tenant, kategori; aksi cepat acknowledge/assign; export.
import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { Icon } from "@buildingvision/ui";
import type { ColumnDef } from "@tanstack/react-table";
import { PageHeader } from "@/components/shell/AppShell";
import { Button } from "@/components/ui/primitives";
import { DataGrid, FilterBar, useUrlFilters } from "@/components/bv/datagrid";
import { FlagBadges, PriorityBadge, StatusBadge } from "@/components/bv/badges";
import { LocationPath, RelativeTime, SLAProgress } from "@/components/bv/common";
import { useList } from "@/api/hooks";
import { useAuth } from "@/lib/auth";
import { statusMap } from "@/lib/status-map";
import type { ServiceRequest } from "@/api/types";
import { AssignDialog, TransitionActions, useExport } from "./dialogs";
import { CreateServiceRequestDialog, useSRCategories } from "./FindingDialogs";

export default function ServiceRequestListPage() {
  const { t } = useTranslation();
  const { propertyId, can } = useAuth();
  const f = useUrlFilters();
  const cats = useSRCategories();
  const query = useMemo(() => {
    const q: Record<string, string | undefined> = { ...f.all, property_id: propertyId ?? undefined };
    if (q.type) { q.category = q.type; delete q.type; }
    delete q.cursor;
    return q;
  }, [f.all, propertyId]);
  const list = useList<ServiceRequest>("service-requests", query);
  const rows = list.data?.pages.flatMap((p) => p.data) ?? [];
  const [createOpen, setCreateOpen] = useState(false);
  const [assign, setAssign] = useState<ServiceRequest | null>(null);
  const exp = useExport();
  const columns = useMemo<ColumnDef<ServiceRequest, unknown>[]>(
    () => [
      { id: "number", header: "ID", cell: ({ row }) => <span className="font-mono text-[13px] font-semibold">{row.original.request_number}</span>, size: 150 },
      { id: "title", header: t("label.title"), cell: ({ row }) => <div className="min-w-0"><div className="truncate font-medium">{row.original.title}</div><div className="text-xs text-muted-foreground">{row.original.category_name ?? row.original.category_code} · {row.original.channel}</div></div> },
      { id: "tenant", header: t("label.tenant"), cell: ({ row }) => <div><div>{row.original.tenant_name ?? row.original.requester_name ?? "—"}</div><LocationPath pathText={row.original.location.path_text} className="text-xs" /></div> },
      { id: "status", header: t("label.status"), cell: ({ row }) => <div className="flex flex-wrap gap-1"><StatusBadge objectType="service_request" status={row.original.status} /><FlagBadges flags={row.original.flags} /></div> },
      { id: "priority", header: t("label.priority"), cell: ({ row }) => <PriorityBadge priority={row.original.priority} />, size: 100 },
      { id: "sla", header: t("label.sla"), cell: ({ row }) => <div className="w-36"><SLAProgress sla={row.original.sla} /></div> },
      { id: "assignee", header: t("label.assignee"), cell: ({ row }) => row.original.assignee.user_name ?? row.original.assignee.team_name ?? <em className="text-muted-foreground">belum ditugaskan</em> },
      { id: "created", header: t("label.created"), cell: ({ row }) => <RelativeTime value={row.original.created_at} />, size: 110 },
      { id: "actions", header: "", cell: ({ row }) => <div className="flex justify-end gap-1" onClick={(e) => e.stopPropagation()}><TransitionActions objectType="service_request" item={row.original} compact onAssign={() => setAssign(row.original)} /></div> },
    ],
    [t],
  );
  return (
    <div>
      <PageHeader title={t("nav.service_requests")} actions={can("tenant.service_requests.create") && <Button onClick={() => setCreateOpen(true)}><Icon name="add" size={16} /> {t("action.create_service_request")}</Button>}>
        <FilterBar
          spec={{
            status: Object.entries(statusMap.service_request).map(([value, d]) => ({ value, label: d.label_id })),
            priority: true,
            location: true,
            assignee: true,
            team: true,
            dateRange: true,
            type: (cats.data ?? []).map((c) => ({ value: c.code, label: c.name })),
            presets: [
              { key: "open", label: "Open", params: { open: "true" } },
              { key: "new", label: "Baru", params: { status: "new" } },
              { key: "sla_risk", label: t("label.sla_risk"), params: { sla_risk: "true" } },
              { key: "mine", label: t("label.mine"), params: { mine: "true" } },
            ],
          }}
          onExport={can("platform.exports.create") ? () => exp.request("service_requests", Object.fromEntries(Object.entries(query).filter(([, v]) => v) as [string, string][])) : undefined}
        />
      </PageHeader>
      <DataGrid columns={columns} rows={rows} rowId={(r) => r.id} onRowClick={(r) => `/operations/service-requests/${r.id}`} loading={list.isLoading} isFiltered={f.isFiltered} empty={{ message: t("empty.service_requests") }} hasMore={list.hasNextPage} onLoadMore={() => list.fetchNextPage()} loadingMore={list.isFetchingNextPage} rowClassName={(r) => (r.flags.includes("sla_breach") ? "border-l-4 border-l-critical" : r.flags.includes("sla_risk") ? "border-l-4 border-l-warning" : undefined)} />
      <CreateServiceRequestDialog open={createOpen} onOpenChange={setCreateOpen} />
      {assign && <AssignDialog objectType="service_request" id={assign.id} open onOpenChange={(o) => !o && setAssign(null)} current={assign.assignee} />}
    </div>
  );
}
