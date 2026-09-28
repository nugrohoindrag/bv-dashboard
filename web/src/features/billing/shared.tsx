// Komponen bersama halaman Billing P4 v2.1: pemilih property/pihak tagih, tile ringkasan, aksi dokumen (PDF + tautan
// bertanda tangan untuk dibagikan lewat WhatsApp manual), meta item (meter/prorata/persentase), badge pengecualian.
// Tipe, label, helper hitung & hook ada di ./types (file ini hanya mengekspor komponen).
import { useMemo, useState, type ReactNode } from "react";
import { Link } from "react-router-dom";
import { Icon } from "@buildingvision/ui";
import { toneContainer, toneOnContainer, type Tone } from "@buildingvision/ui/bv";
import { Badge, Button, DatePicker, Dialog, DialogContent, DialogFooter, Field, Input, NativeSelect } from "@/components/ui/primitives";
import { ComboBox, LocationPicker } from "@/components/bv/pickers";
import { useToast } from "@/components/bv/common";
import { api, downloadFile, uuid } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { fmtDateTime } from "@/lib/format";
import { cn } from "@/lib/utils";
import { invoiceTypeLabel, money, qtyText, unitLabelOf, useParties, type DocumentLink } from "./types";

/** Label bagian (uppercase kecil) di dalam drawer/form. */
export function SectionLabel({ children, className, action }: { children: ReactNode; className?: string; action?: ReactNode }) {
  return (
    <div className={cn("mb-2 flex items-center justify-between gap-2", className)}>
      <div className="text-xs font-semibold uppercase tracking-wide text-on-surface-variant">{children}</div>
      {action}
    </div>
  );
}

/** Tile ringkasan angka (total, sisa, pengecualian…) — pasangan container/on-container token. */
export function SummaryTile({ label, value, sub, tone, className }: { label: string; value: ReactNode; sub?: ReactNode; tone?: Tone; className?: string }) {
  return (
    <div
      className={cn("rounded-[var(--radius-lg)] border border-border px-3 py-2.5", !tone && "bg-surface", className)}
      style={tone ? { backgroundColor: toneContainer[tone], color: toneOnContainer[tone], borderColor: "transparent" } : undefined}
    >
      <div className={cn("text-xs font-semibold uppercase tracking-wide", !tone && "text-on-surface-variant")}>{label}</div>
      <div className="tnum mt-0.5 text-h3 font-bold">{value}</div>
      {sub && <div className={cn("text-xs", !tone && "text-on-surface-variant")}>{sub}</div>}
    </div>
  );
}

/** Nomor invoice: mono; draft (belum bernomor, D-P4-01) → badge "Draft". */
export function InvoiceNo({ number, className }: { number: string | null | undefined; className?: string }) {
  if (!number) return <Badge className={className}>Draft</Badge>;
  return <span className={cn("whitespace-nowrap font-mono text-[13px] font-semibold", className)}>{number}</span>;
}

/** Tautan ke detail invoice (drawer Invoices). */
export function InvoiceLink({ id, number, className }: { id: string; number: string | null | undefined; className?: string }) {
  return (
    <Link to={`/billing/invoices/${id}`} className={cn("text-primary hover:underline", className)} onClick={(e) => e.stopPropagation()}>
      {number ? <span className="font-mono text-[13px] font-semibold">{number}</span> : "Draft"}
    </Link>
  );
}

/** Pilihan property untuk form (disembunyikan bila user hanya punya satu property, kecuali `allowOrg`). */
export function PropertySelect({ value, onChange, allowOrg, label = "Property", required, disabled }: { value: string; onChange: (id: string) => void; allowOrg?: boolean; label?: string; required?: boolean; disabled?: boolean }) {
  const { properties } = useAuth();
  if (!allowOrg && properties.length <= 1) return null;
  return (
    <Field label={label} required={required}>
      <NativeSelect value={value} onChange={(e) => onChange(e.target.value)} disabled={disabled}>
        {allowOrg ? <option value="">Default organization</option> : !value && <option value="">Pilih property…</option>}
        {properties.map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}
      </NativeSelect>
    </Field>
  );
}

/**
 * Pihak tagih: tenant (opsional) dan/atau unit — minimal salah satu (server validateParties). Tenant dipilih → unit dari
 * unit milik tenant; tanpa tenant → pemilih unit pohon lokasi (unit tanpa tenant / pemilik).
 */
export function PartyPicker({ propertyId, tenantId, unitId, onChange, disabled, tenantLabel = "Tenant", unitLabel = "Unit", required = true }: { propertyId: string; tenantId: string; unitId: string; onChange: (tenantId: string, unitId: string) => void; disabled?: boolean; tenantLabel?: string; unitLabel?: string; required?: boolean }) {
  const { tenants, units } = useParties(propertyId);
  const tenantList = useMemo(() => tenants.data ?? [], [tenants.data]);
  const tenant = tenantList.find((t) => t.id === tenantId);
  const unit = (units.data ?? []).find((u) => u.id === unitId);
  const unitDetails = (unit?.details ?? {}) as Record<string, unknown>;
  const occupant = !tenantId && unitDetails.tenant_id ? { id: String(unitDetails.tenant_id), name: String(unitDetails.tenant_name ?? "tenant") } : null;
  return (
    <div className="space-y-2">
      <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
        <Field label={tenantLabel} help={!tenantId ? "Kosongkan untuk menagih unit (mis. pemilik unit tanpa tenant)." : undefined}>
          <ComboBox
            items={tenantList}
            value={tenantId || null}
            onChange={(id) => {
              const t = tenantList.find((x) => x.id === id);
              // unit ikut tenant: pertahankan bila milik tenant, selain itu unit pertama tenant
              const keep = t?.units.some((u) => u.location_id === unitId) ? unitId : t?.units[0]?.location_id ?? "";
              onChange(id ?? "", id ? keep : unitId);
            }}
            render={(t) => <div><div className="font-medium">{t.name}</div><div className="text-xs text-on-surface-variant">{t.tenant_code}{t.units.length ? ` · ${t.units.map((u) => u.unit_number).join(", ")}` : ""}</div></div>}
            label={(t) => `${t.name} (${t.tenant_code})`}
            placeholder={tenants.isLoading ? "Memuat tenant…" : "Pilih tenant (opsional)…"}
            filter={(t, q) => t.name.toLowerCase().includes(q) || t.tenant_code.toLowerCase().includes(q) || t.units.some((u) => u.unit_number.toLowerCase().includes(q))}
            disabled={disabled || !propertyId}
            loading={tenants.isLoading}
          />
        </Field>
        <Field label={unitLabel} required={required && !tenantId}>
          {tenant ? (
            <NativeSelect value={unitId} onChange={(e) => onChange(tenantId, e.target.value)} disabled={disabled}>
              <option value="">Tanpa unit</option>
              {tenant.units.map((u) => <option key={u.location_id} value={u.location_id}>Unit {u.unit_number}</option>)}
            </NativeSelect>
          ) : (
            <LocationPicker propertyId={propertyId} value={unitId || null} onChange={(id) => onChange(tenantId, id ?? "")} allowTypes={["unit"]} placeholder="Pilih unit…" disabled={disabled} />
          )}
        </Field>
      </div>
      {occupant && (
        <p className="flex flex-wrap items-center gap-2 text-xs text-on-surface-variant">
          <Icon name="info" size={14} /> {unit ? unitLabelOf(unit) : "Unit"} ditempati <b>{occupant.name}</b>.
          <Button type="button" variant="link" size="sm" onClick={() => onChange(occupant.id, unitId)}>Tagihkan ke tenant ini</Button>
        </p>
      )}
    </div>
  );
}

/** Filter tenant (combobox) untuk baris filter daftar. */
export function TenantFilter({ propertyId, value, onChange, className }: { propertyId?: string | null; value: string; onChange: (id: string) => void; className?: string }) {
  const { tenants } = useParties(propertyId);
  const list = tenants.data ?? [];
  return (
    <span className={cn("inline-flex w-full sm:w-56", className)}>
      <ComboBox
        items={list}
        value={value || null}
        onChange={(id) => onChange(id ?? "")}
        render={(t) => <div><div className="font-medium">{t.name}</div><div className="text-xs text-on-surface-variant">{t.tenant_code}</div></div>}
        label={(t) => t.name}
        placeholder={propertyId ? "Tenant: Semua" : "Tenant (pilih property)"}
        filter={(t, q) => t.name.toLowerCase().includes(q) || t.tenant_code.toLowerCase().includes(q)}
        disabled={!propertyId}
        loading={tenants.isLoading}
      />
    </span>
  );
}

/** Rentang tanggal filter (YYYY-MM-DD; dikonversi ke awal/akhir hari zona waktu property saat query). */
export function DateRangeFilter({ label, from, to, onChange, className }: { label: string; from: string; to: string; onChange: (from: string, to: string) => void; className?: string }) {
  return (
    <span className={cn("inline-flex w-full items-center gap-1 sm:w-auto", className)} aria-label={label}>
      <span className="shrink-0 text-xs text-on-surface-variant">{label}</span>
      <DatePicker className="min-w-0 flex-1 sm:w-36 sm:flex-none" value={from.slice(0, 10)} onChange={(v) => onChange(v, to)} aria-label={`${label} dari`} />
      <span className="text-on-surface-variant">–</span>
      <DatePicker className="min-w-0 flex-1 sm:w-36 sm:flex-none" value={to.slice(0, 10)} onChange={(v) => onChange(from, v)} aria-label={`${label} sampai`} />
    </span>
  );
}

/**
 * Aksi dokumen: unduh PDF (fetch ber-auth) + salin tautan bertanda tangan 72 jam (untuk dibagikan lewat WhatsApp manual —
 * email di-hold). `kind` invoice → /invoices/{id}; receipt → /payments/{id}/receipt.
 */
export function DocActions({ kind, id, fileName, size = "sm" }: { kind: "invoice" | "receipt"; id: string; fileName?: string; size?: "sm" | "md" }) {
  const toast = useToast();
  const [busy, setBusy] = useState<"pdf" | "link" | null>(null);
  const [link, setLink] = useState<DocumentLink | null>(null);
  const pdfPath = kind === "invoice" ? `invoices/${id}/pdf` : `payments/${id}/receipt`;
  const linkPath = kind === "invoice" ? `invoices/${id}/document-link` : `payments/${id}/document-link`;
  const download = async () => {
    setBusy("pdf");
    try {
      await downloadFile(pdfPath, { download: 1 }, `${fileName ?? kind}.pdf`);
    } catch (e) {
      toast.error(e);
    } finally {
      setBusy(null);
    }
  };
  const share = async () => {
    setBusy("link");
    try {
      const l = await api<DocumentLink>(linkPath, { method: "POST", body: {}, idempotencyKey: uuid() });
      const abs = l.url.startsWith("http") ? l.url : new URL(l.url, window.location.origin).toString();
      setLink({ ...l, url: abs });
      try {
        await navigator.clipboard.writeText(abs);
        toast.success(`Tautan disalin — berlaku sampai ${fmtDateTime(l.expires_at)}`);
      } catch {
        /* clipboard tidak tersedia (non-HTTPS) → tampilkan dialog salin manual */
      }
    } catch (e) {
      toast.error(e);
    } finally {
      setBusy(null);
    }
  };
  return (
    <>
      <Button variant="secondary" size={size} icon="picture_as_pdf" loading={busy === "pdf"} onClick={download}>{kind === "invoice" ? "Unduh PDF" : "Kwitansi PDF"}</Button>
      <Button variant="secondary" size={size} icon="link" loading={busy === "link"} onClick={share}>Salin tautan</Button>
      {link && (
        <Dialog open onOpenChange={(o) => !o && setLink(null)}>
          <DialogContent title="Tautan dokumen" description={`Tautan bertanda tangan untuk dibagikan ke tenant (mis. lewat WhatsApp). Berlaku sampai ${fmtDateTime(link.expires_at)}.`}>
            <Field label="Tautan"><Input readOnly value={link.url} onFocus={(e) => e.currentTarget.select()} /></Field>
            <DialogFooter>
              <Button variant="secondary" icon="open_in_new" onClick={() => window.open(link.url, "_blank", "noopener")}>Buka</Button>
              <Button icon="content_copy" onClick={() => navigator.clipboard?.writeText(link.url).then(() => toast.success("Tautan disalin")).catch(() => toast.warning("Salin manual dari kolom tautan"))}>Salin</Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      )}
    </>
  );
}

/** Keterangan tambahan item dari meta server: meter (awal → akhir), prorata, luas, persentase, kendaraan, denda. */
export function ItemMeta({ meta, unit }: { meta?: Record<string, unknown> | null; unit?: string | null }) {
  if (!meta) return null;
  const parts: ReactNode[] = [];
  const n = (v: unknown) => (typeof v === "number" ? v : Number(v));
  if (meta.meter_number !== undefined) {
    const usage = meta.usage !== undefined ? ` · pemakaian ${qtyText(n(meta.usage))} ${unit ?? ""}` : "";
    const range = meta.previous !== undefined && meta.current !== undefined ? `: ${qtyText(n(meta.previous))} → ${qtyText(n(meta.current))}` : "";
    parts.push(<span key="meter">Meter {String(meta.meter_number)}{range}{usage}</span>);
    if (meta.tariff) parts.push(<span key="tariff">Tarif {String(meta.tariff)}{meta.fixed_charge ? ` · abonemen ${money(n(meta.fixed_charge))}` : ""}</span>);
  }
  if (meta.prorate_days !== undefined && meta.period_days !== undefined) parts.push(<span key="prorate">Prorata {String(meta.prorate_days)}/{String(meta.period_days)} hari</span>);
  if (meta.area_m2 !== undefined) parts.push(<span key="area">Luas {qtyText(n(meta.area_m2))} m²</span>);
  if (meta.percent !== undefined && meta.base_amount !== undefined) parts.push(<span key="pct">{qtyText(n(meta.percent))}% × {money(n(meta.base_amount))}</span>);
  if (meta.plate) parts.push(<span key="plate">Kendaraan {String(meta.plate)}{meta.vehicle_type ? ` (${String(meta.vehicle_type)})` : ""}</span>);
  if (meta.unit_type && meta.basis === "per_unit_type") parts.push(<span key="ut">Tipe unit {String(meta.unit_type)}</span>);
  if (meta.invoice_number && meta.days_late !== undefined) {
    parts.push(
      <span key="pen">
        Denda {meta.invoice_id ? <Link className="text-primary hover:underline" to={`/billing/invoices/${String(meta.invoice_id)}`}>{String(meta.invoice_number)}</Link> : String(meta.invoice_number)} · {String(meta.days_late)} hari
      </span>,
    );
  }
  if (!parts.length) return null;
  return <div className="mt-0.5 flex flex-wrap gap-x-2 gap-y-0.5 text-xs text-on-surface-variant">{parts.map((p, i) => <span key={i} className="inline-flex items-center gap-1">{i > 0 && <span aria-hidden>·</span>}{p}</span>)}</div>;
}

/** Badge pengecualian baris billing run (tone warning; already_billed = neutral). */
export function ExceptionBadge({ code, label }: { code: string; label?: string | null }) {
  return (
    <Badge tone={code === "already_billed" ? "neutral" : "warning"} title={label ?? code}>
      <Icon name={code === "already_billed" ? "done_all" : "warning"} size={12} aria-hidden />
      {label ?? code}
    </Badge>
  );
}

/** Jenis invoice (+ periode) ringkas. */
export function TypeText({ type, className }: { type: string; className?: string }) {
  return <span className={cn("whitespace-nowrap", className)}>{invoiceTypeLabel(type)}</span>;
}
