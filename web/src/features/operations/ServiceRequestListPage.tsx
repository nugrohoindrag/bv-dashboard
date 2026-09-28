// Service Requests (PRD §18): daftar dengan SLA, tenant, kategori; aksi cepat acknowledge/assign; export.
// PRD P3 v2.1 P3-TSH-09: drill-down KPI layanan tenant memakai parameter URL yang diteruskan apa adanya ke API (status, sla_status,
// channel, request_type, category, created_from/to, resolved_from/to, reopened_from, recurring_issue_id); parameter tanpa kontrol
// filter diringkas dalam bahasa manusia di atas daftar.
import { useEffect, useMemo, useState } from "react";
import { Link } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { Icon } from "@buildingvision/ui";
import type { ColumnDef } from "@tanstack/react-table";
import { PageHeader } from "@/components/shell/AppShell";
import { Button } from "@/components/ui/primitives";
import { DataGrid, FilterBar, FilterSelect, useUrlFilters } from "@/components/bv/datagrid";
import { PriorityBadge, requestTypeLabel } from "@/components/bv/badges";
import { CellStatus, CellText, CellTitle } from "@/components/bv/cells";
import { RelativeTime } from "@/components/bv/common";
import { Fab } from "@/components/bv/mobile";
import { useList, useOne } from "@/api/hooks";
import { useAuth } from "@/lib/auth";
import { statusMap } from "@/lib/status-map";
import { statusOptions } from "@/lib/status";
import { fmtDateTime } from "@/lib/format";
import { SR_CHANNELS, labelOf, optionsOf } from "@/features/tenant-relation/labels";
import type { RecurringIssue } from "@/features/tenant-relation/types";
import type { ServiceRequest } from "@/api/types";
import { AssignDialog, TransitionActions, useExport } from "./dialogs";
import { CreateServiceRequestDialog, useSRCategories } from "./FindingDialogs";

export default function ServiceRequestListPage() {
  const { t } = useTranslation();
  const { propertyId, can } = useAuth();
  const f = useUrlFilters();
  const cats = useSRCategories();
  // drill-down dashboard memakai `category`; kontrol filter kategori di FilterBar memakai `type` → samakan di URL
  const urlCategory = f.get("category");
  const urlType = f.get("type");
  useEffect(() => {
    if (urlCategory && !urlType) f.set({ type: urlCategory, category: null });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [urlCategory, urlType]);
  const query = useMemo(() => {
    // property di header menang; tanpa property terpilih, property_id dari tautan drill-down tetap dipakai
    const q: Record<string, string | undefined> = { ...f.all, property_id: propertyId ?? (f.all.property_id || undefined) };
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
      // Tabel disederhanakan (29 Sep 2026, pola Tasks): nomor + judul, tenant (lokasi di tooltip), satu status + satu flag
      // terpenting (termasuk risiko/pelanggaran SLA), prioritas, assignee, dibuat. Tipe request, kategori, kanal, progres SLA &
      // path lokasi ada di halaman detail.
      { id: "title", header: "Request", meta: { mobile: "primary" }, cell: ({ row }) => <CellTitle code={row.original.request_number} title={row.original.title} /> },
      { id: "tenant", header: t("label.tenant"), meta: { mobile: "secondary" }, cell: ({ row }) => { const name = row.original.tenant_name ?? row.original.requester_name ?? "—"; const path = row.original.location.path_text; return <CellText max={170} title={path ? `${name} · ${path}` : name}>{name}</CellText>; } },
      { id: "status", header: t("label.status"), meta: { mobile: "status" }, cell: ({ row }) => <CellStatus objectType="service_request" status={row.original.status} item={row.original} /> },
      { id: "priority", header: t("label.priority"), meta: { mobile: "hidden" }, cell: ({ row }) => <PriorityBadge priority={row.original.priority} />, size: 100 },
      { id: "assignee", header: t("label.assignee"), meta: { mobile: "secondary" }, cell: ({ row }) => { const n = row.original.assignee.user_name ?? row.original.assignee.team_name; return <CellText max={140} muted={!n} className={n ? undefined : "italic"}>{n ?? "belum ditugaskan"}</CellText>; } },
      { id: "created", header: t("label.created"), meta: { mobile: "hidden" }, cell: ({ row }) => <span className="whitespace-nowrap text-sm"><RelativeTime value={row.original.created_at} /></span>, size: 110 },
      { id: "actions", header: "", cell: ({ row }) => <div className="flex justify-end gap-1" onClick={(e) => e.stopPropagation()}><TransitionActions objectType="service_request" item={row.original} compact onAssign={() => setAssign(row.original)} /></div> },
    ],
    [t],
  );
  return (
    <div>
      <PageHeader title={t("nav.service_requests")} actions={can("tenant.service_requests.create") && <span className="hidden md:inline-flex"><Button onClick={() => setCreateOpen(true)}><Icon name="add" size={16} /> {t("action.create_service_request")}</Button></span>}>
        <FilterBar
          spec={{
            status: Object.entries(statusMap.service_request).map(([value, d]) => ({ value, label: d.label_id })),
            priority: true,
            location: true,
            assignee: true,
            team: true,
            dateRange: true,
            type: (cats.data ?? []).map((c) => ({ value: c.code, label: c.name })),
            // PRD P1 v2 §27.2, §39: tipe request · status SLA
            extra: (
              <>
                <FilterSelect param="request_type" label={t("label.request_type")} options={Object.entries(requestTypeLabel).map(([value, label]) => ({ value, label }))} className="sm:w-44" />
                <FilterSelect param="sla_status" label="SLA" options={statusOptions("sla_status")} />
                <FilterSelect param="channel" label="Kanal" options={optionsOf(SR_CHANNELS)} />
              </>
            ),
            // PRD P1 v2 §38 Requests: All · New · In Progress · SLA Risk · Resolved · Closed (+ Open, Reopened untuk drill-down)
            presets: [
              { key: "all", label: t("preset.all"), params: {} },
              { key: "open", label: t("preset.open"), params: { open: "true" } },
              { key: "new", label: t("preset.new"), params: { status: "new" } },
              { key: "in_progress", label: t("preset.in_progress"), params: { status: "in_progress" } },
              { key: "sla_risk", label: t("preset.sla_risk"), params: { sla_risk: "true" } },
              { key: "resolved", label: t("preset.resolved"), params: { status: "resolved" } },
              { key: "closed", label: t("preset.closed"), params: { status: "closed" } },
              { key: "reopened", label: t("preset.reopened"), params: { reopened: "true" } },
              { key: "mine", label: t("label.mine"), params: { mine: "true" } },
            ],
          }}
          onExport={can("platform.exports.create") ? () => exp.request("service_requests", Object.fromEntries(Object.entries(query).filter(([, v]) => v) as [string, string][])) : undefined}
        />
      </PageHeader>
      <DrillDownNote params={f.all} onClear={(keys) => f.set(Object.fromEntries(keys.map((k) => [k, null])))} />
      <DataGrid columns={columns} rows={rows} rowId={(r) => r.id} onRowClick={(r) => `/operations/service-requests/${r.id}`} loading={list.isLoading} error={list.error} onRetry={() => list.refetch()} isFiltered={f.isFiltered} empty={{ icon: "support_agent", title: t("empty.service_requests"), description: t("empty.service_requests_desc"), action: can("tenant.service_requests.create") ? <Button icon="add" onClick={() => setCreateOpen(true)}>{t("action.create_service_request")}</Button> : undefined }} hasMore={list.hasNextPage} onLoadMore={() => list.fetchNextPage()} loadingMore={list.isFetchingNextPage} rowClassName={(r) => (r.flags.includes("sla_breach") ? "border-l-4 border-l-critical" : r.flags.includes("sla_risk") ? "border-l-4 border-l-warning" : undefined)} />
      {can("tenant.service_requests.create") && <Fab label={t("action.create")} aria-label={t("action.create_service_request")} onClick={() => setCreateOpen(true)} />}
      <CreateServiceRequestDialog open={createOpen} onOpenChange={setCreateOpen} />
      {assign && <AssignDialog objectType="service_request" id={assign.id} open onOpenChange={(o) => !o && setAssign(null)} current={assign.assignee} />}
    </div>
  );
}

/** Ringkasan parameter drill-down (KPI Tenant Relation) yang tidak punya kontrol filter sendiri. */
function DrillDownNote({ params, onClear }: { params: Record<string, string>; onClear: (keys: string[]) => void }) {
  const { can } = useAuth();
  const riId = params.recurring_issue_id || null;
  const ri = useOne<RecurringIssue>("recurring-issues", riId, { enabled: can("tenant_relation.recurring_issues.view") });
  const parts: { key: string; node: React.ReactNode }[] = [];
  const when = (v: string) => { const d = new Date(v); return Number.isNaN(d.getTime()) ? v : fmtDateTime(d); };
  if (params.resolved_from) parts.push({ key: "resolved_from", node: <>selesai sejak <b>{when(params.resolved_from)}</b></> });
  if (params.resolved_to) parts.push({ key: "resolved_to", node: <>selesai sampai <b>{when(params.resolved_to)}</b></> });
  if (params.reopened_from) parts.push({ key: "reopened_from", node: <>dibuka kembali sejak <b>{when(params.reopened_from)}</b></> });
  // kanal ganda (CSV) tidak dapat ditampilkan select tunggal → ringkas di sini
  if (params.channel?.includes(",")) parts.push({ key: "channel", node: <>kanal <b>{params.channel.split(",").map((c) => labelOf(SR_CHANNELS, c)).join(", ")}</b></> });
  if (riId) parts.push({
    key: "recurring_issue_id",
    node: ri.data ? <>isu berulang <Link to={`/tenant-relation/recurring-issues/${riId}`} className="font-semibold text-primary hover:underline">{ri.data.category_name ?? ri.data.category_code} · {ri.data.location_path}</Link></> : <>isu berulang tertentu</>,
  });
  if (!parts.length) return null;
  return (
    <div className="mb-3 flex flex-wrap items-center gap-x-2 gap-y-1 rounded-[var(--radius-md)] bg-info-soft px-3 py-2 text-sm text-info-text" role="status">
      <Icon name="filter_alt" size={16} aria-hidden />
      <span>Menampilkan permintaan:</span>
      {parts.map((p, i) => <span key={p.key}>{p.node}{i < parts.length - 1 ? " ·" : ""}</span>)}
      <button type="button" className="ml-auto text-xs font-semibold underline" onClick={() => onClear(parts.map((p) => p.key))}>Hapus filter ini</button>
    </div>
  );
}
