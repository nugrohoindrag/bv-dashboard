// Incidents (PRD §14): daftar dengan severity, kategori, lokasi; Report Incident; export. Prop security → default incident_type security.
import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { Icon } from "@buildingvision/ui";
import type { ColumnDef } from "@tanstack/react-table";
import { PageHeader } from "@/components/shell/AppShell";
import { Button } from "@/components/ui/primitives";
import { DataGrid, FilterBar, FilterSelect, useUrlFilters } from "@/components/bv/datagrid";
import { SeverityBadge } from "@/components/bv/badges";
import { CellLocation, CellStatus, CellText, CellTitle } from "@/components/bv/cells";
import { RelativeTime } from "@/components/bv/common";
import { Fab } from "@/components/bv/mobile";
import { useList } from "@/api/hooks";
import { useAuth } from "@/lib/auth";
import { statusMap } from "@/lib/status-map";
import { statusOptions } from "@/lib/status";
import type { Incident } from "@/api/types";
import { AssignDialog, TransitionActions, useExport } from "./dialogs";
import { CreateIncidentDialog, INCIDENT_CATEGORIES } from "./FindingDialogs";
import { CASE_SORTS, safeSort } from "@/lib/sort";
import { incidentQuery } from "@/lib/drilldown";

export default function IncidentListPage({ security }: { security?: boolean }) {
  const { t } = useTranslation();
  const { propertyId, can } = useAuth();
  const f = useUrlFilters();
  const query = useMemo(() => {
    // drill-down Security Dashboard: reported=today / from&to → created_from/created_to (server: reported_at)
    const q: Record<string, string | undefined> = { ...incidentQuery(f.all).query, property_id: propertyId ?? f.all.property_id ?? undefined, sort: safeSort(f.all.sort, CASE_SORTS) };
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
      // Tabel disederhanakan (29 Sep 2026, pola Tasks): nomor + judul, lokasi terakhir, severity (prioritas domain insiden),
      // satu status + satu flag terpenting, assignee, waktu lapor. Tipe/kategori, prioritas, SLA & pelapor ada di halaman detail.
      { id: "title", header: "Insiden", meta: { mobile: "primary" }, cell: ({ row }) => <CellTitle code={row.original.incident_number} title={row.original.title} /> },
      { id: "location", header: t("label.location"), meta: { mobile: "secondary" }, cell: ({ row }) => <CellLocation path={row.original.location.path_text} max={150} /> },
      { id: "severity", header: t("label.severity"), meta: { mobile: "secondary" }, cell: ({ row }) => <SeverityBadge severity={row.original.severity} />, size: 110 },
      { id: "status", header: t("label.status"), meta: { mobile: "status" }, cell: ({ row }) => <CellStatus objectType="incident" status={row.original.status} item={row.original} /> },
      { id: "assignee", header: t("label.assignee"), meta: { mobile: "secondary" }, cell: ({ row }) => { const n = row.original.assignee.user_name ?? row.original.assignee.team_name; return <CellText max={120} muted={!n} className={n ? undefined : "italic"}>{n ?? "belum ditugaskan"}</CellText>; } },
      { id: "reported", header: "Dilaporkan", meta: { mobile: "hidden" }, cell: ({ row }) => <span className="whitespace-nowrap text-sm"><RelativeTime value={row.original.reported_at} /></span>, size: 120 },
      { id: "actions", header: "", cell: ({ row }) => <div className="flex justify-end gap-1" onClick={(e) => e.stopPropagation()}><TransitionActions objectType="incident" item={row.original} compact onAssign={() => setAssign(row.original)} /></div> },
    ],
    [t],
  );
  return (
    <div>
      <PageHeader title={security ? "Security Incidents" : t("nav.incidents")} actions={can("operations.incidents.create") && <span className="hidden md:inline-flex"><Button onClick={() => setCreateOpen(true)}><Icon name="add" size={16} /> {t("action.report_incident")}</Button></span>}>
        <FilterBar
          spec={{
            status: Object.entries(statusMap.incident).map(([value, d]) => ({ value, label: d.label_id })),
            severity: true,
            location: true,
            assignee: true,
            team: true,
            dateRange: true,
            type: INCIDENT_CATEGORIES.map((c) => ({ value: c, label: c.replace("_", " ") })),
            extra: <FilterSelect param="sla_status" label="SLA" options={statusOptions("sla_status")} />, // PRD P1 v2 §33
            presets: [
              { key: "open", label: "Open", params: { open: "true" } },
              { key: "critical", label: "Kritis", params: { severity: "critical" } },
              { key: "new", label: "Baru", params: { status: "new" } },
            ],
          }}
          onExport={can("platform.exports.create") ? () => exp.request("incidents", Object.fromEntries(Object.entries(query).filter(([, v]) => v) as [string, string][])) : undefined}
        />
      </PageHeader>
      <DataGrid columns={columns} rows={rows} rowId={(r) => r.id} onRowClick={(r) => `/operations/incidents/${r.id}`} loading={list.isLoading} error={list.error} onRetry={() => list.refetch()} isFiltered={f.isFiltered} empty={{ icon: "emergency_home", title: t("empty.incidents"), description: t("empty.incidents_desc"), action: can("operations.incidents.create") ? <Button icon="add" onClick={() => setCreateOpen(true)}>{t("action.report_incident")}</Button> : undefined }} hasMore={list.hasNextPage} onLoadMore={() => list.fetchNextPage()} loadingMore={list.isFetchingNextPage} rowClassName={(r) => (r.severity === "critical" && !["closed", "cancelled"].includes(r.status) ? "border-l-4 border-l-critical" : undefined)} />
      {can("operations.incidents.create") && <Fab label={t("action.create")} aria-label={t("action.report_incident")} onClick={() => setCreateOpen(true)} />}
      <CreateIncidentDialog open={createOpen} onOpenChange={setCreateOpen} defaults={{ incident_type: security ? "security" : undefined }} />
      {assign && <AssignDialog objectType="incident" id={assign.id} open onOpenChange={(o) => !o && setAssign(null)} current={assign.assignee} domain={security ? "security" : undefined} />}
    </div>
  );
}
