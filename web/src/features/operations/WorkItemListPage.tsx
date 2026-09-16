// Daftar Task / Work Order (PRD §9, §10; DS §5.2): FilterBar + DataGrid + bulk assign + export.
// Dipakai ulang untuk Corrective Maintenance, Inspections, Cleaning, dst lewat prop fixedType.
import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { Icon } from "@buildingvision/ui";
import type { ColumnDef } from "@tanstack/react-table";
import { PageHeader } from "@/components/shell/AppShell";
import { Button } from "@/components/ui/primitives";
import { DataGrid, FilterBar, useUrlFilters } from "@/components/bv/datagrid";
import { FlagBadges, PriorityBadge, StatusBadge, workTypeLabel } from "@/components/bv/badges";
import { LocationPath } from "@/components/bv/common";
import { useList, useAction, resourceOf } from "@/api/hooks";
import { useAuth } from "@/lib/auth";
import { statusMap } from "@/lib/status-map";
import { fmtDateTime } from "@/lib/format";
import { cn } from "@/lib/utils";
import type { WorkItem } from "@/api/types";
import { AssignDialog, CreateWorkItemDialog, TransitionActions, useExport } from "./dialogs";
import { TeamPicker, UserPicker } from "@/components/bv/pickers";
import { Dialog, DialogContent, DialogFooter, Field } from "@/components/ui/primitives";
import { useToast } from "@/components/bv/common";

export default function WorkItemListPage({ objectType, fixedType, title, housekeeping }: { objectType: "task" | "work_order"; fixedType?: string; title?: string; housekeeping?: boolean }) {
  const { t } = useTranslation();
  const { propertyId, can } = useAuth();
  const f = useUrlFilters();
  const resource = resourceOf(objectType);
  const query = useMemo(() => {
    const q: Record<string, string | undefined> = { ...f.all, property_id: propertyId ?? undefined };
    if (fixedType) q.type = fixedType;
    if (housekeeping) q.source_type = "cleaning_task";
    delete q.cursor;
    return q;
  }, [f.all, propertyId, fixedType, housekeeping]);
  const list = useList<WorkItem>(resource, query);
  const rows = list.data?.pages.flatMap((p) => p.data) ?? [];
  const total = list.data?.pages[0]?.total;
  const [createOpen, setCreateOpen] = useState(false);
  const [assign, setAssign] = useState<WorkItem | null>(null);
  const [bulk, setBulk] = useState<string[] | null>(null);
  const exp = useExport();
  const statuses = Object.entries(statusMap[objectType]).map(([value, d]) => ({ value, label: d.label_id }));
  const canCreate = can(objectType === "task" ? "operations.tasks.create" : "operations.work_orders.create");

  const columns = useMemo<ColumnDef<WorkItem, unknown>[]>(
    () => [
      { id: "number", header: "ID", cell: ({ row }) => <span className="font-mono text-[13px] font-semibold">{row.original.number}</span>, size: 150 },
      { id: "title", header: t("label.title"), cell: ({ row }) => <div className="min-w-0"><div className="truncate font-medium">{row.original.title}</div><div className="text-xs text-muted-foreground">{workTypeLabel[row.original.type] ?? row.original.type}{row.original.asset.asset_code ? ` · ${row.original.asset.asset_code}` : ""}</div></div> },
      { id: "location", header: t("label.location"), cell: ({ row }) => <LocationPath pathText={row.original.location.path_text} className="max-w-[260px]" /> },
      { id: "status", header: t("label.status"), cell: ({ row }) => <div className="flex flex-wrap gap-1"><StatusBadge objectType={objectType} status={row.original.status} /><FlagBadges flags={row.original.flags} /></div> },
      { id: "priority", header: t("label.priority"), cell: ({ row }) => <PriorityBadge priority={row.original.priority} />, size: 100 },
      { id: "assignee", header: t("label.assignee"), cell: ({ row }) => <span className={cn(!row.original.assignee.user_name && !row.original.assignee.team_name && "italic text-muted-foreground")}>{row.original.assignee.user_name ?? row.original.assignee.team_name ?? "belum ditugaskan"}</span> },
      { id: "due", header: t("label.due"), cell: ({ row }) => <span className={cn("tnum", row.original.is_overdue && "font-semibold text-critical-text")}>{fmtDateTime(row.original.due_at)}</span>, size: 150 },
      { id: "actions", header: "", cell: ({ row }) => <div className="flex justify-end gap-1" onClick={(e) => e.stopPropagation()}><TransitionActions objectType={objectType} item={row.original} compact onAssign={() => setAssign(row.original)} /></div> },
    ],
    [objectType, t],
  );

  return (
    <div>
      <PageHeader
        title={title ?? (objectType === "task" ? t("nav.tasks") : t("nav.work_orders"))}
        subtitle={total !== undefined ? t("label.showing", { n: `${rows.length}/${total}` }) : undefined}
        actions={canCreate && <Button onClick={() => setCreateOpen(true)}><Icon name="add" size={16} /> {objectType === "task" ? t("action.create_task") : t("action.create_work_order")}</Button>}
      >
        <FilterBar
          spec={{
            status: statuses,
            type: fixedType ? undefined : Object.entries(workTypeLabel).filter(([k]) => (objectType === "task" ? ["general", "inspection", "patrol", "cleaning", "routine_maintenance"] : ["maintenance", "corrective", "repair", "service", "general"]).includes(k)).map(([value, label]) => ({ value, label })),
            priority: true,
            location: true,
            assignee: true,
            team: true,
            dateRange: true,
            presets: [
              { key: "open", label: "Open", params: { open: "true" } },
              { key: "overdue", label: t("label.overdue"), params: { overdue: "true" } },
              { key: "sla_risk", label: t("label.sla_risk"), params: { sla_risk: "true" } },
              { key: "mine", label: t("label.mine"), params: { mine: "true" } },
              { key: "today", label: t("label.today"), params: { scheduled_on: new Date().toISOString().slice(0, 10) + "T00:00:00Z" } },
            ],
          }}
          onExport={can("platform.exports.create") ? () => exp.request(objectType === "task" ? "tasks" : "work_orders", Object.fromEntries(Object.entries(query).filter(([, v]) => v) as [string, string][])) : undefined}
        />
      </PageHeader>
      <DataGrid
        columns={columns}
        rows={rows}
        rowId={(r) => r.id}
        onRowClick={(r) => `/operations/${resource}/${r.id}`}
        loading={list.isLoading}
        isFiltered={f.isFiltered}
        empty={{ message: objectType === "task" ? t("empty.tasks") : t("empty.work_orders"), cta: canCreate ? <Button onClick={() => setCreateOpen(true)}><Icon name="add" size={16} /> {objectType === "task" ? t("action.create_task") : t("action.create_work_order")}</Button> : undefined }}
        hasMore={list.hasNextPage}
        onLoadMore={() => list.fetchNextPage()}
        loadingMore={list.isFetchingNextPage}
        selectable={can(objectType === "task" ? "operations.tasks.assign" : "operations.work_orders.assign")}
        bulkActions={(ids, clear) => (
          <Button size="sm" onClick={() => { setBulk(ids); clear(); }}>{t("action.assign")} ({ids.length})</Button>
        )}
        rowClassName={(r) => (r.is_overdue ? "border-l-4 border-l-critical" : r.flags.includes("sla_risk") ? "border-l-4 border-l-warning" : undefined)}
      />
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
          <Button loading={bulk.isPending} disabled={!teamId && !userId} onClick={() => bulk.mutateAsync({ ids, assignee_team_id: teamId, assignee_user_id: userId }).then(() => { toast.success(`${ids.length} item ditugaskan`); onClose(); }).catch(toast.error)}>{t("action.assign")}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
