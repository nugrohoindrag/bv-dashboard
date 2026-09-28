// Daftar Task / Work Order (PRD §9, §10; DS §5.2): FilterBar + DataGrid + bulk assign + export.
// Dipakai ulang untuk Corrective Maintenance, Inspections, Cleaning, dst lewat prop fixedType.
// PRD P2 v2.1: parameter drill-down dashboard (date=today, from/to, result) diterjemahkan ke filter server (lib/drilldown).
import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { Icon } from "@buildingvision/ui";
import type { ColumnDef } from "@tanstack/react-table";
import { PageHeader } from "@/components/shell/AppShell";
import { Alert, Button } from "@/components/ui/primitives";
import { DataGrid, FilterBar, FilterSelect, useUrlFilters } from "@/components/bv/datagrid";
import { PriorityBadge, workTypeLabel } from "@/components/bv/badges";
import { CellLocation, CellStatus, CellTitle } from "@/components/bv/cells";
import { useList, useAction, resourceOf, useTaskCategories } from "@/api/hooks";
import { useAuth } from "@/lib/auth";
import { statusMap } from "@/lib/status-map";
import { statusOptions } from "@/lib/status";
import { fmtDateTime } from "@/lib/format";
import { cn } from "@/lib/utils";
import type { WorkItem } from "@/api/types";
import { AssignDialog, CreateWorkItemDialog, TASK_TYPES, TransitionActions, WO_TYPES, useExport } from "./dialogs";
import { EquipmentPicker, TeamPicker, UserPicker, VendorPicker } from "@/components/bv/pickers";
import { Dialog, DialogContent, DialogFooter, Field } from "@/components/ui/primitives";
import { useToast } from "@/components/bv/common";
import { Fab } from "@/components/bv/mobile";
import { WORK_ITEM_SORTS, safeSort } from "@/lib/sort";
import { workItemQuery } from "@/lib/drilldown";

export default function WorkItemListPage({ objectType, fixedType, title, housekeeping }: { objectType: "task" | "work_order"; fixedType?: string; title?: string; housekeeping?: boolean }) {
  const { t } = useTranslation();
  const { propertyId, can } = useAuth();
  const f = useUrlFilters();
  const resource = resourceOf(objectType);
  const drill = useMemo(() => workItemQuery(f.all, "completed"), [f.all]);
  const query = useMemo(() => {
    const q: Record<string, string | undefined> = { ...drill.query, property_id: propertyId ?? drill.query.property_id ?? undefined, sort: safeSort(f.all.sort, WORK_ITEM_SORTS) };
    if (fixedType) q.type = fixedType;
    if (housekeeping) q.source_type = "cleaning_task";
    delete q.cursor;
    return q;
  }, [drill.query, f.all.sort, propertyId, fixedType, housekeeping]);
  const list = useList<WorkItem>(resource, query);
  const rows = list.data?.pages.flatMap((p) => p.data) ?? [];
  const total = list.data?.pages[0]?.total;
  const [createOpen, setCreateOpen] = useState(false);
  const [assign, setAssign] = useState<WorkItem | null>(null);
  const [bulk, setBulk] = useState<string[] | null>(null);
  const exp = useExport();
  const statuses = Object.entries(statusMap[objectType]).map(([value, d]) => ({ value, label: d.label_id }));
  const canCreate = can(objectType === "task" ? "operations.tasks.create" : "operations.work_orders.create");
  const categories = useTaskCategories();
  const isTask = objectType === "task";

  const columns = useMemo<ColumnDef<WorkItem, unknown>[]>(
    () => [
      // Tabel disederhanakan (29 Sep 2026): nomor + judul (dipotong "…"), lokasi = nama lokasi terakhir, satu status + satu
      // flag terpenting, prioritas, assignee, jatuh tempo. Tipe/aset, kategori, SLA & path lokasi lengkap ada di halaman detail
      // (menu opsi → "Lihat detail").
      { id: "title", header: objectType === "task" ? "Task" : "Work Order", meta: { mobile: "primary" }, cell: ({ row }) => <CellTitle code={row.original.number} title={row.original.title} /> },
      { id: "location", header: t("label.location"), meta: { mobile: "secondary" }, cell: ({ row }) => <CellLocation path={row.original.location.path_text} max={130} /> },
      { id: "status", header: t("label.status"), meta: { mobile: "status" }, cell: ({ row }) => <CellStatus objectType={objectType} status={row.original.status} item={row.original} /> },
      { id: "priority", header: t("label.priority"), cell: ({ row }) => <PriorityBadge priority={row.original.priority} />, size: 100 },
      { id: "assignee", header: t("label.assignee"), meta: { mobile: "secondary" }, cell: ({ row }) => <span className={cn("block max-w-[110px] truncate text-sm", !row.original.assignee.user_name && !row.original.assignee.team_name && "italic text-muted-foreground")} title={row.original.assignee.user_name ?? row.original.assignee.team_name ?? undefined}>{row.original.assignee.user_name ?? row.original.assignee.team_name ?? "belum ditugaskan"}</span> },
      { id: "due", header: t("label.due"), meta: { mobile: "secondary" }, cell: ({ row }) => <span className={cn("tnum text-sm", row.original.is_overdue && "font-semibold text-critical-text")}>{fmtDateTime(row.original.due_at)}</span>, size: 150 },
      { id: "actions", header: "", cell: ({ row }) => <div className="flex justify-end gap-1" onClick={(e) => e.stopPropagation()}><TransitionActions objectType={objectType} item={row.original} compact onAssign={() => setAssign(row.original)} detailTo={`/operations/${resource}/${row.original.id}`} /></div> },
    ],
    [objectType, t, resource],
  );

  return (
    <div>
      <PageHeader
        title={title ?? (objectType === "task" ? t("nav.tasks") : t("nav.work_orders"))}
        subtitle={total !== undefined ? t("label.showing", { n: `${rows.length}/${total}` }) : undefined}
        actions={canCreate && <span className="hidden md:inline-flex"><Button onClick={() => setCreateOpen(true)}><Icon name="add" size={16} /> {objectType === "task" ? t("action.create_task") : t("action.create_work_order")}</Button></span>}
      >
        <FilterBar
          spec={{
            status: statuses,
            type: fixedType ? undefined : (isTask ? TASK_TYPES : WO_TYPES).map((value) => ({ value, label: workTypeLabel[value] ?? value })),
            priority: true,
            location: true,
            assignee: true,
            team: true,
            dateRange: true,
            // PRD P0 v2 §17.2: filter equipment (subtree aset) & vendor (WO); building/floor lewat Lokasi (subtree)
            extra: (
              <>
                {/* PRD P1 v2 §39: filter SLA · kategori (task) · eskalasi */}
                <FilterSelect param="sla_status" label="SLA" options={statusOptions("sla_status")} />
                {isTask && <FilterSelect param="category" label={t("label.category")} options={categories} />}
                <FilterSelect param="escalated" label={t("label.escalated")} options={[{ value: "true", label: "Ya" }, { value: "false", label: "Tidak" }]} className="sm:w-36" />
                {can("engineering.equipment.view") && <EquipmentPicker className="w-[calc(50%-4px)] sm:w-44" value={f.get("equipment_id") || null} onChange={(id) => f.set({ equipment_id: id })} placeholder="Equipment" />}
                {objectType === "work_order" && can("vendor.vendors.view") && <VendorPicker className="w-[calc(50%-4px)] sm:w-44" value={f.get("vendor_id") || null} onChange={(id) => f.set({ vendor_id: id })} placeholder="Vendor" />}
              </>
            ),
            // PRD P1 v2 §38: view standar (eksklusif, "Semua" = tanpa preset); drill-down dashboard memakai parameter yang sama
            presets: isTask
              ? [
                  { key: "all", label: t("preset.all"), params: {} },
                  { key: "mine", label: t("preset.my_tasks"), params: { mine: "true" } },
                  { key: "open", label: t("preset.open"), params: { open: "true" } },
                  { key: "due_today", label: t("preset.due_today"), params: { due_today: "true" } },
                  { key: "overdue", label: t("preset.overdue"), params: { overdue: "true" } },
                  { key: "sla_risk", label: t("preset.sla_risk"), params: { sla_risk: "true" } },
                  { key: "completed", label: t("preset.completed"), params: { status: "completed,closed" } },
                  { key: "today", label: "Jadwal hari ini", params: { scheduled_on: new Date().toISOString().slice(0, 10) + "T00:00:00Z" } },
                ]
              : [
                  { key: "all", label: t("preset.all"), params: {} },
                  { key: "open", label: t("preset.open"), params: { open: "true" } },
                  { key: "assigned", label: t("preset.assigned"), params: { status: "assigned" } },
                  { key: "in_progress", label: t("preset.in_progress"), params: { status: "in_progress" } },
                  { key: "completed", label: t("preset.completed"), params: { status: "completed,closed" } },
                  { key: "overdue", label: t("preset.overdue"), params: { overdue: "true" } },
                  { key: "sla_risk", label: t("preset.sla_risk"), params: { sla_risk: "true" } },
                  { key: "draft", label: t("preset.draft"), params: { status: "draft" } },
                  { key: "mine", label: t("label.mine"), params: { mine: "true" } },
                ],
          }}
          onExport={can("platform.exports.create") ? () => exp.request(objectType === "task" ? "tasks" : "work_orders", Object.fromEntries(Object.entries(query).filter(([, v]) => v) as [string, string][])) : undefined}
        />
      </PageHeader>
      {drill.notes.map((n) => <Alert key={n} variant="info" className="mb-3">{n}</Alert>)}
      <DataGrid
        columns={columns}
        rows={rows}
        rowId={(r) => r.id}
        onRowClick={(r) => `/operations/${resource}/${r.id}`}
        loading={list.isLoading} error={list.error} onRetry={() => list.refetch()}
        isFiltered={f.isFiltered}
        empty={{ icon: objectType === "task" ? "task_alt" : "construction", title: objectType === "task" ? t("empty.tasks") : t("empty.work_orders"), description: objectType === "task" ? t("empty.tasks_desc") : t("empty.work_orders_desc"), action: canCreate ? <Button onClick={() => setCreateOpen(true)}><Icon name="add" size={16} /> {objectType === "task" ? t("action.create_task") : t("action.create_work_order")}</Button> : undefined }}
        hasMore={list.hasNextPage}
        onLoadMore={() => list.fetchNextPage()}
        loadingMore={list.isFetchingNextPage}
        selectable={can(objectType === "task" ? "operations.tasks.assign" : "operations.work_orders.assign")}
        bulkActions={(ids, clear) => (
          <Button size="sm" onClick={() => { setBulk(ids); clear(); }}>{t("action.assign")} ({ids.length})</Button>
        )}
        rowClassName={(r) => (r.is_overdue ? "border-l-4 border-l-critical" : r.flags.includes("sla_risk") ? "border-l-4 border-l-warning" : undefined)}
      />
      {canCreate && <Fab label={t("action.create")} aria-label={objectType === "task" ? t("action.create_task") : t("action.create_work_order")} onClick={() => setCreateOpen(true)} />}
      <CreateWorkItemDialog objectType={objectType} open={createOpen} onOpenChange={setCreateOpen} defaults={fixedType ? { type: fixedType.split(",")[0] } : undefined} />
      {assign && <AssignDialog objectType={objectType} id={assign.id} open onOpenChange={(o) => !o && setAssign(null)} current={assign.assignee} />}
      {bulk && <BulkAssignDialog resource={resource} ids={bulk} onClose={() => setBulk(null)} />}
    </div>
  );
}

function BulkAssignDialog({ resource, ids, onClose }: { resource: string; ids: string[]; onClose: () => void }) {
  const { t } = useTranslation();
  const { propertyId } = useAuth();
  const toast = useToast();
  const [teamId, setTeamId] = useState<string | null>(null);
  const [userId, setUserId] = useState<string | null>(null);
  const bulk = useAction<{ ids: string[]; assignee_team_id: string | null; assignee_user_id: string | null }>(() => `${resource}/bulk-assign`);
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent title={`${t("action.assign")} ${ids.length} item`}>
        <div className="space-y-4">
          <Field label={t("label.team")}><TeamPicker propertyId={propertyId} value={teamId} onChange={(v) => { setTeamId(v); setUserId(null); }} /></Field>
          <Field label={t("label.assignee")}><UserPicker propertyId={propertyId} teamId={teamId} value={userId} onChange={setUserId} /></Field>
        </div>
        <DialogFooter>
          <Button variant="secondary" onClick={onClose}>{t("action.discard")}</Button>
          <Button loading={bulk.isPending} disabled={!teamId && !userId} onClick={() => bulk.mutateAsync({ ids, assignee_team_id: teamId, assignee_user_id: userId }).then(() => { toast.action("assigned", `${ids.length} item`); onClose(); }).catch(toast.error)}>{t("action.assign")}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
