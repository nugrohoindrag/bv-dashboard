// Dialog Package (PRD P3 v2.1 §5.9 P3-PKG-01..04): catat paket masuk (penerima unit/tenant/akun, kurir, resi, deskripsi, lokasi
// simpan, waktu terima, foto, beri tahu tenant), edit, serah terima (nama pengambil, catatan, tanda tangan & foto opsional —
// diunggah ke object `package` sebelum aksi), dan retur (alasan wajib). Tipe API: api/internal/parcels/service.go.
import { useEffect, useMemo, useRef, useState } from "react";
import { Icon } from "@buildingvision/ui";
import { Button, Checkbox, Dialog, DialogContent, DialogFooter, Field, Input, NativeSelect, Textarea } from "@/components/ui/primitives";
import { useToast } from "@/components/bv/common";
import { LocationPicker } from "@/components/bv/pickers";
import { SignaturePad } from "@/components/bv/SignaturePad";
import { uploadAttachment, useAll, useInvalidate } from "@/api/hooks";
import { api, uuid } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { getTimezone } from "@/lib/format";
import { signatureFile } from "@/lib/signature";
import type { Attachment, Tenant } from "@/api/types";
import { localInputToISO, nowLocalInput } from "@/features/tenant-relation/utils";
import type { TenantUser } from "@/features/tenant-relation/TenantUsersPage";
import { PrivacyNote, PropertyField } from "./shared";
import { TOUCH, usePropertyChoice } from "./hooks";

export interface PackagePhoto { id: string; attachment_type: string; url?: string; thumb_url?: string }
export interface Package {
  id: string;
  package_number: string;
  property_id: string;
  unit_location_id: string | null;
  unit_name: string | null;
  tenant_id: string | null;
  tenant_name: string | null;
  recipient_user_id: string | null;
  recipient_name: string;
  /** document | parcel | food | large | other */
  package_type: string;
  courier: string | null;
  tracking_number: string | null;
  description: string | null;
  storage_location: string | null;
  /** received | notified | picked_up | returned */
  status: string;
  received_at: string;
  received_by_name: string | null;
  notified_at: string | null;
  reminder_count: number;
  last_reminded_at: string | null;
  picked_up_at: string | null;
  picked_up_by_name: string | null;
  handed_over_by_name: string | null;
  handover_note: string | null;
  returned_at: string | null;
  return_reason: string | null;
  days_waiting: number;
  photos: PackagePhoto[];
  allowed_actions: string[];
  version: number;
}

const PACKAGE_TYPES: Record<string, string> = { parcel: "Paket", document: "Dokumen", food: "Makanan / minuman", large: "Barang besar", other: "Lainnya" };
const PACKAGE_TYPE_ICON: Record<string, string> = { parcel: "inventory_2", document: "description", food: "room_service", large: "inventory_2", other: "inventory" };
const COURIERS = ["JNE", "J&T Express", "SiCepat", "AnterAja", "Pos Indonesia", "Ninja Xpress", "Shopee Express", "Lion Parcel", "GoSend", "GrabExpress", "Lalamove", "Paxel", "DHL", "FedEx"];
const MAX_PHOTOS = 3;

export function PackageTypeLabel({ type, withIcon }: { type: string; withIcon?: boolean }) {
  const label = PACKAGE_TYPES[type] ?? type.replace(/_/g, " ");
  if (!withIcon) return <>{label}</>;
  return <span className="inline-flex items-center gap-1"><Icon name={PACKAGE_TYPE_ICON[type] ?? "inventory_2"} size={14} className="text-on-surface-variant" aria-hidden />{label}</span>;
}

function PackageTypeSelect({ value, onChange }: { value: string; onChange: (v: string) => void }) {
  return <NativeSelect value={value} onChange={(e) => onChange(e.target.value)}>{Object.entries(PACKAGE_TYPES).map(([v, l]) => <option key={v} value={v}>{l}</option>)}</NativeSelect>;
}

function CourierInput({ value, onChange }: { value: string; onChange: (v: string) => void }) {
  return (
    <>
      <Input list="bv-couriers" value={value} onChange={(e) => onChange(e.target.value)} placeholder="mis. JNE" maxLength={80} />
      <datalist id="bv-couriers">{COURIERS.map((c) => <option key={c} value={c} />)}</datalist>
    </>
  );
}

/** Pemilih foto lokal (pratinjau) untuk diunggah setelah record tersimpan. */
function PhotoPicker({ files, onChange, max = MAX_PHOTOS, label = "Foto paket" }: { files: File[]; onChange: (f: File[]) => void; max?: number; label?: string }) {
  const toast = useToast();
  const ref = useRef<HTMLInputElement>(null);
  // pratinjau lokal; object URL dibebaskan saat daftar berubah / dialog ditutup
  const urls = useMemo(() => files.map((f) => URL.createObjectURL(f)), [files]);
  useEffect(() => () => urls.forEach((u) => URL.revokeObjectURL(u)), [urls]);
  const add = (list: FileList | null) => {
    if (!list?.length) return;
    const images = Array.from(list).filter((f) => f.type.startsWith("image/"));
    if (images.length < list.length) toast.warning("Hanya file foto (JPG, PNG, WebP).");
    const room = max - files.length;
    if (images.length > room) toast.warning(`Maksimal ${max} foto.`);
    onChange([...files, ...images.slice(0, Math.max(0, room))]);
  };
  return (
    <Field label={label} help={`Opsional · maks ${max} foto, dikompres otomatis.`}>
      <div className="space-y-2">
        {files.length > 0 && (
          <ul className="flex flex-wrap gap-2">
            {files.map((f, i) => (
              <li key={`${f.name}-${i}`} className="relative">
                {urls[i] && <img src={urls[i]} alt={f.name} className="h-16 w-16 rounded-[var(--radius-md)] border border-border object-cover" />}
                <button type="button" aria-label={`Hapus ${f.name}`} onClick={() => onChange(files.filter((_, j) => j !== i))} className="absolute -right-1.5 -top-1.5 inline-flex h-6 w-6 items-center justify-center rounded-full border border-border bg-surface text-on-surface-variant hover:text-on-surface"><Icon name="close" size={14} /></button>
              </li>
            ))}
          </ul>
        )}
        <input ref={ref} type="file" accept="image/jpeg,image/png,image/webp" capture="environment" multiple className="hidden" onChange={(e) => { add(e.target.files); e.target.value = ""; }} />
        <Button type="button" size="sm" variant="secondary" icon="photo_camera" className={TOUCH} disabled={files.length >= max} onClick={() => ref.current?.click()}>Tambah foto</Button>
      </div>
    </Field>
  );
}

async function uploadAll(files: File[], packageId: string, attachmentType: string, onProgress?: (msg: string) => void): Promise<Attachment[]> {
  const out: Attachment[] = [];
  for (let i = 0; i < files.length; i++) {
    onProgress?.(`Mengunggah foto ${i + 1}/${files.length}…`);
    out.push(await uploadAttachment(files[i], "package", packageId, attachmentType));
  }
  return out;
}

// ---------- Catat paket masuk (P3-PKG-01) ----------
export function RecordPackageDialog({ onClose, onCreated }: { onClose: () => void; onCreated: (p: Package) => void }) {
  const toast = useToast();
  const invalidate = useInvalidate();
  const { can } = useAuth();
  const tz = getTimezone();
  const [pid, setPid] = usePropertyChoice();
  const [initialReceived] = useState(() => nowLocalInput(tz));
  const [f, setF] = useState({ unit_location_id: null as string | null, unit_label: "", tenant_id: "", recipient_user_id: "", recipient_name: "", package_type: "parcel", courier: "", tracking_number: "", description: "", storage_location: "", received_at: initialReceived, notify: true });
  const [photos, setPhotos] = useState<File[]>([]);
  const [busy, setBusy] = useState<string | null>(null);
  const canTenants = !!pid && can("property.tenants.view", pid);
  const canAccounts = !!pid && can("tenant_relation.tenant_users.view", pid);
  const tenants = useAll<Tenant>("tenants", { property_id: pid || undefined }, { enabled: canTenants });
  const accounts = useAll<TenantUser>("tenant-users", { property_id: pid || undefined, status: "active" }, { enabled: canAccounts });
  // akun penerima yang relevan: pemegang akses unit terpilih atau anggota tenant terpilih
  const accountOptions = (accounts.data ?? []).filter((a) => (!f.unit_location_id && !f.tenant_id) || (f.unit_location_id && a.access.some((x) => x.id === f.unit_location_id)) || (f.tenant_id && a.tenant_id === f.tenant_id));
  const hasRecipient = !!f.unit_location_id || !!f.tenant_id || !!f.recipient_user_id;
  const valid = !!pid && hasRecipient;
  const set = <K extends keyof typeof f>(k: K, v: (typeof f)[K]) => setF((s) => ({ ...s, [k]: v }));
  const submit = async () => {
    if (!valid) return;
    setBusy("Menyimpan…");
    try {
      const receivedISO = f.received_at !== initialReceived ? localInputToISO(f.received_at, tz) : "";
      const p = await api<Package>("packages", {
        body: {
          property_id: pid, unit_location_id: f.unit_location_id, tenant_id: f.tenant_id || null, recipient_user_id: f.recipient_user_id || null,
          recipient_name: f.recipient_name.trim() || null, package_type: f.package_type, courier: f.courier.trim() || null, tracking_number: f.tracking_number.trim() || null,
          description: f.description.trim() || null, storage_location: f.storage_location.trim() || null, received_at: receivedISO || null, notify: f.notify,
        },
        idempotencyKey: uuid(),
      });
      if (photos.length) {
        try {
          await uploadAll(photos, p.id, "photo", setBusy);
        } catch (e) {
          toast.failed("uploaded", e, "Foto paket");
        }
      }
      invalidate("list", "all", "attachments");
      toast.success(`Paket ${p.package_number} dicatat${f.notify ? " — tenant diberi tahu" : ""}`);
      onCreated(p);
      onClose();
    } catch (e) {
      toast.failed("created", e, "Paket");
    } finally {
      setBusy(null);
    }
  };
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent side="right" title="Catat Paket Masuk" description="Penerima minimal salah satu: unit, tenant, atau akun Tenant App. Tenant terisi otomatis dari unit bila kosong.">
        <div className="space-y-4">
          <PropertyField value={pid} onChange={(v) => { setPid(v); setF((s) => ({ ...s, unit_location_id: null, unit_label: "", tenant_id: "", recipient_user_id: "" })); }} />
          <Field label="Unit penerima">
            <LocationPicker propertyId={pid || null} value={f.unit_location_id} allowTypes={["unit"]} placeholder="Pilih unit…" onChange={(id, node) => setF((s) => ({ ...s, unit_location_id: id, unit_label: node?.name ?? "", recipient_user_id: "" }))} />
          </Field>
          {canTenants && (
            <Field label="Tenant" help="Opsional — otomatis dari unit bila kosong.">
              <NativeSelect value={f.tenant_id} onChange={(e) => setF((s) => ({ ...s, tenant_id: e.target.value, recipient_user_id: "" }))}>
                <option value="">— Otomatis / tanpa tenant —</option>
                {(tenants.data ?? []).map((t) => <option key={t.id} value={t.id}>{t.name} ({t.tenant_code})</option>)}
              </NativeSelect>
            </Field>
          )}
          {canAccounts && (
            <Field label="Akun Tenant App penerima" help="Opsional — notifikasi langsung ke akun ini; nama penerima terisi otomatis.">
              <NativeSelect value={f.recipient_user_id} onChange={(e) => { const acc = accountOptions.find((a) => a.user_id === e.target.value); setF((s) => ({ ...s, recipient_user_id: e.target.value, recipient_name: acc && !s.recipient_name ? acc.full_name : s.recipient_name })); }}>
                <option value="">— Tidak dipilih —</option>
                {accountOptions.map((a) => <option key={a.user_id} value={a.user_id}>{a.full_name}{a.tenant_name ? ` · ${a.tenant_name}` : ""}</option>)}
              </NativeSelect>
            </Field>
          )}
          <Field label="Nama penerima (tertulis di paket)" help="Kosongkan untuk memakai nama akun / tenant."><Input value={f.recipient_name} onChange={(e) => set("recipient_name", e.target.value)} maxLength={150} /></Field>
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <Field label="Jenis"><PackageTypeSelect value={f.package_type} onChange={(v) => set("package_type", v)} /></Field>
            <Field label="Kurir"><CourierInput value={f.courier} onChange={(v) => set("courier", v)} /></Field>
            <Field label="Nomor resi"><Input value={f.tracking_number} onChange={(e) => set("tracking_number", e.target.value)} className="font-mono" maxLength={80} /></Field>
            <Field label="Lokasi simpan" help="mis. Rak A-3 lobby, lemari resepsionis."><Input value={f.storage_location} onChange={(e) => set("storage_location", e.target.value)} maxLength={120} /></Field>
            <Field label="Diterima" help={`Zona waktu ${tz}.`}><Input type="datetime-local" value={f.received_at} onChange={(e) => set("received_at", e.target.value)} /></Field>
          </div>
          <Field label="Deskripsi"><Textarea rows={2} value={f.description} onChange={(e) => set("description", e.target.value)} placeholder="mis. kardus sedang, label fragile" /></Field>
          <PhotoPicker files={photos} onChange={setPhotos} />
          <Checkbox label="Beri tahu tenant sekarang (notifikasi Tenant App)" checked={f.notify} onCheckedChange={(v) => set("notify", v)} />
          {!hasRecipient && <p className="text-xs text-on-surface-variant">Pilih unit, tenant, atau akun penerima.</p>}
        </div>
        <DialogFooter className="flex-wrap">
          <span className="mr-auto self-center text-xs text-on-surface-variant">{busy}</span>
          <Button variant="secondary" onClick={onClose}>Batal</Button>
          <Button icon="inventory_2" disabled={!valid} loading={!!busy} onClick={submit}>Catat paket</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

// ---------- Edit ----------
export function EditPackageDialog({ pkg, onClose }: { pkg: Package; onClose: () => void }) {
  const toast = useToast();
  const invalidate = useInvalidate();
  const [f, setF] = useState({ recipient_name: pkg.recipient_name, package_type: pkg.package_type, courier: pkg.courier ?? "", tracking_number: pkg.tracking_number ?? "", description: pkg.description ?? "", storage_location: pkg.storage_location ?? "" });
  const [busy, setBusy] = useState(false);
  const submit = async () => {
    setBusy(true);
    try {
      await api<Package>(`packages/${pkg.id}`, { method: "PATCH", body: { recipient_name: f.recipient_name.trim(), package_type: f.package_type, courier: f.courier.trim(), tracking_number: f.tracking_number.trim(), description: f.description.trim(), storage_location: f.storage_location.trim() } });
      invalidate("list", "one");
      toast.action("saved", pkg.package_number);
      onClose();
    } catch (e) {
      toast.failed("saved", e, pkg.package_number);
    } finally {
      setBusy(false);
    }
  };
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent side="right" title={`Edit · ${pkg.package_number}`}>
        <div className="space-y-4">
          <Field label="Nama penerima" required><Input value={f.recipient_name} onChange={(e) => setF({ ...f, recipient_name: e.target.value })} maxLength={150} /></Field>
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <Field label="Jenis"><PackageTypeSelect value={f.package_type} onChange={(v) => setF({ ...f, package_type: v })} /></Field>
            <Field label="Kurir"><CourierInput value={f.courier} onChange={(v) => setF({ ...f, courier: v })} /></Field>
            <Field label="Nomor resi"><Input value={f.tracking_number} onChange={(e) => setF({ ...f, tracking_number: e.target.value })} className="font-mono" /></Field>
            <Field label="Lokasi simpan"><Input value={f.storage_location} onChange={(e) => setF({ ...f, storage_location: e.target.value })} /></Field>
          </div>
          <Field label="Deskripsi"><Textarea rows={2} value={f.description} onChange={(e) => setF({ ...f, description: e.target.value })} /></Field>
        </div>
        <DialogFooter>
          <Button variant="secondary" onClick={onClose}>Batal</Button>
          <Button disabled={!f.recipient_name.trim()} loading={busy} onClick={submit}>Simpan</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

// ---------- Serah terima (P3-PKG-03) ----------
export function PickupDialog({ pkg, onClose }: { pkg: Package; onClose: () => void }) {
  const toast = useToast();
  const invalidate = useInvalidate();
  const [f, setF] = useState({ picked_up_by_name: pkg.recipient_name, note: "" });
  const [sig, setSig] = useState<Attachment | null>(null);
  const [savingSig, setSavingSig] = useState(false);
  const [photos, setPhotos] = useState<File[]>([]);
  const [uploaded, setUploaded] = useState(0);
  const [busy, setBusy] = useState<string | null>(null);
  const saveSignature = async (png: Blob) => {
    setSavingSig(true);
    try {
      setSig(await uploadAttachment(signatureFile(png), "package", pkg.id, "signature", undefined, { compress: false }));
      toast.action("saved", "Tanda tangan");
    } catch (e) {
      toast.failed("saved", e, "Tanda tangan");
    } finally {
      setSavingSig(false);
    }
  };
  const submit = async () => {
    const name = f.picked_up_by_name.trim();
    if (!name) return;
    try {
      if (photos.length) {
        await uploadAll(photos, pkg.id, "photo_after", setBusy);
        // sudah terlampir ke paket — kirim ulang tidak mengunggah dua kali
        setUploaded((n) => n + photos.length);
        setPhotos([]);
      }
      setBusy("Menyimpan serah terima…");
      const r = await api<Package>(`packages/${pkg.id}/pickup`, { body: { picked_up_by_name: name, note: f.note.trim(), signature_attachment_id: sig?.id ?? null }, idempotencyKey: uuid() });
      invalidate("list", "one", "activities", "attachments");
      toast.success(`${r.package_number} diserahkan ke ${r.picked_up_by_name ?? name}`);
      onClose();
    } catch (e) {
      toast.failed("updated", e, pkg.package_number);
    } finally {
      setBusy(null);
    }
  };
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent side="right" title={`Serah terima · ${pkg.package_number}`} description="Pastikan pengambil adalah penerima atau orang yang diberi kuasa oleh tenant.">
        <div className="space-y-4">
          <PrivacyNote>Tanda tangan & foto serah terima tersimpan sebagai bukti — hanya terlihat oleh staf berwenang dan tenant penerima.</PrivacyNote>
          <Field label="Diambil oleh" required><Input value={f.picked_up_by_name} autoFocus onChange={(e) => setF({ ...f, picked_up_by_name: e.target.value })} maxLength={150} /></Field>
          <Field label="Catatan"><Textarea rows={2} value={f.note} onChange={(e) => setF({ ...f, note: e.target.value })} placeholder="mis. diambil oleh ART unit, menunjukkan KTP" /></Field>
          <Field label="Tanda tangan pengambil" help={sig ? undefined : "Opsional, dianjurkan — minta pengambil menandatangani lalu tekan Simpan."}>
            {sig ? (
              <div className="flex flex-wrap items-center gap-3">
                {sig.url ? <img src={sig.url} alt="Tanda tangan pengambil" className="h-24 rounded-[var(--radius-md)] border border-border bg-surface object-contain" /> : <span className="inline-flex items-center gap-1 text-sm"><Icon name="draw" size={16} aria-hidden />Tanda tangan tersimpan</span>}
                <Button size="sm" variant="ghost" icon="replay" className={TOUCH} onClick={() => setSig(null)}>Ulangi</Button>
              </div>
            ) : (
              <SignaturePad onSave={saveSignature} saving={savingSig} height={150} />
            )}
          </Field>
          <PhotoPicker files={photos} onChange={setPhotos} max={Math.max(0, 2 - uploaded)} label="Foto serah terima" />
          {uploaded > 0 && <p className="-mt-2 text-xs text-on-surface-variant">{uploaded} foto serah terima sudah terunggah.</p>}
        </div>
        <DialogFooter className="flex-wrap">
          <span className="mr-auto self-center text-xs text-on-surface-variant">{busy}</span>
          <Button variant="secondary" onClick={onClose}>Batal</Button>
          <Button icon="handshake" disabled={!f.picked_up_by_name.trim() || savingSig} loading={!!busy} onClick={submit}>Serahkan paket</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

// ---------- Retur ----------
export function ReturnPackageDialog({ pkg, onClose }: { pkg: Package; onClose: () => void }) {
  const toast = useToast();
  const invalidate = useInvalidate();
  const [reason, setReason] = useState("");
  const [busy, setBusy] = useState(false);
  const submit = async () => {
    setBusy(true);
    try {
      const r = await api<Package>(`packages/${pkg.id}/return`, { body: { reason: reason.trim() }, idempotencyKey: uuid() });
      invalidate("list", "one", "activities");
      toast.success(`${r.package_number} dikembalikan`);
      onClose();
    } catch (e) {
      toast.failed("updated", e, pkg.package_number);
    } finally {
      setBusy(false);
    }
  };
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent title={`Retur paket · ${pkg.package_number}`} description="Paket dikembalikan ke kurir/pengirim; tenant diberi tahu. Status tidak dapat dikembalikan.">
        <Field label="Alasan retur" required><Textarea rows={3} autoFocus value={reason} onChange={(e) => setReason(e.target.value)} placeholder="mis. penerima tidak dikenal / ditolak tenant / tidak diambil 14 hari" /></Field>
        <DialogFooter>
          <Button variant="secondary" onClick={onClose}>Batal</Button>
          <Button variant="destructive" icon="assignment_return" disabled={!reason.trim()} loading={busy} onClick={submit}>Retur paket</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

