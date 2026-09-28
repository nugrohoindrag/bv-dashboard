// Receivables › Penagihan (PRD P4 v2.1 P4-COL-03..04): daftar kerja penagihan (prioritas dihitung server dari umur tunggakan,
// janji ingkar/jatuh tempo, tindak lanjut, belum pernah dihubungi) dengan aksi catat kontak/janji bayar, pengingat WhatsApp
// manual (click-to-chat; email di-hold), dan statement; serta riwayat log penagihan & janji bayar. Deep link notifikasi janji
// ingkar: /billing/collections?log=<id>; drill-down dashboard: /billing/collections?party=<tenant_id|unit_id>.
import { useEffect, useMemo, useState } from "react";
import { useNavigate, useSearchParams } from "react-router-dom";
import type { ColumnDef } from "@tanstack/react-table";
import { FilterChip } from "@buildingvision/ui/bv";
import { Alert, Badge, Button, SearchInput } from "@/components/ui/primitives";
import { DataGrid } from "@/components/bv/datagrid";
import { RelativeTime } from "@/components/bv/common";
import { StatusBadge } from "@/components/bv/badges";
import { CellText } from "@/components/bv/cells";
import { WhatsAppDialog } from "@/components/bv/WhatsAppButton";
import { useAll, useList } from "@/api/hooks";
import { useAuth } from "@/lib/auth";
import { fmtMoney, fmtNumber } from "@/lib/format";
import { PropertySelect, StatTile } from "@/features/finance/fin-ui";
import { fmtDay, fmtMoneyTile, usePropertyParam } from "@/features/finance/fin-utils";
import { CollectionLogDialog, CollectionLogDrawer } from "./receivables-log-dialog";
import { PRIORITY, PROMISE_FILTERS, agingInvoicesLink, channelLabel, filterWorklist, outcomeLabel, partyLabel, statementLink, worklistSummary, type CollectionLog, type WorkItem } from "./receivables-model";

export function CollectionsTab() {
  const { can } = useAuth();
  const [sp, setSp] = useSearchParams();
  const [pid, setPid] = usePropertyParam();
  const logId = sp.get("log");
  const view = logId || sp.get("view") === "logs" ? "logs" : "worklist";
  const [create, setCreate] = useState(false);
  const setView = (v: "worklist" | "logs") => {
    const n = new URLSearchParams(sp);
    if (v === "logs") n.set("view", "logs");
    else n.delete("view");
    n.delete("log");
    setSp(n, { replace: true });
  };
  const canManage = can("billing.collections.manage", pid ?? undefined);
  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center gap-2">
        <PropertySelect value={pid} onChange={setPid} allowAll />
        <span className="flex flex-wrap gap-1.5">
          <FilterChip selected={view === "worklist"} onClick={() => setView("worklist")}>Daftar kerja</FilterChip>
          <FilterChip selected={view === "logs"} onClick={() => setView("logs")}>Log & janji bayar</FilterChip>
        </span>
        {canManage && <Button icon="add_call" className="ml-auto" onClick={() => setCreate(true)}>Catat kontak</Button>}
      </div>
      {view === "worklist" ? <Worklist pid={pid} /> : <LogList pid={logId && !sp.has("property_id") ? null : pid} logId={logId} />}
      {create && <CollectionLogDialog propertyId={pid} onClose={() => setCreate(false)} />}
    </div>
  );
}

// ---------- daftar kerja ----------
function Worklist({ pid }: { pid: string | null }) {
  const nav = useNavigate();
  const { can } = useAuth();
  const [sp, setSp] = useSearchParams();
  const party = sp.get("party") ?? "";
  const priority = sp.get("priority") ?? "";
  const [q, setQ] = useState("");
  const list = useAll<WorkItem>("billing/collections/worklist", { property_id: pid ?? undefined });
  const all = useMemo(() => list.data ?? [], [list.data]);
  const items = useMemo(() => filterWorklist(all, { priority, q, party }), [all, priority, q, party]);
  const sum = worklistSummary(all);
  const [logFor, setLogFor] = useState<{ row: WorkItem; channel?: string; outcome?: string; title?: string } | null>(null);
  const [waFor, setWaFor] = useState<WorkItem | null>(null);
  const setParam = (k: string, v: string | null) => {
    const n = new URLSearchParams(sp);
    if (v) n.set(k, v);
    else n.delete(k);
    setSp(n, { replace: true });
  };
  const partyName = party ? all.find((w) => w.tenant_id === party || w.unit_location_id === party)?.label : null;
  const columns = useMemo<ColumnDef<WorkItem, unknown>[]>(() => [
    // Tabel disederhanakan (29 Sep 2026, pola Tasks): satu baris per sel. Alasan prioritas di tooltip badge prioritas; telepon,
    // jumlah invoice overdue, total piutang, hasil kontak & nominal janji di tooltip / drawer log.
    { id: "priority", header: "Prioritas", size: 100, meta: { mobile: "status" }, cell: ({ row: { original: w } }) => <span className="inline-flex" title={w.reasons.length ? w.reasons.join(" · ") : undefined}><Badge tone={PRIORITY[w.priority].tone}>{PRIORITY[w.priority].label}</Badge></span> },
    {
      id: "party", header: "Tenant / unit", meta: { mobile: "primary" },
      cell: ({ row: { original: w } }) => (
        <CellText max={240} className="font-medium" title={[w.label, w.kind === "unit" ? "Unit tanpa tenant" : null, w.contact_phone, ...w.reasons].filter(Boolean).join(" · ") || undefined}>{w.label || "—"}</CellText>
      ),
    },
    ...(pid ? [] : [{ id: "property", header: "Property", meta: { mobile: "hidden" as const }, cell: ({ row: { original: w } }: { row: { original: WorkItem } }) => <CellText max={160}>{w.property_name}</CellText> }]),
    {
      id: "overdue", header: "Tunggakan", size: 150, meta: { mobile: "secondary" },
      cell: ({ row: { original: w } }) => (
        <div className="tnum whitespace-nowrap text-right font-semibold" title={`${fmtNumber(w.overdue_count)} invoice overdue${w.outstanding_amount > w.overdue_amount ? ` · total ${fmtMoney(w.outstanding_amount)}` : ""}`}>{fmtMoney(w.overdue_amount)}</div>
      ),
    },
    { id: "oldest", header: "Tertua", size: 90, meta: { mobile: "secondary" }, cell: ({ row: { original: w } }) => (w.oldest_days_overdue > 0 ? <span className="tnum whitespace-nowrap">{w.oldest_days_overdue} hari</span> : <span className="text-on-surface-variant">—</span>) },
    {
      id: "contact", header: "Kontak terakhir", size: 140, meta: { mobile: "secondary" },
      cell: ({ row: { original: w } }) => (w.last_contact_at
        ? <span className="whitespace-nowrap text-sm" title={outcomeLabel(w.last_outcome)}><RelativeTime value={w.last_contact_at} /></span>
        : <span className="whitespace-nowrap text-sm text-on-surface-variant">Belum pernah</span>),
    },
    {
      id: "promise", header: "Janji bayar / tindak lanjut", size: 190, meta: { mobile: "hidden" },
      cell: ({ row: { original: w } }) => {
        const tip = [w.promise_amount ? `Janji ${fmtMoney(w.promise_amount)}` : null, w.follow_up_on ? `Tindak lanjut ${fmtDay(w.follow_up_on)}` : null].filter(Boolean).join(" · ") || undefined;
        if (w.promise_date) return <div className="flex items-center gap-1 whitespace-nowrap text-sm" title={tip}><span className="tnum">{fmtDay(w.promise_date)}</span>{w.promise_status && <StatusBadge objectType="collection_promise" status={w.promise_status} />}</div>;
        if (w.follow_up_on) return <span className="whitespace-nowrap text-sm text-on-surface-variant">Tindak lanjut {fmtDay(w.follow_up_on)}</span>;
        return <span className="text-on-surface-variant">—</span>;
      },
    },
    {
      id: "actions", header: "", size: 190,
      cell: ({ row: { original: w } }) => {
        const manage = can("billing.collections.manage", w.property_id);
        return (
          <span className="inline-flex flex-wrap justify-end gap-1" onClick={(e) => e.stopPropagation()}>
            {manage && <Button size="sm" variant="secondary" icon="add_call" onClick={() => setLogFor({ row: w })}>Catat</Button>}
            {w.tenant_id && <Button size="sm" variant="ghost" icon="chat" onClick={() => setWaFor(w)} aria-label={`Kirim pengingat WhatsApp ke ${w.label}`}>WA</Button>}
          </span>
        );
      },
    },
  ], [can, pid, setLogFor, setWaFor]);
  return (
    <div className="space-y-4">
      <section aria-label="Ringkasan penagihan" className="grid grid-cols-2 gap-3 md:grid-cols-5">
        <StatTile label="Prioritas tinggi" value={fmtNumber(sum.high)} sub="pihak" tone={sum.high > 0 ? "error" : undefined} />
        <StatTile label="Prioritas sedang" value={fmtNumber(sum.medium)} sub="pihak" tone={sum.medium > 0 ? "warning" : undefined} />
        <StatTile label="Prioritas rendah" value={fmtNumber(sum.low)} sub="pihak" />
        <StatTile label="Total tunggakan" value={fmtMoneyTile(sum.overdue)} title={fmtMoney(sum.overdue)} sub="invoice overdue" to={agingInvoicesLink(null, "open", pid)} />
        <StatTile label="Janji bayar terbuka" value={fmtNumber(sum.promisesDue)} sub="pihak dengan janji menunggu" />
      </section>
      <div className="flex flex-wrap items-center gap-2">
        <SearchInput className="w-full sm:w-64" placeholder="Cari tenant / unit…" value={q} onChange={(e) => setQ(e.target.value)} aria-label="Cari tenant atau unit" />
        <span className="flex flex-wrap gap-1.5">
          <FilterChip selected={!priority} onClick={() => setParam("priority", null)}>Semua</FilterChip>
          {(["high", "medium", "low"] as const).map((p) => <FilterChip key={p} selected={priority === p} onClick={() => setParam("priority", priority === p ? null : p)}>{PRIORITY[p].label}</FilterChip>)}
        </span>
      </div>
      {party && (
        <Alert variant="info" action={<Button size="sm" variant="ghost" onClick={() => setParam("party", null)}>Tampilkan semua</Button>}>
          Difilter ke satu pihak{partyName ? `: ${partyName}` : ""} (dari dashboard / aging).
        </Alert>
      )}
      <DataGrid
        columns={columns}
        rows={items}
        rowId={(r) => `${r.property_id}:${r.tenant_id ?? r.unit_location_id}`}
        loading={list.isLoading}
        error={list.error}
        onRetry={() => list.refetch()}
        isFiltered={!!priority || !!q || !!party}
        empty={{ icon: "task_alt", title: "Tidak ada tunggakan untuk ditagih", description: "Daftar kerja berisi pihak dengan invoice overdue atau janji bayar terbuka." }}
        rowActions={(w) => [
          { label: "Statement of account", icon: "receipt_long", onSelect: () => nav(statementLink(w.property_id, w)) },
          { label: "Invoice terbuka", icon: "request_quote", onSelect: () => nav(agingInvoicesLink(w, "open", w.property_id)) },
          { label: "Riwayat log", icon: "history", onSelect: () => nav(`/billing/collections?view=logs&property_id=${w.property_id}&${w.tenant_id ? `tenant_id=${w.tenant_id}` : `unit_location_id=${w.unit_location_id}`}`) },
          ...(can("billing.collections.manage", w.property_id) ? [{ label: "Catat janji bayar", icon: "event_available", onSelect: () => setLogFor({ row: w, outcome: "promise_to_pay", title: "Catat janji bayar" }) }] : []),
        ]}
      />
      {logFor && (
        <CollectionLogDialog
          propertyId={logFor.row.property_id}
          party={{ tenant_id: logFor.row.tenant_id, unit_location_id: logFor.row.unit_location_id, label: logFor.row.label }}
          defaults={{ channel: logFor.channel, outcome: logFor.outcome }}
          title={logFor.title}
          onClose={() => setLogFor(null)}
        />
      )}
      {waFor?.tenant_id && (
        <WhatsAppDialog
          context="collection_reminder"
          objectType="tenant"
          objectId={waFor.tenant_id}
          onClose={() => setWaFor(null)}
          onSent={() => { const w = waFor; if (can("billing.collections.manage", w.property_id)) setLogFor({ row: w, channel: "whatsapp", outcome: "contacted", title: "Catat hasil pengingat WhatsApp" }); }}
        />
      )}
    </div>
  );
}

// ---------- log penagihan ----------
function LogList({ pid, logId }: { pid: string | null; logId: string | null }) {
  const [sp, setSp] = useSearchParams();
  const fkey = sp.get("promise") ?? "all";
  const filter = logId ? PROMISE_FILTERS[0] : PROMISE_FILTERS.find((f) => f.key === fkey) ?? PROMISE_FILTERS[0];
  // filter satu pihak (dari daftar kerja "Riwayat log")
  const tenantId = logId ? null : sp.get("tenant_id");
  const unitId = logId || tenantId ? null : sp.get("unit_location_id");
  const list = useList<CollectionLog>("billing/collection-logs", { property_id: pid ?? undefined, tenant_id: tenantId ?? undefined, unit_location_id: unitId ?? undefined, ...filter.params }, { limit: logId ? 100 : 50 });
  const rows = useMemo(() => list.data?.pages.flatMap((p) => p.data) ?? [], [list.data]);
  const selected = logId ? rows.find((r) => r.id === logId) : undefined;
  const pages = list.data?.pages.length ?? 0;
  const { hasNextPage, isFetchingNextPage, fetchNextPage } = list;
  // log dari deep link belum ada di halaman yang dimuat → muat halaman berikutnya (maks. 10 halaman)
  useEffect(() => {
    if (logId && !selected && hasNextPage && !isFetchingNextPage && pages > 0 && pages < 10) void fetchNextPage();
  }, [logId, selected, hasNextPage, isFetchingNextPage, pages, fetchNextPage]);
  const setParam = (patch: Record<string, string | null>) => {
    const n = new URLSearchParams(sp);
    for (const [k, v] of Object.entries(patch)) {
      if (v) n.set(k, v);
      else n.delete(k);
    }
    setSp(n, { replace: true });
  };
  const columns = useMemo<ColumnDef<CollectionLog, unknown>[]>(() => [
    // Tabel disederhanakan (29 Sep 2026): satu baris per sel; pencatat, unit, narahubung & nominal janji di tooltip / drawer log.
    { id: "created_at", header: "Dicatat", size: 120, meta: { mobile: "secondary" }, cell: ({ row: { original: l } }) => <span className="whitespace-nowrap text-sm" title={l.created_by_name ?? undefined}><RelativeTime value={l.created_at} /></span> },
    { id: "party", header: "Tenant / unit", meta: { mobile: "primary" }, cell: ({ row: { original: l } }) => <CellText max={220} className="font-medium" title={l.tenant_name && l.unit_label ? `${partyLabel(l)} · ${l.unit_label}` : partyLabel(l)}>{partyLabel(l)}</CellText> },
    { id: "channel", header: "Kanal · hasil", size: 190, meta: { mobile: "secondary" }, cell: ({ row: { original: l } }) => <CellText max={190} title={`${channelLabel(l.channel)} · ${outcomeLabel(l.outcome)}${l.contact_person ? ` · ${l.contact_person}` : ""}`}>{`${channelLabel(l.channel)} · ${outcomeLabel(l.outcome)}`}</CellText> },
    {
      id: "status", header: "Janji bayar", size: 190, meta: { mobile: "status" },
      cell: ({ row: { original: l } }) => (l.promise_status ? <div className="flex items-center gap-1 whitespace-nowrap" title={l.promise_amount ? fmtMoney(l.promise_amount) : undefined}><StatusBadge objectType="collection_promise" status={l.promise_status} /><span className="text-xs tnum text-on-surface-variant">{fmtDay(l.promise_date)}</span></div> : <span className="text-on-surface-variant">—</span>),
    },
    { id: "follow_up", header: "Tindak lanjut", size: 120, meta: { mobile: "hidden" }, cell: ({ row }) => <span className="whitespace-nowrap text-sm tnum">{row.original.follow_up_on ? fmtDay(row.original.follow_up_on) : "—"}</span> },
    { id: "notes", header: "Catatan", meta: { mobile: "hidden" }, cell: ({ row }) => <CellText max={240} muted>{row.original.notes ?? "—"}</CellText> },
  ], []);
  const notFound = !!logId && !selected && !list.isLoading && !list.isFetching && !hasNextPage && !isFetchingNextPage;
  return (
    <div className="space-y-3">
      {!logId && (
        <span className="flex flex-wrap items-center gap-1.5">
          {PROMISE_FILTERS.map((f) => <FilterChip key={f.key} selected={filter.key === f.key} onClick={() => setParam({ promise: f.key === "all" ? null : f.key })}>{f.label}</FilterChip>)}
          {(tenantId || unitId) && <Button size="sm" variant="secondary" icon="close" onClick={() => setParam({ tenant_id: null, unit_location_id: null })}>{rows[0] ? partyLabel(rows[0]) : "Satu pihak"}</Button>}
        </span>
      )}
      {notFound && <Alert variant="warning" title="Log tidak ditemukan" action={<Button size="sm" variant="ghost" onClick={() => setParam({ log: null })}>Tutup</Button>}>Log penagihan dari tautan tidak ada pada property/scope ini atau sudah tidak tersedia.</Alert>}
      <DataGrid
        columns={columns}
        rows={rows}
        rowId={(r) => r.id}
        onRowClick={(r) => { setParam({ log: r.id }); }}
        loading={list.isLoading}
        error={list.error}
        onRetry={() => list.refetch()}
        isFiltered={filter.key !== "all" || !!tenantId || !!unitId}
        empty={{ icon: "history", title: "Belum ada log penagihan", description: "Catat setiap kontak penagihan (telepon, WhatsApp, kunjungan, surat) beserta janji bayar dari daftar kerja." }}
        hasMore={list.hasNextPage}
        onLoadMore={() => list.fetchNextPage()}
        loadingMore={list.isFetchingNextPage}
        rowClassName={(r) => (r.id === logId ? "bg-primary-soft" : r.promise_status === "broken" ? "border-l-4 border-l-critical" : undefined)}
      />
      {selected && <CollectionLogDrawer key={selected.id} log={selected} onClose={() => setParam({ log: null })} onOpenLog={(id) => setParam({ log: id })} />}
      <p className="text-xs text-on-surface-variant">Janji terbuka dinilai otomatis setiap 15 menit: ditepati bila pembayaran masuk, ingkar bila lewat tanggal (notifikasi ke Finance).</p>
    </div>
  );
}
