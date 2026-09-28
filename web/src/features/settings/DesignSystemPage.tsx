// Katalog Design System (PRD P0 §20, §28 "Design system foundation tersedia", §29 "core components 100%"):
// token warna (light/dark), skala tipografi, Status System umum + flag + priority/severity, dan setiap komponen §20.3
// dengan contoh hidup + cuplikan pemakaian. Semua contoh memakai komponen produksi yang sama (bukan tiruan).
import { useMemo, useState } from "react";
import { Link } from "react-router-dom";
import type { ColumnDef } from "@tanstack/react-table";
import { Icon } from "@buildingvision/ui";
import { FilterChip, MetricCard } from "@buildingvision/ui/bv";
import { PageHeader } from "@/components/shell/AppShell";
import {
  Alert, Badge, Button, Card, CardContent, CardHeader, CardSubtitle, CardTitle, DatePicker, Dialog, DialogContent, DialogFooter, Drawer, Field, Input,
  NativeSelect, SearchInput, Tabs, TabsContent, TabsList, TabsTrigger, Textarea,
} from "@/components/ui/primitives";
import { CommonStatusBadge, FlagBadge, PriorityBadge, SeverityBadge, StatusBadge, type Flag } from "@/components/bv/badges";
import { DataGrid, FilterBar } from "@/components/bv/datagrid";
import { useToast } from "@/components/bv/common";
import { CardSkeleton, EmptyState, ErrorState, ForbiddenState, FormSkeleton, KpiSkeleton, PageSkeleton, TableSkeleton } from "@/components/bv/states";
import { Fab, MobileActionBar } from "@/components/bv/mobile";
import { TodayCounter } from "@/components/bv/cards";
import { NotificationItem } from "@/components/shell/NotificationInbox";
import { statusMap } from "@/lib/status-map";
import { STATUS_CATEGORIES, statusCategory } from "@/lib/status-category";
import type { Notification } from "@/api/types";
import { cn } from "@/lib/utils";
import { WeekdayPicker } from "@/components/bv/pickers";
import { KpiCard } from "@/features/dashboards/KpiCard";
import { HealthBadge } from "@/features/assets/AssetP2Tabs";
import { ShiftDot, ValidityBadge } from "@/features/workforce/components";
import { SHIFT_TONES } from "@/features/workforce/workforce";

const SECTIONS = [
  ["colors", "Warna"], ["typography", "Tipografi"], ["status", "Status System"], ["button", "Button"], ["inputs", "Input · Select · Date · Search"],
  ["filter", "Filter"], ["table", "Table"], ["card", "Card & Dashboard card"], ["overlay", "Modal & Drawer"], ["tabs", "Tabs"], ["badge", "Badge & Status indicator"],
  ["states", "Empty · Loading · Error"], ["toast", "Toast"], ["notification", "Notification"], ["mobile", "Mobile action"], ["workforce", "Dashboard & Workforce (P2)"],
] as const;

// ---------- kerangka ----------
function Section({ id, title, desc, children }: { id: string; title: string; desc?: string; children: React.ReactNode }) {
  return (
    <section id={id} className="scroll-mt-4 space-y-3">
      <div>
        <h2 className="text-h2 font-bold text-on-surface">{title}</h2>
        {desc && <p className="text-sm text-on-surface-variant">{desc}</p>}
      </div>
      {children}
    </section>
  );
}
function Example({ title, code, children, className }: { title?: string; code?: string; children: React.ReactNode; className?: string }) {
  return (
    <Card>
      {title && <CardHeader><CardTitle className="text-body">{title}</CardTitle></CardHeader>}
      <CardContent className={cn(!title && "pt-4", "space-y-3")}>
        <div className={cn("flex flex-wrap items-center gap-2", className)}>{children}</div>
        {code && <pre className="overflow-x-auto rounded-[var(--radius-md)] bg-surface-container px-3 py-2 font-mono text-caption text-on-surface-variant"><code>{code}</code></pre>}
      </CardContent>
    </Card>
  );
}

// ---------- token warna (dibaca dari palette.css di kedua tema) ----------
const COLOR_TOKENS: [string, string][] = [
  ["primary", "Primary"], ["on-primary", "On primary"], ["primary-container", "Primary container"], ["secondary", "Secondary"], ["secondary-container", "Secondary container"],
  ["background", "Background"], ["surface", "Surface"], ["surface-container", "Surface container"], ["surface-container-high", "Surface container high"],
  ["on-surface", "Text · primary"], ["on-surface-variant", "Text · secondary"], ["outline-variant", "Text · disabled / ikon"], ["border", "Border / divider"], ["outline", "Border · kontrol"],
  ["success", "Success"], ["success-container", "Success container"], ["warning", "Warning"], ["warning-container", "Warning container"],
  ["error", "Error (= critical)"], ["error-container", "Error container"], ["info", "Information"], ["info-container", "Info container"],
];
/** Nilai token di kedua tema: atribut tema ditukar sesaat secara sinkron lalu dikembalikan (tidak ada frame di antaranya). */
function readThemeValues(): Record<"light" | "dark", Record<string, string>> {
  if (typeof document === "undefined") return { light: {}, dark: {} };
  const root = document.documentElement;
  const prev = root.getAttribute("data-theme");
  const read = () => Object.fromEntries(COLOR_TOKENS.map(([k]) => [k, getComputedStyle(root).getPropertyValue(`--md-sys-color-${k}`).trim()]));
  root.setAttribute("data-theme", "light");
  const light = read();
  root.setAttribute("data-theme", "dark");
  const dark = read();
  if (prev) root.setAttribute("data-theme", prev);
  else root.removeAttribute("data-theme");
  return { light, dark };
}
function useThemeValues() {
  const [values] = useState(readThemeValues);
  return values;
}
function ColorTokens() {
  const v = useThemeValues();
  return (
    <div className="grid grid-cols-1 gap-3 md:grid-cols-2">
      {(["light", "dark"] as const).map((mode) => (
        <Card key={mode}>
          <CardHeader><CardTitle className="text-body">{mode === "light" ? "Light" : "Dark"}</CardTitle></CardHeader>
          <CardContent>
            <ul className="grid grid-cols-1 gap-1.5 sm:grid-cols-2">
              {COLOR_TOKENS.map(([k, label]) => (
                <li key={k} className="flex items-center gap-2">
                  <span className="h-8 w-8 shrink-0 rounded-[var(--radius-sm)] border border-border" style={{ backgroundColor: v[mode][k] || `var(--color-${k})` }} aria-hidden />
                  <span className="min-w-0">
                    <span className="block truncate text-sm font-semibold text-on-surface">{label}</span>
                    <span className="block truncate font-mono text-caption text-on-surface-variant">--color-{k} · {v[mode][k] || "…"}</span>
                  </span>
                </li>
              ))}
            </ul>
          </CardContent>
        </Card>
      ))}
    </div>
  );
}

// ---------- data contoh ----------
interface SampleRow { id: string; number: string; title: string; location: string; status: string; priority: string; assignee: string }
const SAMPLE_ROWS: SampleRow[] = [
  { id: "1", number: "WO-2026-000123", title: "AC unit lantai 12 bocor", location: "Tower A / Lantai 12", status: "in_progress", priority: "high", assignee: "Budi (Engineering)" },
  { id: "2", number: "WO-2026-000124", title: "Ganti lampu koridor", location: "Tower B / Lantai 3", status: "on_hold", priority: "low", assignee: "Tim Engineering" },
  { id: "3", number: "WO-2026-000125", title: "Pompa air utama bunyi abnormal", location: "Basement / Ruang Pompa", status: "new", priority: "critical", assignee: "—" },
];
const NOTIFS: Notification[] = [
  { id: "n1", type: "sla", title: "SLA Risk · WO-2026-000123", body: "Sisa 45 menit sebelum batas resolusi.", object_type: "work_order", object_id: "1", object_label: "WO-2026-000123", deep_link: null, severity: "warning", created_at: new Date().toISOString(), read_at: null },
  { id: "n2", type: "assign", title: "Anda ditugaskan", body: "Task Patroli malam · Tower A", object_type: "task", object_id: "2", object_label: "TSK-1", deep_link: null, severity: "info", created_at: new Date(Date.now() - 3_600_000).toISOString(), read_at: new Date().toISOString() },
];

export default function DesignSystemPage() {
  const toast = useToast();
  const [modal, setModal] = useState(false);
  const [drawer, setDrawer] = useState(false);
  const [chips, setChips] = useState<string[]>(["open"]);
  const [date, setDate] = useState("");
  const [loading, setLoading] = useState(false);
  const [weekdays, setWeekdays] = useState<number[]>([1, 2, 3, 4, 5]);
  const columns = useMemo<ColumnDef<SampleRow, unknown>[]>(
    () => [
      { id: "number", header: "ID", meta: { mobile: "hidden" }, cell: ({ row }) => <span className="font-mono text-[13px] font-semibold">{row.original.number}</span> },
      { id: "title", header: "Judul", meta: { mobile: "primary" }, cell: ({ row }) => <div><div className="font-mono text-xs text-on-surface-variant sm:hidden">{row.original.number}</div>{row.original.title}</div> },
      { id: "location", header: "Lokasi", meta: { mobile: "secondary" }, cell: ({ row }) => row.original.location },
      { id: "status", header: "Status", meta: { mobile: "status" }, cell: ({ row }) => <StatusBadge objectType="work_order" status={row.original.status} /> },
      { id: "priority", header: "Prioritas", meta: { mobile: "secondary" }, cell: ({ row }) => <PriorityBadge priority={row.original.priority} /> },
      { id: "assignee", header: "Assignee", meta: { mobile: "secondary" }, cell: ({ row }) => row.original.assignee },
    ],
    [],
  );
  const flags: Flag[] = ["overdue", "sla_risk", "sla_breach", "critical", "reopened", "evidence_incomplete"];

  return (
    <div className="space-y-8 pb-8">
      <PageHeader
        breadcrumb={<><Link to="/settings" className="hover:underline">Settings</Link> / Design System</>}
        title="Design System"
        subtitle="Satu bahasa visual & interaksi BuildingVision (PRD P0 §20). Pedoman lengkap: web/docs/DESIGN-SYSTEM-GUIDELINE.md."
      >
        <nav aria-label="Bagian katalog" className="flex flex-wrap gap-1.5">
          {SECTIONS.map(([id, label]) => (
            <a key={id} href={`#${id}`} className="rounded-full bg-surface-container-high px-2.5 py-1 text-xs font-semibold text-on-surface-variant hover:bg-primary-container hover:text-on-primary-container">{label}</a>
          ))}
        </nav>
      </PageHeader>

      <Section id="colors" title="Warna" desc="Token alias --color-* (Tailwind: bg-primary, text-on-surface-variant, border-border …). Tidak ada warna literal di kode produk. Error = critical.">
        <ColorTokens />
      </Section>

      <Section id="typography" title="Tipografi" desc="Display · Heading (h1–h3) · Body · Label · Caption. Kelas Tailwind text-* membaca token tokens.json.">
        <Card><CardContent className="space-y-3 pt-4">
          {[
            ["text-display font-bold", "Display 28/36 · 700", "Building Operations"],
            ["text-h1 font-bold", "Heading 1 · 22/30 · 700", "Work Orders"],
            ["text-h2 font-semibold", "Heading 2 · 18/26 · 600", "Penugasan"],
            ["text-h3 font-semibold", "Heading 3 · 16/24 · 600", "Lokasi & Aset"],
            ["text-body", "Body · 14/20 · 400", "AC unit di lantai 12 bocor sejak pagi; perlu pengecekan kompresor."],
            ["text-label uppercase tracking-wide text-on-surface-variant", "Label · 12/16 · 600", "Prioritas"],
            ["text-caption text-on-surface-variant", "Caption · 11/16 · 400", "Diperbarui 5 menit lalu oleh Budi"],
          ].map(([cls, spec, sample]) => (
            <div key={spec} className="grid grid-cols-1 items-baseline gap-1 border-b border-border pb-2 last:border-0 sm:grid-cols-[220px_1fr]">
              <span className="font-mono text-caption text-on-surface-variant">{spec}<br />{cls.split(" ")[0]}</span>
              <span className={cn("text-on-surface", cls)}>{sample}</span>
            </div>
          ))}
        </CardContent></Card>
      </Section>

      <Section id="status" title="Status System" desc="Delapan kategori umum konsisten lintas modul (contracts/status-map.yaml grup common). Status object tetap kanonik; statusCategory() memetakannya ke kategori.">
        <Example title="Kategori umum" code={`<CommonStatusBadge category="in_progress" />\nstatusCategory("work_order", "on_hold") // → "pending"`}>
          {["new", "in_progress", "pending", "completed", "closed", "cancelled", "overdue", "critical"].map((c) => <CommonStatusBadge key={c} category={c} />)}
        </Example>
        <Example title="Flag" code={`<FlagBadges item={workOrder} showCritical />  // flags + reopen_count > 0 + priority critical`}>
          {flags.map((f) => <FlagBadge key={f} flag={f} />)}
        </Example>
        <Example title="Priority & Severity" code={`<PriorityBadge priority="high" />  <SeverityBadge severity="critical" />`}>
          {["low", "medium", "high", "critical"].map((p) => <PriorityBadge key={p} priority={p} />)}
          {["low", "critical"].map((p) => <SeverityBadge key={p} severity={p} />)}
        </Example>
        <Card><CardContent className="overflow-x-auto pt-4">
          <table className="bv-table">
            <thead><tr><th>Object</th>{STATUS_CATEGORIES.map((c) => <th key={c}>{c}</th>)}</tr></thead>
            <tbody>
              {(["task", "work_order", "service_request", "incident", "finding", "booking", "invoice"] as const).map((ot) => (
                <tr key={ot}>
                  <td className="font-mono text-xs">{ot}</td>
                  {STATUS_CATEGORIES.map((c) => (
                    <td key={c}><span className="flex flex-wrap gap-1">{Object.keys(statusMap[ot]).filter((s) => statusCategory(ot, s) === c).map((s) => <StatusBadge key={s} objectType={ot} status={s} />)}</span></td>
                  ))}
                </tr>
              ))}
            </tbody>
          </table>
        </CardContent></Card>
      </Section>

      <Section id="button" title="Button" desc="Maks. satu Primary per area. Loading = spinner + non-aktif (action loading).">
        <Example code={`<Button icon="add">Buat Work Order</Button>\n<Button variant="secondary">Batal</Button>\n<Button loading>Menyimpan</Button>`}>
          <Button icon="add">Primary</Button>
          <Button variant="secondary">Secondary</Button>
          <Button variant="tonal">Tonal</Button>
          <Button variant="ghost">Ghost</Button>
          <Button variant="success" icon="check_circle">Success</Button>
          <Button variant="destructive" icon="cancel">Destructive</Button>
          <Button variant="link">Link</Button>
          <Button loading={loading} onClick={() => { setLoading(true); setTimeout(() => setLoading(false), 1500); }}>{loading ? "Menyimpan…" : "Klik: loading"}</Button>
          <Button size="sm">Small</Button>
          <Button size="icon" variant="secondary" aria-label="Lainnya"><Icon name="more_vert" size={20} /></Button>
        </Example>
      </Section>

      <Section id="inputs" title="Input · Select · Date picker · Search">
        <div className="grid grid-cols-1 gap-3 md:grid-cols-2">
          <Example title="Input & Field" code={`<Field label="Judul" required error={err}>\n  <Input value={v} onChange={…} />\n</Field>`} className="block space-y-3">
            <Field label="Judul" required><Input placeholder="Mis. AC bocor lantai 12" /></Field>
            <Field label="Catatan" help="Opsional, tampil di Activity."><Textarea rows={2} /></Field>
            <Field label="Nomor unit" error="Nomor unit wajib diisi."><Input aria-invalid /></Field>
          </Example>
          <Example title="Select, Date picker, Search" code={`<NativeSelect value={s} onChange={…}>…</NativeSelect>\n<DatePicker label="Jatuh tempo" value={d} onChange={setD} />\n<SearchInput placeholder="Cari…" />`} className="block space-y-3">
            <Field label="Prioritas"><NativeSelect defaultValue="medium">{["low", "medium", "high", "critical"].map((p) => <option key={p} value={p}>{p}</option>)}</NativeSelect></Field>
            <DatePicker label="Jatuh tempo" value={date} onChange={setDate} />
            <DatePicker value={date} onChange={setDate} aria-label="Tanggal (ringkas)" />
            <SearchInput placeholder="Cari ID / judul…" />
          </Example>
        </div>
      </Section>

      <Section id="filter" title="Filter" desc="FilterBar menyimpan filter di URL (?status=…); FilterChip untuk preset.">
        <Example className="block space-y-3" code={`<FilterBar spec={{ status, priority: true, presets }} onExport={…} />\n<FilterChip selected={on} onClick={…}>Overdue</FilterChip>`}>
          <FilterBar spec={{ status: Object.keys(statusMap.work_order).map((v) => ({ value: v, label: statusMap.work_order[v].label_id })), priority: true, presets: [{ key: "overdue", label: "Overdue", params: { overdue: "true" } }] }} />
          <div className="flex flex-wrap gap-1.5">
            {["open", "overdue", "sla_risk", "mine"].map((c) => (
              <FilterChip key={c} selected={chips.includes(c)} onClick={() => setChips((s) => (s.includes(c) ? s.filter((x) => x !== c) : [...s, c]))}>{c}</FilterChip>
            ))}
          </div>
        </Example>
      </Section>

      <Section id="table" title="Table" desc="DataGrid: header solid primary; < 640px otomatis menjadi kartu (meta kolom mobile). Error → ErrorState, bukan kosong.">
        <Example className="block" code={`<DataGrid columns={cols} rows={rows} rowId={(r) => r.id}\n  isLoading={q.isLoading} error={q.error} onRetry={() => q.refetch()}\n  empty={{ title, description, action }} />`}>
          <DataGrid columns={columns} rows={SAMPLE_ROWS} rowId={(r) => r.id} layout="table" />
        </Example>
        <Example title="Mode kartu (mobile)" className="block" code={`meta: { mobile: "primary" | "secondary" | "status" | "hidden" }  // layout="cards" memaksa kartu`}>
          <div className="max-w-sm"><DataGrid columns={columns} rows={SAMPLE_ROWS} rowId={(r) => r.id} layout="cards" /></div>
        </Example>
      </Section>

      <Section id="card" title="Card & Dashboard card">
        <div className="grid grid-cols-1 gap-3 md:grid-cols-3">
          <Card><CardHeader><div><CardTitle>Card</CardTitle><CardSubtitle>SurfaceCard + hairline + radius-xl</CardSubtitle></div></CardHeader><CardContent className="text-sm text-on-surface-variant">{`<Card><CardHeader>…</CardHeader><CardContent>…</CardContent></Card>`}</CardContent></Card>
          <Card railTone="warning"><CardHeader><CardTitle>Rail tone</CardTitle></CardHeader><CardContent className="text-sm text-on-surface-variant">{`<Card railTone="warning">`} — penanda perhatian tanpa border-l manual.</CardContent></Card>
          <TodayCounter label="Open Work Orders" value={42} link="/operations/work-orders" breakdown={[{ label: "Overdue", value: 3, tone: "critical" }, { label: "SLA Risk", value: 5, tone: "warning" }]} />
        </div>
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-4">
          <MetricCard label="Open Work Orders" value="42" delta="+4" tone="primary" icon={<Icon name="construction" size={20} />} />
          <MetricCard label="SLA compliance" value="96%" delta="+1.2%" tone="success" />
          <MetricCard label="Overdue" value="3" delta="-2" deltaType="negative" tone="error" />
          <MetricCard label="Preventive Maintenance 7 hari" value="11" tone="warning" />
        </div>
      </Section>

      <Section id="overlay" title="Modal & Drawer" desc="Modal untuk konfirmasi/form pendek; Drawer kanan (560px, penuh di mobile) untuk form panjang/detail.">
        <Example code={`<Dialog open={o} onOpenChange={setO}><DialogContent title="…">…<DialogFooter>…</DialogFooter></DialogContent></Dialog>\n<Drawer open={o} onClose={…} title="…">…</Drawer>`}>
          <Button variant="secondary" onClick={() => setModal(true)}>Buka Modal</Button>
          <Button variant="secondary" onClick={() => setDrawer(true)}>Buka Drawer</Button>
        </Example>
        <Dialog open={modal} onOpenChange={setModal}>
          <DialogContent title="Tutup Work Order?" description="Work Order yang ditutup tidak bisa diedit, tetapi bisa dibuka kembali dengan alasan.">
            <Field label="Catatan penutupan"><Textarea rows={2} /></Field>
            <DialogFooter>
              <Button variant="ghost" onClick={() => setModal(false)}>Batal</Button>
              <Button onClick={() => { setModal(false); toast.action("closed", "Work Order WO-2026-000123"); }}>Tutup</Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
        <Drawer open={drawer} onClose={() => setDrawer(false)} title="Edit Work Order" description="Contoh drawer form panjang">
          <FormSkeleton fields={5} />
        </Drawer>
      </Section>

      <Section id="tabs" title="Tabs">
        <Example className="block" code={`<Tabs defaultValue="detail"><TabsList><TabsTrigger value="detail">Detail</TabsTrigger>…</TabsList><TabsContent value="detail">…</TabsContent></Tabs>`}>
          <Tabs defaultValue="detail">
            <TabsList>
              <TabsTrigger value="detail">Detail</TabsTrigger>
              <TabsTrigger value="checklist" badge={3}>Checklist</TabsTrigger>
              <TabsTrigger value="activity">Activity</TabsTrigger>
            </TabsList>
            <TabsContent value="detail"><p className="text-sm text-on-surface-variant">Isi tab Detail.</p></TabsContent>
            <TabsContent value="checklist"><p className="text-sm text-on-surface-variant">3 item checklist.</p></TabsContent>
            <TabsContent value="activity"><p className="text-sm text-on-surface-variant">Riwayat aktivitas.</p></TabsContent>
          </Tabs>
        </Example>
      </Section>

      <Section id="badge" title="Badge & Status indicator" desc="Badge = pasangan container/on-container. Status indicator = StatusBadge dari kontrak (warna tidak boleh manual).">
        <Example code={`<Badge tone="success">Aktif</Badge>\n<StatusBadge objectType="service_request" status="waiting_for_tenant" />`}>
          {(["primary", "success", "warning", "error", "info", "neutral"] as const).map((t) => <Badge key={t} tone={t}>{t}</Badge>)}
          <Badge dot tone="info">dengan dot</Badge>
          <StatusBadge objectType="service_request" status="waiting_for_tenant" />
          <StatusBadge objectType="incident" status="resolved" />
          <StatusBadge objectType="invoice" status="overdue" />
          <StatusBadge objectType="booking" status="pending" />
        </Example>
      </Section>

      <Section id="states" title="Empty · Loading · Error" desc="Empty: apa yang kosong · mengapa · aksi berikutnya. Error: apa yang terjadi · dampak · pemulihan. Loading: page/table/card/form/KPI + action.">
        <div className="grid grid-cols-1 gap-3 lg:grid-cols-2">
          <Example title="Empty state" className="block" code={`<EmptyState icon="construction" title="Belum ada Work Order."\n  description="…" action={<Button icon="add">Buat Work Order</Button>} />`}>
            <EmptyState icon="construction" title="Belum ada Work Order." description="Work Order muncul setelah dibuat dari Task, Finding, jadwal Preventive Maintenance, atau manual." action={<Button icon="add">Buat Work Order</Button>} />
          </Example>
          <Example title="Error state" className="block" code={`<QueryErrorState error={q.error} onRetry={() => q.refetch()} />  // 403 → Akses ditolak\n<ErrorState title="…" impact="…" onRetry={…} />`}>
            <ErrorState title="Gagal memuat Work Order" description="Server tidak merespons (timeout)." onRetry={() => toast.info("Mencoba lagi…")} requestId="req_01J9Z…" />
          </Example>
          <Example title="Akses ditolak (403)" className="block" code={`<RouteGuard access={ACCESS.workOrders}>…</RouteGuard>  // → ForbiddenPage`}>
            <ForbiddenState compact />
          </Example>
          <Example title="Loading: table & card" className="block" code={`<TableSkeleton />  <CardSkeleton />`}>
            <TableSkeleton rows={3} />
            <CardSkeleton />
          </Example>
          <Example title="Loading: form" className="block" code={`<FormSkeleton fields={3} />`}>
            <FormSkeleton fields={3} />
          </Example>
          <Example title="Loading: page" className="block" code={`<PageSkeleton />  // fallback Suspense route`}>
            <div className="max-h-64 overflow-hidden"><PageSkeleton /></div>
          </Example>
        </div>
        <Example title="Loading: KPI / dashboard card" className="block" code={`<KpiSkeleton count={4} />`}>
          <KpiSkeleton count={4} />
        </Example>
      </Section>

      <Section id="toast" title="Toast (Success State)" desc="Konfirmasi setelah Create · Update · Assign · Complete · Delete dari katalog pesan lib/messages.ts (i18n toast.*).">
        <Example code={`const toast = useToast();\ntoast.action("created", "Work Order WO-1");   // "Work Order WO-1 berhasil dibuat"\ntoast.transition("start", "WO-1");            // aksi server → pesan standar\ntoast.failed("saved", err, "Vendor");         // "Vendor gagal disimpan. <detail>"`}>
          {(["created", "updated", "assigned", "completed", "deleted"] as const).map((a) => (
            <Button key={a} variant="secondary" size="sm" onClick={() => toast.action(a, "Work Order WO-2026-000123", a === "created" ? { to: "/operations/work-orders", label: "Lihat" } : undefined)}>{a}</Button>
          ))}
          <Button variant="secondary" size="sm" onClick={() => toast.failed("saved", new Error("Versi data sudah berubah (409)."), "Work Order")}>error</Button>
          <Button variant="secondary" size="sm" onClick={() => toast.warning("Koneksi lambat, perubahan disimpan offline.")}>warning</Button>
        </Example>
      </Section>

      <Section id="notification" title="Notification" desc="Item inbox: titik severity, judul, isi, waktu relatif; belum dibaca = latar primary-soft.">
        <Card><CardContent className="max-w-md space-y-1 pt-4">
          {NOTIFS.map((n) => <NotificationItem key={n.id} n={n} onRead={() => undefined} />)}
          <pre className="mt-2 overflow-x-auto rounded-[var(--radius-md)] bg-surface-container px-3 py-2 font-mono text-caption text-on-surface-variant"><code>{`<NotificationItem n={notification} onRead={markRead} />`}</code></pre>
        </CardContent></Card>
      </Section>

      <Section id="mobile" title="Mobile action component" desc="< 768px: MobileActionBar (aksi utama menempel di bawah, sekunder di overflow/BottomSheet) di detail Task/WO/SR/Incident; FAB untuk Buat di daftar. Pratinjau di bawah statis.">
        <div className="grid grid-cols-1 gap-3 md:grid-cols-2">
          <Example title="MobileActionBar" className="block" code={`<MobileActionBar primary={{ label: "Mulai", icon: "play_arrow", onSelect }}\n  secondary={[{ label: "Tunda", onSelect }, { label: "Batalkan", destructive: true, onSelect }]} />\n// detail: <TransitionActions … mobileBar />`}>
            <div className="mx-auto w-full max-w-[360px] overflow-hidden rounded-[var(--radius-lg)] border border-border">
              <div className="space-y-2 p-3"><CardSkeleton lines={2} /></div>
              <MobileActionBar preview primary={{ label: "Mulai", icon: "play_arrow", onSelect: () => toast.action("started", "Task TSK-1") }} secondary={[{ label: "Tugaskan", icon: "person_add", onSelect: () => undefined }, { label: "Tunda", icon: "pause_circle", onSelect: () => undefined }, { label: "Batalkan", icon: "cancel", destructive: true, onSelect: () => undefined }]} />
            </div>
          </Example>
          <Example title="FAB" className="block" code={`{canCreate && <Fab label="Buat" onClick={() => setCreateOpen(true)} />}`}>
            <div className="relative mx-auto h-48 w-full max-w-[360px] overflow-hidden rounded-[var(--radius-lg)] border border-border bg-background p-3">
              <CardSkeleton lines={1} />
              <div className="absolute bottom-3 right-3"><Fab preview label="Buat" onClick={() => toast.info("Buka form Buat")} /></div>
            </div>
          </Example>
        </div>
        <Alert variant="info" title="Breakpoint">mobile &lt; 768 · tablet 768–1279 (sidebar rail) · desktop ≥ 1280. Tabel menjadi kartu &lt; 640.</Alert>
      </Section>

      <Section id="workforce" title="Dashboard & Workforce (PRD P2 v2.1)" desc="KPI dashboard domain (nilai, severity, hint, drill-down dari server), health equipment (NC §14), masa berlaku dokumen/sertifikat, status shift & route, pemilih hari jadwal berulang.">
        <div className="grid grid-cols-1 gap-3 lg:grid-cols-2">
          <Example title="KpiCard (severity server → rail + badge)" className="block" code={`<KpiCard kpi={{ key: "pm_overdue", label: "Preventive Maintenance terlambat", value: 3, unit: "count",
  severity: "warning", hint: "…", drill_down: "/engineering/preventive-maintenance?status=overdue" }} />`}>
            <div className="grid grid-cols-2 gap-3">
              <KpiCard kpi={{ key: "pm_overdue", label: "Preventive Maintenance terlambat", value: 3, unit: "count", severity: "warning", hint: "Jadwal Preventive Maintenance melewati jatuh tempo", drill_down: "/engineering/preventive-maintenance?status=overdue" }} />
              <KpiCard kpi={{ key: "pm_compliance", label: "Kepatuhan Preventive Maintenance", value: 96.4, unit: "pct", severity: "normal", hint: "Preventive Maintenance selesai tepat waktu ÷ yang jatuh tempo", drill_down: "/reports/maintenance" }} />
            </div>
          </Example>
          <Example title="Health & masa berlaku" className="block space-y-2" code={`<HealthBadge status="warning" score={82} />
<ValidityBadge status="expiring" days={12} />`}>
            <div className="flex flex-wrap gap-2">{["healthy", "warning", "critical", "offline", "unknown"].map((h) => <HealthBadge key={h} status={h} score={h === "healthy" ? 95 : h === "warning" ? 82 : h === "critical" ? 61 : null} />)}</div>
            <div className="flex flex-wrap gap-3"><ValidityBadge status="valid" days={120} /><ValidityBadge status="expiring" days={12} /><ValidityBadge status="expired" days={-3} /><ValidityBadge status="no_expiry" /></div>
          </Example>
          <Example title="Status shift, kehadiran, serah terima, route" code={`<StatusBadge objectType="roster_attendance" status="late" />
<StatusBadge objectType="shift_handover" status="submitted" />`}>
            {Object.keys(statusMap.roster_attendance).map((st) => <StatusBadge key={st} objectType="roster_attendance" status={st} />)}
            <StatusBadge objectType="attendance" status="auto_closed" />
            <StatusBadge objectType="shift_handover" status="submitted" />
            <StatusBadge objectType="cleaning_route_run" status="in_progress" />
          </Example>
          <Example title="Warna shift (tone token, bukan hex) & WeekdayPicker" className="block space-y-3" code={`<ShiftDot color="info" />  // color = primary|info|success|warning|error|neutral
<WeekdayPicker value={days} onChange={setDays} />  // 0 = Minggu … 6 = Sabtu`}>
            <div className="flex flex-wrap gap-3 text-sm">{SHIFT_TONES.map((t) => <span key={t.value} className="inline-flex items-center gap-1.5"><ShiftDot color={t.value} />{t.label}</span>)}</div>
            <WeekdayPicker value={weekdays} onChange={setWeekdays} />
          </Example>
        </div>
      </Section>
    </div>
  );
}
