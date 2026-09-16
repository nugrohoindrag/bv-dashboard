// Incidents (PRD §14): daftar dengan severity, kategori, lokasi; Report Incident; export. Prop security → default incident_type security.
import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { Icon } from "@buildingvision/ui";
import type { ColumnDef } from "@tanstack/react-table";
import { PageHeader } from "@/components/shell/AppShell";
import { Button } from "@/components/ui/primitives";
import { DataGrid, FilterBar, useUrlFilters } from "@/components/bv/datagrid";
import { FlagBadges, PriorityBadge, SeverityBadge, StatusBadge } from "@/components/bv/badges";
import { LocationPath, RelativeTime } from "@/components/bv/common";
import { useList } from "@/api/hooks";
import { useAuth } from "@/lib/auth";
import { statusMap } from "@/lib/status-map";
import type { Incident } from "@/api/types";
import { AssignDialog, TransitionActions, useExport } from "./dialogs";
import { CreateIncidentDialog, INCIDENT_CATEGORIES } from "./FindingDialogs";

export default function IncidentListPage({ security }: { security?: boolean }) {
  const { t } = useTranslation();
  const { propertyId, can } = useAuth();
  const f = useUrlFilters();
  const query = useMemo(() => {
    const q: Record<string, string | undefined> = { ...f.all, property_id: propertyId ?? undefined };
    if (q.type) { q.category = q.type; delete q.type; }
    delete q.cursor;
    return q;
  }, [f.all, propertyId]);
  const list = useList<Incident>("incidents", query);
  const rows = list.data?.pages.flatMap((p) => p.data) ?? [];
  const [createOpen, setCreateOpen] = useState(false);
  const [assign, setAssign] = useState<Incident | null>(null);
  const exp = useExport();
  const columns = useMemo<ColumnDef<Incident, unknown>[]>(
    () => [
      { id: "number", header: "ID", cell: ({ row }) => <span className="font-mono text-[13px] font-semibold">{row.original.incident_number}</span>, size: 150 },
      { id: "title", header: t("label.title"), cell: ({ row }) => <div className="min-w-0"><div className="truncate font-medium">{row.original.title}</div><div className="text-xs text-muted-foreground">{row.original.incident_type} · {row.original.category}</div></div> },
      { id: "location", header: t("label.location"), cell: ({ row }) => <LocationPath pathText={row.original.location.path_text} className="max-w-[240px]" /> },
      { id: "severity", header: t("label.severity"), cell: ({ row }) => <SeverityBadge severity={row.original.severity} />, size: 120 },
      { id: "status", header: t("label.status"), cell: ({ row }) => <div className="flex flex-wrap gap-1"><StatusBadge objectType="incident" status={row.original.status} /><FlagBadges flags={row.original.flags} /></div> },
      { id: "priority", header: t("label.priority"), cell: ({ row }) => <PriorityBadge priority={row.original.priority} />, size: 100 },
      { id: "assignee", header: t("label.assignee"), cell: ({ row }) => row.original.assignee.user_name ?? row.original.assignee.team_name ?? <em className="text-muted-foreground">belum ditugaskan</em> },
      { id: "reported", header: "Dilaporkan", cell: ({ row }) => <div><RelativeTime value={row.original.reported_at} /><div className="text-xs text-muted-foreground">{row.original.reported_by_name}</div></div>, size: 130 },
      { id: "actions", header: "", cell: ({ row }) => <div className="flex justify-end gap-1" onClick={(e) => e.stopPropagation()}><TransitionActions objectType="incident" item={row.original} compact onAssign={() => setAssign(row.original)} /></div> },
    ],
    [t],
  );
  return (
    <div>
      <PageHeader title={security ? "Security Incidents" : t("nav.incidents")} actions={can("operations.incidents.create") && <Button onClick={() => setCreateOpen(true)}><Icon name="add" size={16} /> {t("action.report_incident")}</Button>}>
        <FilterBar
          spec={{
            status: Object.entries(statusMap.incident).map(([value, d]) => ({ value, label: d.label_id })),
            severity: true,
            location: true,
            assignee: true,
            team: true,
            dateRange: true,
            type: INCIDENT_CATEGORIES.map((c) => ({ value: c, label: c.replace("_", " ") })),
            presets: [
              { key: "open", label: "Open", params: { open: "true" } },
              { key: "critical", label: "Kritis", params: { severity: "critical" } },
              { key: "new", label: "Baru", params: { status: "new" } },
            ],
          }}
          onExport={can("platform.exports.create") ? () => exp.request("incidents", Object.fromEntries(Object.entries(query).filter(([, v]) => v) as [string, string][])) : undefined}
        />
      </PageHeader>
      <DataGrid columns={columns} rows={rows} rowId={(r) => r.id} onRowClick={(r) => `/operations/incidents/${r.id}`} loading={list.isLoading} isFiltered={f.isFiltered} empty={{ message: t("empty.incidents") }} hasMore={list.hasNextPage} onLoadMore={() => list.fetchNextPage()} loadingMore={list.isFetchingNextPage} rowClassName={(r) => (r.severity === "critical" && !["closed", "cancelled"].includes(r.status) ? "border-l-4 border-l-critical" : undefined)} />
      <CreateIncidentDialog open={createOpen} onOpenChange={setCreateOpen} defaults={{ incident_type: security ? "security" : undefined }} />
      {assign && <AssignDialog objectType="incident" id={assign.id} open onOpenChange={(o) => !o && setAssign(null)} current={assign.assignee} domain={security ? "security" : undefined} />}
    </div>
  );
}
