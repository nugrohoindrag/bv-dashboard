// Form Pengumuman & Broadcast darurat (PRD P3 v2.1 P3-ANN-02..06, P3-BRC-01): kategori (Pengumuman · News · Alert), tingkat
// (Alert), judul/ringkasan/isi, gambar (image_attachment_id), audiens, sasaran lokasi (building/tower/lantai/area/unit — mencakup
// subtree) & tenant tertentu, wajib konfirmasi baca, jadwal publish & kedaluwarsa (zona waktu property → RFC3339).
// Target pengumuman yang sudah terbit tidak dapat diubah (server 409) sehingga tidak dikirim saat edit.
import { useEffect, useRef, useState } from "react";
import { Icon } from "@buildingvision/ui";
import { useQuery } from "@tanstack/react-query";
import { Alert, Badge, Button, Checkbox, Dialog, DialogContent, DialogFooter, Field, Input, NativeSelect, Segmented, Textarea } from "@/components/ui/primitives";
import { ComboBox, LocationPicker } from "@/components/bv/pickers";
import { useToast } from "@/components/bv/common";
import { uploadAttachment, useAll, useInvalidate } from "@/api/hooks";
import { api, uuid } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { fmtDateTime, getTimezone } from "@/lib/format";
import type { Attachment, Tenant } from "@/api/types";
import type { Announcement, AnnouncementTarget } from "./types";
import { ANNOUNCEMENT_AUDIENCES, ANNOUNCEMENT_CATEGORIES, ANNOUNCEMENT_SEVERITIES, LOCATION_TYPE_LABELS, TARGET_LOCATION_TYPES, labelOf, optionsOf } from "./labels";
import { isoToLocalInput, localInputToISO } from "./utils";
import { useNow } from "./hooks";

const NIL_UUID = "00000000-0000-0000-0000-000000000000";
const IMAGE_ACCEPT = "image/jpeg,image/png,image/webp";

// ---------- Sasaran (lokasi subtree + tenant) ----------
export function TargetPicker({ propertyId, targets, onChange, disabled }: { propertyId: string; targets: AnnouncementTarget[]; onChange: (t: AnnouncementTarget[]) => void; disabled?: boolean }) {
  const { can } = useAuth();
  const tenants = useAll<Tenant>("tenants", { property_id: propertyId }, { enabled: can("property.tenants.view", propertyId) });
  const locs = targets.filter((t) => t.kind === "location");
  const tens = targets.filter((t) => t.kind === "tenant");
  const add = (t: AnnouncementTarget) => { if (!targets.some((x) => x.kind === t.kind && x.id === t.id)) onChange([...targets, t]); };
  const remove = (t: AnnouncementTarget) => onChange(targets.filter((x) => !(x.kind === t.kind && x.id === t.id)));
  const available = (tenants.data ?? []).filter((x) => !tens.some((t) => t.id === x.id));
  return (
    <div className="space-y-3">
      <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
        <Field label="Tambah lokasi" help="Building, tower, lantai, area, atau unit — mencakup seluruh unit di bawahnya.">
          <LocationPicker propertyId={propertyId} value={null} allowTypes={TARGET_LOCATION_TYPES} disabled={disabled} placeholder="Pilih lokasi…" onChange={(id, node) => { if (id) add({ kind: "location", id, label: node?.path_text || node?.name || id, type: node?.location_type }); }} />
        </Field>
        <Field label="Tambah tenant" help="Seluruh akun Tenant App milik tenant tersebut.">
          <ComboBox<Tenant>
            items={available}
            value={null}
            onChange={(id, item) => { if (id && item) add({ kind: "tenant", id, label: item.name }); }}
            render={(t) => <span className="flex flex-col"><span>{t.name}</span><span className="text-xs text-on-surface-variant">{t.tenant_code}{t.units.length ? ` · Unit ${t.units.map((u) => u.unit_number).join(", ")}` : ""}</span></span>}
            label={(t) => t.name}
            placeholder={tenants.isLoading ? "Memuat tenant…" : "Pilih tenant…"}
            filter={(t, q) => t.name.toLowerCase().includes(q) || t.tenant_code.toLowerCase().includes(q)}
            loading={tenants.isLoading}
            disabled={disabled || !can("property.tenants.view", propertyId)}
          />
        </Field>
      </div>
      {targets.length === 0 ? (
        <p className="flex items-center gap-1.5 text-sm text-on-surface-variant"><Icon name="groups" size={16} aria-hidden />Tanpa sasaran khusus — dikirim ke seluruh tenant di property.</p>
      ) : (
        <ul className="flex flex-wrap gap-1.5" aria-label="Sasaran terpilih">
          {[...locs, ...tens].map((t) => (
            <li key={`${t.kind}-${t.id}`} className="inline-flex max-w-full items-center gap-1 rounded-full bg-surface-container-high py-0.5 pl-2 pr-1 text-xs text-on-surface">
              <Icon name={t.kind === "tenant" ? "business" : "location_on"} size={12} aria-hidden />
              <span className="truncate" title={t.kind === "location" && t.type ? `${labelOf(LOCATION_TYPE_LABELS, t.type)} · ${t.label}` : t.label}>{t.label}</span>
              {!disabled && <button type="button" aria-label={`Hapus sasaran ${t.label}`} onClick={() => remove(t)} className="inline-flex rounded-full p-0.5 hover:bg-surface-container"><Icon name="close" size={12} /></button>}
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

/** Pratinjau gambar pengumuman (GET /attachments/{id}). */
export function AnnouncementImage({ attachmentId, className }: { attachmentId: string; className?: string }) {
  const q = useQuery({ queryKey: ["attachment", attachmentId], queryFn: ({ signal }) => api<Attachment>(`attachments/${attachmentId}`, { signal }), staleTime: 5 * 60_000, retry: false });
  if (q.isLoading) return <div className={className ?? "h-40 w-full animate-pulse rounded-[var(--radius-md)] bg-surface-container"} />;
  if (!q.data?.url) return <p className="inline-flex items-center gap-1.5 text-sm text-on-surface-variant"><Icon name="hide_image" size={16} aria-hidden />Gambar terlampir (pratinjau tidak tersedia)</p>;
  return <img src={q.data.url} alt="Gambar pengumuman" className={className ?? "max-h-56 w-full rounded-[var(--radius-md)] border border-border object-cover"} />;
}

function ImageField({ current, file, removed, onPick, onRemove, onUndo }: { current: string | null; file: File | null; removed: boolean; onPick: (f: File) => void; onRemove: () => void; onUndo: () => void }) {
  const ref = useRef<HTMLInputElement>(null);
  const [preview, setPreview] = useState<string | null>(null);
  // object URL pratinjau dibebaskan saat diganti / komponen ditutup
  useEffect(() => () => { if (preview) URL.revokeObjectURL(preview); }, [preview]);
  const pick = (f: File | undefined) => {
    if (!f) return;
    setPreview(URL.createObjectURL(f));
    onPick(f);
  };
  return (
    <Field label="Gambar" help="Opsional · JPG/PNG/WebP, dikompres otomatis. Tampil di kartu & detail pengumuman Tenant App.">
      <div className="space-y-2">
        {file && preview ? <img src={preview} alt="Pratinjau gambar baru" className="max-h-44 w-full rounded-[var(--radius-md)] border border-border object-cover" /> : current && !removed ? <AnnouncementImage attachmentId={current} className="max-h-44 w-full rounded-[var(--radius-md)] border border-border object-cover" /> : null}
        <div className="flex flex-wrap items-center gap-2">
          <input ref={ref} type="file" accept={IMAGE_ACCEPT} className="hidden" onChange={(e) => { pick(e.target.files?.[0]); e.target.value = ""; }} />
          <Button type="button" size="sm" variant="secondary" icon="photo_camera" onClick={() => ref.current?.click()}>{file || (current && !removed) ? "Ganti gambar" : "Pilih gambar"}</Button>
          {(file || (current && !removed)) && <Button type="button" size="sm" variant="ghost" icon="delete" onClick={() => { setPreview(null); onRemove(); }}>Hapus gambar</Button>}
          {removed && !file && current && <Button type="button" size="sm" variant="ghost" icon="replay" onClick={onUndo}>Batalkan hapus</Button>}
          {file && <span className="truncate text-xs text-on-surface-variant">{file.name}</span>}
        </div>
      </div>
    </Field>
  );
}

// ---------- Form pengumuman (buat / edit) ----------
export function AnnouncementFormDialog({ item, propertyId, onClose, onSaved }: { item: Announcement | null; propertyId: string | null; onClose: () => void; onSaved?: (a: Announcement) => void }) {
  const toast = useToast();
  const invalidate = useInvalidate();
  const { can } = useAuth();
  const tz = getTimezone();
  const now = useNow(30_000);
  const published = item?.status === "published";
  const readOnly = !!item && !item.allowed_actions.includes("update");
  const canOrgWide = !item && can("platform.organizations.update");
  const [f, setF] = useState({
    category: item?.category ?? "announcement",
    severity: item?.severity ?? "info",
    title: item?.title ?? "",
    excerpt: item?.excerpt ?? "",
    body: item?.body ?? "",
    audience: item?.audience ?? "tenant",
    importance: item?.importance ?? "normal",
    requires_ack: item?.requires_ack ?? false,
    publish_at: isoToLocalInput(item?.publish_at, tz),
    expires_at: isoToLocalInput(item?.expires_at, tz),
    orgWide: item ? item.property_id === null : !propertyId,
  });
  const [targets, setTargets] = useState<AnnouncementTarget[]>(item?.targets ?? []);
  const [imageFile, setImageFile] = useState<File | null>(null);
  const [removeImage, setRemoveImage] = useState(false);
  const [busy, setBusy] = useState<string | null>(null);
  const [err, setErr] = useState<string | null>(null);
  const scopePid = item ? item.property_id : f.orgWide ? null : propertyId;
  const publishISO = localInputToISO(f.publish_at, tz);
  const expiresISO = localInputToISO(f.expires_at, tz);
  const futurePublish = !!publishISO && new Date(publishISO).getTime() > now;
  const canScheduleAfterSave = futurePublish && (!item || item.status === "draft") && (item ? item.allowed_actions.includes("schedule") : can("tenant_relation.announcements.publish", scopePid ?? undefined));
  const valid = !!f.title.trim() && !!f.body.trim() && (!!item || (f.orgWide ? canOrgWide : !!propertyId));

  const save = async (andSchedule: boolean) => {
    setErr(null);
    if (!valid) return;
    if (publishISO && expiresISO && new Date(expiresISO) <= new Date(publishISO)) return setErr("Kedaluwarsa harus setelah jadwal publish.");
    if (!item && f.orgWide && !canOrgWide) return setErr("Pilih property di header untuk membuat pengumuman.");
    const common: Record<string, unknown> = {
      title: f.title.trim(), excerpt: f.excerpt.trim(), body: f.body.trim(), audience: f.audience, importance: f.importance,
      category: f.category, severity: f.category === "alert" ? f.severity : "info", requires_ack: f.requires_ack,
    };
    try {
      let a: Announcement;
      if (item) {
        setBusy("Menyimpan…");
        const body: Record<string, unknown> = { ...common };
        // hanya kirim waktu yang diubah ("" = kosongkan); jadwal pengumuman yang sudah terbit tidak diubah
        if (!published && f.publish_at !== isoToLocalInput(item.publish_at, tz)) body.publish_at = publishISO;
        if (f.expires_at !== isoToLocalInput(item.expires_at, tz)) body.expires_at = expiresISO;
        if (!published && scopePid) {
          body.target_location_ids = targets.filter((t) => t.kind === "location").map((t) => t.id);
          body.target_tenant_ids = targets.filter((t) => t.kind === "tenant").map((t) => t.id);
        }
        if (removeImage && !imageFile && item.image_attachment_id) body.image_attachment_id = NIL_UUID;
        a = await api<Announcement>(`announcements/${item.id}`, { method: "PATCH", body, ifMatch: item.version });
      } else {
        setBusy("Menyimpan…");
        const body: Record<string, unknown> = { ...common, excerpt: f.excerpt.trim() || null, property_id: scopePid, publish_at: publishISO || undefined, expires_at: expiresISO || undefined };
        if (scopePid) {
          body.target_location_ids = targets.filter((t) => t.kind === "location").map((t) => t.id);
          body.target_tenant_ids = targets.filter((t) => t.kind === "tenant").map((t) => t.id);
        }
        a = await api<Announcement>("announcements", { body, idempotencyKey: uuid() });
      }
      if (imageFile) {
        try {
          setBusy("Mengunggah gambar…");
          const att = await uploadAttachment(imageFile, "announcement", a.id, "photo");
          a = await api<Announcement>(`announcements/${a.id}`, { method: "PATCH", body: { image_attachment_id: att.id }, ifMatch: a.version });
        } catch (e) {
          toast.failed("uploaded", e, "Gambar pengumuman");
        }
      }
      if (andSchedule && a.allowed_actions.includes("schedule")) {
        setBusy("Menjadwalkan…");
        a = await api<Announcement>(`announcements/${a.id}/schedule`, { body: {}, idempotencyKey: uuid() });
        toast.success(`Pengumuman dijadwalkan terbit ${fmtDateTime(a.publish_at)}`);
      } else {
        toast.action(item ? "saved" : "created", item ? "Announcement" : "Draft announcement");
      }
      invalidate("list", "one", "attachment");
      onSaved?.(a);
      onClose();
    } catch (e) {
      const c = (e as { code?: string }).code;
      setErr(c === "STALE_VERSION" || c === "VERSION_CONFLICT" ? "Pengumuman sudah diubah orang lain — tutup lalu buka kembali untuk memuat versi terbaru." : (e as Error).message);
    } finally {
      setBusy(null);
    }
  };

  const set = <K extends keyof typeof f>(k: K, v: (typeof f)[K]) => setF((s) => ({ ...s, [k]: v }));
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent side="right" title={item ? `Edit Announcement` : "Buat Announcement"} description={item ? (published ? "Sudah terbit — isi & kedaluwarsa dapat diubah; sasaran tidak dapat diubah." : "Perubahan disimpan pada draft/jadwal.") : "Disimpan sebagai draft. Publikasikan atau jadwalkan agar tenant sasaran menerima notifikasi (in-app & push)."}>
        <div className="space-y-4">
          {err && <Alert variant="critical">{err}</Alert>}
          {readOnly && <Alert variant="info">Pengumuman ini tidak dapat diubah (status {item?.status}).</Alert>}
          <Field label="Kategori">
            <Segmented value={f.category} onChange={(v) => set("category", v)} disabled={readOnly} options={optionsOf(ANNOUNCEMENT_CATEGORIES).map((o) => ({ value: o.value, label: o.label }))} />
          </Field>
          {f.category === "alert" && (
            <Field label="Tingkat" help="Alert dikirim sebagai pemberitahuan penting (in-app + push).">
              <NativeSelect value={f.severity} onChange={(e) => set("severity", e.target.value)} disabled={readOnly}>{optionsOf(ANNOUNCEMENT_SEVERITIES).map((o) => <option key={o.value} value={o.value}>{o.label}</option>)}</NativeSelect>
            </Field>
          )}
          <Field label="Judul" required><Input value={f.title} onChange={(e) => set("title", e.target.value)} disabled={readOnly} maxLength={200} /></Field>
          <Field label="Ringkasan (tampil di kartu Home)"><Input value={f.excerpt} onChange={(e) => set("excerpt", e.target.value)} disabled={readOnly} maxLength={300} /></Field>
          <Field label="Isi" required><Textarea rows={7} value={f.body} onChange={(e) => set("body", e.target.value)} disabled={readOnly} /></Field>
          {!readOnly && <ImageField current={item?.image_attachment_id ?? null} file={imageFile} removed={removeImage} onPick={(file) => { setImageFile(file); setRemoveImage(false); }} onRemove={() => { setImageFile(null); setRemoveImage(true); }} onUndo={() => setRemoveImage(false)} />}
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <Field label="Audiens"><NativeSelect value={f.audience} onChange={(e) => set("audience", e.target.value)} disabled={readOnly}>{optionsOf(ANNOUNCEMENT_AUDIENCES).map((o) => <option key={o.value} value={o.value}>{o.label}</option>)}</NativeSelect></Field>
            <Field label="Prioritas"><NativeSelect value={f.importance} onChange={(e) => set("importance", e.target.value)} disabled={readOnly}><option value="normal">Normal</option><option value="important">Penting (Important Announcement)</option></NativeSelect></Field>
          </div>
          <Checkbox label="Wajib konfirmasi baca (tenant menekan “Saya sudah membaca”)" checked={f.requires_ack} onCheckedChange={(v) => set("requires_ack", v)} disabled={readOnly} />
          {canOrgWide && <Checkbox label="Berlaku untuk seluruh property organisasi (tanpa sasaran khusus)" checked={f.orgWide} onCheckedChange={(v) => { set("orgWide", v); if (v) setTargets([]); }} disabled={!propertyId} />}
          {!item && !propertyId && !canOrgWide && <Alert variant="warning">Pilih property di header untuk membuat pengumuman.</Alert>}
          <fieldset className="space-y-2 rounded-[var(--radius-md)] border border-border p-3">
            <legend className="px-1 text-xs font-semibold uppercase tracking-wide text-on-surface-variant">Sasaran</legend>
            {scopePid ? (
              published ? (
                <TargetSummary targets={item?.targets ?? []} />
              ) : (
                <TargetPicker propertyId={scopePid} targets={targets} onChange={setTargets} disabled={readOnly} />
              )
            ) : (
              <p className="text-sm text-on-surface-variant">Pengumuman lintas property dikirim ke seluruh tenant organisasi; sasaran lokasi/tenant hanya untuk pengumuman satu property.</p>
            )}
          </fieldset>
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <Field label="Jadwal publish" help={`Zona waktu ${tz}. Kosongkan untuk publish manual.`}><Input type="datetime-local" value={f.publish_at} onChange={(e) => set("publish_at", e.target.value)} disabled={readOnly || published} /></Field>
            <Field label="Kedaluwarsa" help="Setelah waktu ini pengumuman tidak tampil lagi di Tenant App."><Input type="datetime-local" value={f.expires_at} onChange={(e) => set("expires_at", e.target.value)} disabled={readOnly} /></Field>
          </div>
        </div>
        <DialogFooter className="flex-wrap">
          <span className="mr-auto self-center text-xs text-on-surface-variant">{busy}</span>
          <Button variant="secondary" onClick={onClose}>Batal</Button>
          {!readOnly && canScheduleAfterSave && <Button variant="secondary" icon="schedule" disabled={!valid} loading={!!busy} onClick={() => save(true)}>Simpan & jadwalkan</Button>}
          {!readOnly && <Button disabled={!valid} loading={!!busy} onClick={() => save(false)}>{item ? "Simpan" : "Simpan draft"}</Button>}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

export function TargetSummary({ targets }: { targets: AnnouncementTarget[] }) {
  if (!targets.length) return <p className="flex items-center gap-1.5 text-sm text-on-surface-variant"><Icon name="groups" size={16} aria-hidden />Seluruh tenant di property.</p>;
  return (
    <ul className="flex flex-wrap gap-1.5">
      {targets.map((t) => (
        <li key={`${t.kind}-${t.id}`}>
          <Badge title={t.kind === "location" && t.type ? `${labelOf(LOCATION_TYPE_LABELS, t.type)} · ${t.label}` : `Tenant · ${t.label}`}><Icon name={t.kind === "tenant" ? "business" : "location_on"} size={12} aria-hidden />{t.label}</Badge>
        </li>
      ))}
    </ul>
  );
}

// ---------- Broadcast darurat / operasional (P3-BRC-01) ----------
export function BroadcastDialog({ onClose, onSent }: { onClose: () => void; onSent?: (a: Announcement) => void }) {
  const toast = useToast();
  const invalidate = useInvalidate();
  const { propertyId, properties, can } = useAuth();
  const tz = getTimezone();
  const [pid, setPid] = useState(propertyId ?? (properties.length === 1 ? properties[0].id : ""));
  const [f, setF] = useState({ title: "", body: "", severity: "warning", requires_ack: true, expires_at: "" });
  const [targets, setTargets] = useState<AnnouncementTarget[]>([]);
  const [confirm, setConfirm] = useState(false);
  const [busy, setBusy] = useState(false);
  const allowed = !!pid && can("tenant_relation.announcements.broadcast", pid);
  const valid = allowed && !!f.title.trim() && !!f.body.trim();
  const send = async () => {
    setBusy(true);
    try {
      const a = await api<Announcement>("announcements/broadcast", {
        body: {
          property_id: pid, title: f.title.trim(), body: f.body.trim(), severity: f.severity, requires_ack: f.requires_ack,
          expires_at: localInputToISO(f.expires_at, tz) || null,
          target_location_ids: targets.filter((t) => t.kind === "location").map((t) => t.id),
          target_tenant_ids: targets.filter((t) => t.kind === "tenant").map((t) => t.id),
        },
        idempotencyKey: uuid(),
      });
      invalidate("list", "one", "tr-metrics");
      toast.success(`Broadcast terkirim ke ${a.recipients_count ?? 0} penerima`, { to: `/tenant-relation/announcements/${a.id}`, label: "Lihat" });
      onSent?.(a);
      onClose();
    } catch (e) {
      toast.failed("sent", e, "Broadcast");
      setConfirm(false);
    } finally {
      setBusy(false);
    }
  };
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent side="right" title="Broadcast darurat / operasional" description="Langsung terbit sebagai Alert dan dikirim ke tenant sasaran lewat in-app & push — mis. pemadaman listrik/air, gangguan lift, evakuasi.">
        <div className="space-y-4">
          {properties.length > 1 && (
            <Field label="Property" required>
              <NativeSelect value={pid} onChange={(e) => { setPid(e.target.value); setTargets([]); }}>
                {!pid && <option value="">Pilih property…</option>}
                {properties.map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}
              </NativeSelect>
            </Field>
          )}
          {pid && !allowed && <Alert variant="warning">Anda tidak memiliki izin broadcast pada property ini.</Alert>}
          <Field label="Tingkat">
            <Segmented value={f.severity} onChange={(v) => setF({ ...f, severity: v })} options={optionsOf(ANNOUNCEMENT_SEVERITIES)} />
          </Field>
          <Field label="Judul" required><Input value={f.title} onChange={(e) => setF({ ...f, title: e.target.value })} maxLength={200} placeholder="mis. Pemadaman listrik Tower A" autoFocus /></Field>
          <Field label="Isi" required><Textarea rows={5} value={f.body} onChange={(e) => setF({ ...f, body: e.target.value })} placeholder="mis. PLN melakukan pemeliharaan pukul 13.00–15.00. Lift beroperasi dengan genset; mohon hemat pemakaian." /></Field>
          <Checkbox label="Wajib konfirmasi baca" checked={f.requires_ack} onCheckedChange={(v) => setF({ ...f, requires_ack: v })} />
          <Field label="Kedaluwarsa" help={`Opsional · zona waktu ${tz}.`}><Input type="datetime-local" value={f.expires_at} onChange={(e) => setF({ ...f, expires_at: e.target.value })} /></Field>
          <fieldset className="space-y-2 rounded-[var(--radius-md)] border border-border p-3">
            <legend className="px-1 text-xs font-semibold uppercase tracking-wide text-on-surface-variant">Sasaran</legend>
            {pid ? <TargetPicker propertyId={pid} targets={targets} onChange={setTargets} /> : <p className="text-sm text-on-surface-variant">Pilih property terlebih dahulu.</p>}
          </fieldset>
        </div>
        <DialogFooter>
          <Button variant="secondary" onClick={onClose}>Batal</Button>
          <Button variant="destructive" icon="emergency_home" disabled={!valid} onClick={() => setConfirm(true)}>Kirim broadcast</Button>
        </DialogFooter>
        {confirm && (
          <Dialog open onOpenChange={(o) => !o && setConfirm(false)}>
            <DialogContent title="Kirim broadcast sekarang?" description={`"${f.title.trim()}" langsung terbit dan dikirim ke ${targets.length ? `${targets.length} sasaran terpilih` : "seluruh tenant property"} (in-app & push). Tindakan ini tidak dapat dibatalkan — arsipkan bila perlu menghentikan tampilannya.`}>
              <DialogFooter>
                <Button variant="secondary" onClick={() => setConfirm(false)}>Periksa lagi</Button>
                <Button variant="destructive" icon="campaign" loading={busy} onClick={send}>Ya, kirim</Button>
              </DialogFooter>
            </DialogContent>
          </Dialog>
        )}
      </DialogContent>
    </Dialog>
  );
}
