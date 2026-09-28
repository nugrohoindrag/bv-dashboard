// Incident lengkap (PRD P2 v2.1 §6.3): investigasi (P2-SIN-06), orang terlibat (P2-SIN-03 — data pribadi, akses diaudit),
// video evidence (P2-SIN-04: mp4/mov/webm ≤ 50 MB tanpa kompresi). Tombol & kartu mengikuti allowed_actions server:
// investigate · view_people · manage_people · attach.
import { useCallback, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useDropzone, type FileRejection } from "react-dropzone";
import { Icon } from "@buildingvision/ui";
import type { Tone } from "@buildingvision/ui/bv";
import { Badge, Button, Card, CardContent, CardHeader, CardTitle, ConfirmDialog, Dialog, DialogContent, DialogFooter, Field, Input, NativeSelect, Textarea } from "@/components/ui/primitives";
import { AsyncState, KeyValue, useToast } from "@/components/bv/common";
import { UserPicker } from "@/components/bv/pickers";
import { uploadAttachment, useInvalidate, useUpdate } from "@/api/hooks";
import { api, uuid, type ListResponse } from "@/lib/api";
import { fieldErrorsOf, problemOf } from "@/lib/problem";
import { fmtDateTime } from "@/lib/format";
import { cn } from "@/lib/utils";
import type { Attachment, Incident, IncidentPerson } from "@/api/types";
import { MAX_VIDEO_BYTES, PERSON_ROLES, VIDEO_ACCEPT, labelOf, optionsOf, validateVideoFile } from "@/features/security/p2";
import { PrivacyNote } from "@/features/security/shared";
import { StatusBadge } from "@/components/bv/badges";
import { statusOptions } from "@/lib/status";
import { TOUCH } from "@/features/security/hooks";
import { errMessage } from "./dialogs";

const TERMINAL = ["closed", "cancelled"];

// ---------- Investigasi (PATCH /incidents/{id}, If-Match version) ----------
export function InvestigationCard({ incident }: { incident: Incident }) {
  const [open, setOpen] = useState(false);
  const inv = incident.investigation;
  const status = inv?.status ?? "not_started";
  const canEdit = incident.allowed_actions.includes("investigate") && !TERMINAL.includes(incident.status);
  // tanpa hak investigasi: kartu tampil setelah investigasi dimulai
  if (!canEdit && status === "not_started") return null;
  const started = status !== "not_started" || !!inv?.findings || !!inv?.investigator_user_id;
  return (
    <Card>
      <CardHeader>
        <CardTitle>Investigasi</CardTitle>
        <div className="flex items-center gap-2">
          <StatusBadge objectType="investigation" status={status} />
          {canEdit && <Button variant="link" size="sm" onClick={() => setOpen(true)}>{started ? "Edit" : "Mulai investigasi"}</Button>}
        </div>
      </CardHeader>
      <CardContent>
        {!started ? (
          <p className="text-sm italic text-muted-foreground">Investigasi belum dimulai. Catat investigator, temuan, akar masalah, dan tindakan korektif.</p>
        ) : (
          <KeyValue items={[
            { label: "Investigator", value: inv?.investigator_name ?? "—" },
            { label: "Temuan", value: inv?.findings ? <span className="whitespace-pre-line">{inv.findings}</span> : "—" },
            { label: "Akar masalah", value: inv?.root_cause ? <span className="whitespace-pre-line">{inv.root_cause}</span> : "—" },
            { label: "Tindakan korektif", value: inv?.corrective_action ? <span className="whitespace-pre-line">{inv.corrective_action}</span> : "—" },
            { label: "PIC korektif", value: inv?.corrective_owner_name ?? "—" },
            { label: "Selesai", value: inv?.investigated_at ? fmtDateTime(inv.investigated_at) : "—" },
          ]} />
        )}
      </CardContent>
      {open && <InvestigationDialog incident={incident} onClose={() => setOpen(false)} />}
    </Card>
  );
}

/** Pesan field server ("wajib") → kalimat untuk pengguna. */
function fieldText(msg: string): string {
  return msg === "wajib" ? "Wajib diisi sebelum investigasi ditandai Selesai." : msg;
}

function InvestigationDialog({ incident, onClose }: { incident: Incident; onClose: () => void }) {
  const toast = useToast();
  const inv = incident.investigation;
  const update = useUpdate<{ id: string; version: number } & Record<string, unknown>>("incidents");
  const initial = {
    investigation_status: inv?.status && inv.status !== "not_started" ? inv.status : "in_progress",
    investigator_user_id: inv?.investigator_user_id ?? null,
    investigation_findings: inv?.findings ?? "",
    root_cause: inv?.root_cause ?? "",
    corrective_action: inv?.corrective_action ?? "",
    corrective_owner_user_id: inv?.corrective_owner_user_id ?? null,
  };
  const [f, setF] = useState(initial);
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [formError, setFormError] = useState<string | null>(null);
  const completing = f.investigation_status === "completed";
  const submit = async () => {
    setErrors({});
    setFormError(null);
    // selaras validasi server: Selesai wajib akar masalah & tindakan korektif
    if (completing) {
      const e: Record<string, string> = {};
      if (!f.root_cause.trim()) e.root_cause = fieldText("wajib");
      if (!f.corrective_action.trim()) e.corrective_action = fieldText("wajib");
      if (Object.keys(e).length) return setErrors(e);
    }
    // kirim hanya field yang berubah (status selalu) agar audit investigasi ringkas
    const body: Record<string, unknown> = { investigation_status: f.investigation_status };
    for (const k of ["investigation_findings", "root_cause", "corrective_action"] as const) if (f[k].trim() !== initial[k].trim()) body[k] = f[k].trim();
    for (const k of ["investigator_user_id", "corrective_owner_user_id"] as const) if (f[k] && f[k] !== initial[k]) body[k] = f[k];
    try {
      await update.mutateAsync({ id: incident.id, version: incident.version, ...body });
      toast.action("saved", `Investigasi ${incident.incident_number}`);
      onClose();
    } catch (err) {
      const fe = fieldErrorsOf(err);
      setErrors(Object.fromEntries(Object.entries(fe).map(([k, v]) => [k, fieldText(v)])));
      setFormError(problemOf(err).code === "STALE_VERSION" ? "Incident sudah diubah pengguna lain. Tutup dialog, muat ulang halaman, lalu simpan kembali." : errMessage(err));
    }
  };
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent side="right" title={`Investigasi · ${incident.incident_number}`} description="Temuan, akar masalah, dan tindakan korektif. Perubahan tercatat di Activity.">
        <div className="space-y-4">
          {formError && <p role="alert" className="rounded-[var(--radius-md)] bg-error-container px-3 py-2 text-sm text-on-error-container">{formError}</p>}
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <Field label="Status investigasi" required error={errors.investigation_status}>
              <NativeSelect value={f.investigation_status} onChange={(e) => setF({ ...f, investigation_status: e.target.value })}>{statusOptions("investigation").map((o) => <option key={o.value} value={o.value}>{o.label}</option>)}</NativeSelect>
            </Field>
            <Field label="Investigator" error={errors.investigator_user_id}><UserPicker propertyId={incident.property_id} value={f.investigator_user_id} onChange={(v) => setF({ ...f, investigator_user_id: v })} placeholder="Pilih investigator…" /></Field>
          </div>
          <Field label="Temuan investigasi" error={errors.investigation_findings}><Textarea rows={4} value={f.investigation_findings} onChange={(e) => setF({ ...f, investigation_findings: e.target.value })} placeholder="Kronologi, bukti CCTV, keterangan saksi…" /></Field>
          <Field label="Akar masalah" required={completing} error={errors.root_cause}><Textarea rows={3} aria-invalid={!!errors.root_cause || undefined} value={f.root_cause} onChange={(e) => setF({ ...f, root_cause: e.target.value })} /></Field>
          <Field label="Tindakan korektif" required={completing} error={errors.corrective_action}><Textarea rows={3} aria-invalid={!!errors.corrective_action || undefined} value={f.corrective_action} onChange={(e) => setF({ ...f, corrective_action: e.target.value })} placeholder="Langkah pencegahan agar tidak terulang" /></Field>
          <Field label="PIC tindakan korektif" error={errors.corrective_owner_user_id}><UserPicker propertyId={incident.property_id} value={f.corrective_owner_user_id} onChange={(v) => setF({ ...f, corrective_owner_user_id: v })} placeholder="Pilih penanggung jawab…" /></Field>
        </div>
        <DialogFooter>
          <Button variant="secondary" onClick={onClose}>Batal</Button>
          <Button loading={update.isPending} onClick={submit}>Simpan</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

// ---------- Orang terlibat (security.incident_people.*) ----------
const ROLE_TONE: Record<string, Tone> = { suspect: "error", victim: "warning", witness: "info", reporter: "primary", other: "neutral" };

export function PeopleCard({ incident }: { incident: Incident }) {
  const toast = useToast();
  const invalidate = useInvalidate();
  const canView = incident.allowed_actions.includes("view_people");
  const canManage = incident.allowed_actions.includes("manage_people") && incident.status !== "cancelled";
  const count = incident.people_count ?? 0;
  // data pribadi dimuat atas permintaan (setiap GET tercatat di audit log server)
  const [revealed, setRevealed] = useState(false);
  const people = useQuery({
    queryKey: ["incident-people", incident.id],
    enabled: canView && revealed,
    queryFn: ({ signal }) => api<ListResponse<IncidentPerson>>(`incidents/${incident.id}/people`, { signal }).then((r) => r.data),
    staleTime: 5 * 60_000,
    refetchOnWindowFocus: false,
  });
  const [edit, setEdit] = useState<IncidentPerson | "new" | null>(null);
  const [del, setDel] = useState<IncidentPerson | null>(null);
  if (!canView) return null;
  const remove = (p: IncidentPerson) =>
    api(`incident-people/${p.id}`, { method: "DELETE" })
      .then(() => { invalidate("incident-people", "one", "activities"); toast.action("deleted", p.name); setDel(null); })
      .catch((e) => toast.failed("deleted", e, p.name));
  return (
    <Card>
      <CardHeader>
        <CardTitle>Orang terlibat ({count})</CardTitle>
        {canManage && <Button variant="link" size="sm" icon="person_add" onClick={() => { setRevealed(true); setEdit("new"); }}>Tambah</Button>}
      </CardHeader>
      <CardContent className="space-y-3">
        <PrivacyNote />
        {!revealed ? (
          count > 0 ? (
            <Button variant="secondary" icon="visibility" className={TOUCH} onClick={() => setRevealed(true)}>Tampilkan {count} orang terlibat</Button>
          ) : (
            <p className="text-sm italic text-muted-foreground">Belum ada pelapor, korban, saksi, atau terduga yang dicatat.</p>
          )
        ) : (
          <AsyncState query={people} empty={{ icon: "group", title: "Belum ada orang terlibat", description: "Catat pelapor, korban, saksi, atau terduga beserta kontak dan identitasnya." }}>
            {(list) => (
              <ul className="divide-y divide-border rounded-[var(--radius-lg)] border border-border">
                {list.map((p) => (
                  <li key={p.id} className="flex flex-wrap items-start justify-between gap-2 px-3 py-2.5">
                    <div className="min-w-0">
                      <div className="flex flex-wrap items-center gap-2"><span className="font-medium text-on-surface">{p.name}</span><Badge tone={ROLE_TONE[p.person_role] ?? "neutral"}>{labelOf(PERSON_ROLES, p.person_role)}</Badge></div>
                      <div className="text-sm text-on-surface-variant">{[p.contact, p.identity_number ? `ID ${p.identity_number}` : null, p.tenant_name].filter(Boolean).join(" · ") || "—"}</div>
                      {p.notes && <p className="mt-0.5 whitespace-pre-line text-sm">{p.notes}</p>}
                    </div>
                    {canManage && (
                      <div className="flex shrink-0 gap-1">
                        <Button size="sm" variant="ghost" icon="edit" className={TOUCH} onClick={() => setEdit(p)} aria-label={`Edit ${p.name}`}>Edit</Button>
                        <Button size="sm" variant="ghost" icon="delete" className={TOUCH} onClick={() => setDel(p)} aria-label={`Hapus ${p.name}`}>Hapus</Button>
                      </div>
                    )}
                  </li>
                ))}
              </ul>
            )}
          </AsyncState>
        )}
      </CardContent>
      {edit && <PersonDialog incident={incident} item={edit === "new" ? null : edit} onClose={() => setEdit(null)} />}
      {del && <ConfirmDialog open onOpenChange={(o) => !o && setDel(null)} title={`Hapus ${del.name}?`} description="Data orang terlibat dihapus dari incident ini (tercatat di audit log)." confirmLabel="Hapus" destructive onConfirm={() => remove(del)} />}
    </Card>
  );
}

function PersonDialog({ incident, item, onClose }: { incident: Incident; item: IncidentPerson | null; onClose: () => void }) {
  const toast = useToast();
  const invalidate = useInvalidate();
  const [f, setF] = useState({ person_role: item?.person_role ?? "witness", name: item?.name ?? "", contact: item?.contact ?? "", identity_number: item?.identity_number ?? "", notes: item?.notes ?? "" });
  const [busy, setBusy] = useState(false);
  const submit = async () => {
    if (!f.name.trim()) return;
    setBusy(true);
    try {
      const body = { person_role: f.person_role, name: f.name.trim(), contact: f.contact.trim() || (item ? "" : null), identity_number: f.identity_number.trim() || (item ? "" : null), notes: f.notes.trim() || (item ? "" : null) };
      if (item) await api(`incident-people/${item.id}`, { method: "PATCH", body });
      else await api(`incidents/${incident.id}/people`, { body, idempotencyKey: uuid() });
      invalidate("incident-people", "one", "activities");
      toast.action(item ? "saved" : "created", f.name.trim());
      onClose();
    } catch (e) {
      toast.failed(item ? "saved" : "created", e, "Orang terlibat");
    } finally {
      setBusy(false);
    }
  };
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent title={item ? `Edit · ${item.name}` : "Tambah orang terlibat"} description="Data pribadi — hanya terlihat oleh role Security/Management.">
        <div className="space-y-4">
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <Field label="Peran" required><NativeSelect value={f.person_role} onChange={(e) => setF({ ...f, person_role: e.target.value })}>{optionsOf(PERSON_ROLES).map((o) => <option key={o.value} value={o.value}>{o.label}</option>)}</NativeSelect></Field>
            <Field label="Nama" required><Input value={f.name} autoFocus onChange={(e) => setF({ ...f, name: e.target.value })} /></Field>
            <Field label="Kontak"><Input type="tel" inputMode="tel" value={f.contact} onChange={(e) => setF({ ...f, contact: e.target.value })} placeholder="No. HP / unit" /></Field>
            <Field label="Nomor identitas"><Input value={f.identity_number} autoComplete="off" onChange={(e) => setF({ ...f, identity_number: e.target.value })} placeholder="KTP / SIM / kartu akses" /></Field>
          </div>
          <Field label="Catatan"><Textarea rows={3} value={f.notes} onChange={(e) => setF({ ...f, notes: e.target.value })} placeholder="Keterangan singkat, mis. isi kesaksian" /></Field>
        </div>
        <DialogFooter>
          <Button variant="secondary" onClick={onClose}>Batal</Button>
          <Button disabled={!f.name.trim()} loading={busy} onClick={submit}>Simpan</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

// ---------- Video evidence (attachment_type "video") ----------
export function VideoEvidence({ incident, videos, onUploaded }: { incident: Incident; videos: Attachment[]; onUploaded: () => void }) {
  const canUpload = incident.allowed_actions.includes("attach") && !TERMINAL.includes(incident.status);
  return (
    <div>
      <div className="mb-2 text-xs font-semibold uppercase text-muted-foreground">Video ({videos.length})</div>
      {canUpload && <VideoUploader incidentId={incident.id} onUploaded={onUploaded} />}
      {videos.length === 0 ? (
        <p className="text-sm text-muted-foreground">Belum ada video evidence.</p>
      ) : (
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
          {videos.map((v) => (
            <figure key={v.id} className="overflow-hidden rounded-[var(--radius-md)] border border-border bg-surface">
              {v.url && v.status === "ready" ? (
                <video controls preload="metadata" playsInline src={v.url} className="aspect-video w-full bg-inverse-surface" aria-label={v.original_filename ?? "Video evidence"} />
              ) : (
                <div className="flex aspect-video items-center justify-center gap-2 text-sm text-on-surface-variant"><Icon name="videocam" size={28} aria-hidden />{v.status === "failed" ? "Unggah gagal" : "Memproses…"}</div>
              )}
              <figcaption className="space-y-0.5 px-2 py-1.5 text-xs text-muted-foreground">
                <div className="truncate font-medium text-on-surface">{v.original_filename ?? "video"}</div>
                <div className="tnum">{(v.size_bytes / 1024 / 1024).toFixed(1)} MB · {v.uploaded_by_name} · {fmtDateTime(v.uploaded_at)}</div>
              </figcaption>
            </figure>
          ))}
        </div>
      )}
    </div>
  );
}

function VideoUploader({ incidentId, onUploaded }: { incidentId: string; onUploaded: () => void }) {
  const toast = useToast();
  const [progress, setProgress] = useState<number | null>(null);
  const onDrop = useCallback(
    async (files: File[], rejected: FileRejection[]) => {
      for (const r of rejected) toast.error(new Error(validateVideoFile(r.file) ?? `File ${r.file.name} ditolak`));
      for (const file of files) {
        const err = validateVideoFile(file);
        if (err) {
          toast.error(new Error(err));
          continue;
        }
        setProgress(0);
        try {
          // video tidak dikompres (P2-SIN-04); batas 50 MB divalidasi klien & server
          await uploadAttachment(file, "incident", incidentId, "video", setProgress, { compress: false });
          toast.action("uploaded", "Video");
          onUploaded();
        } catch (e) {
          toast.failed("uploaded", e, "Video");
        } finally {
          setProgress(null);
        }
      }
    },
    [incidentId, onUploaded, toast],
  );
  const { getRootProps, getInputProps, isDragActive } = useDropzone({ onDrop, accept: VIDEO_ACCEPT, maxSize: MAX_VIDEO_BYTES, multiple: false, disabled: progress !== null });
  return (
    <div {...getRootProps()} className={cn("mb-3 flex min-h-24 cursor-pointer items-center justify-center gap-2 rounded-md border border-dashed px-4 py-3 text-center text-sm text-muted-foreground transition-colors hover:border-brand-500 hover:bg-brand-50", isDragActive && "border-brand-500 bg-brand-50", progress !== null && "cursor-wait opacity-80")}>
      <input {...getInputProps()} aria-label="Unggah video evidence" />
      {progress !== null ? (
        <span className="inline-flex items-center gap-2"><Icon name="cloud_upload" size={16} className="animate-pulse" /> Mengunggah video… {progress}%</span>
      ) : (
        <span className="inline-flex items-center gap-2"><Icon name="videocam" size={16} /> Seret video ke sini atau klik untuk memilih (MP4/MOV/WebM, maks 50 MB, tanpa kompresi)</span>
      )}
    </div>
  );
}
