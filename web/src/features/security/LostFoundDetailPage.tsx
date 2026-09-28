// Detail barang temuan Lost & Found (PRD P2 v2.1 §6.7): foto, lokasi temuan & simpan, masa simpan, laporan kehilangan
// yang cocok; aksi dari allowed_actions: update · match (pilih laporan open) · return (serah terima: nama + identitas wajib,
// kontak, tanda tangan SignaturePad → attachment `signature`) · dispose (hanya setelah masa simpan). Identitas penerima =
// data pribadi (hanya security.lost_found.manage).
import { useState } from "react";
import { Link, useParams } from "react-router-dom";
import { Icon } from "@buildingvision/ui";
import { PageHeader } from "@/components/shell/AppShell";
import { Button, Card, CardContent, CardHeader, CardTitle, Dialog, DialogContent, DialogFooter, Field, Input, NativeSelect, Textarea } from "@/components/ui/primitives";
import { AsyncState, DetailSkeleton, KeyValue, LocationPath, RelativeTime, useToast } from "@/components/bv/common";
import { AttachmentGrid, PhotoEvidenceUploader } from "@/components/bv/checklist";
import { SignaturePad } from "@/components/bv/SignaturePad";
import { MobileActionBar } from "@/components/bv/mobile";
import { uploadAttachment, useAction, useAll, useAttachments, useOne } from "@/api/hooks";
import { signatureFile } from "@/lib/signature";
import { fmtDate, fmtDateTime } from "@/lib/format";
import { cn } from "@/lib/utils";
import type { Attachment } from "@/api/types";
import type { LostFoundItem, LostReport } from "./types";
import { DISPOSAL_METHODS, LOST_FOUND_CATEGORIES, labelOf, optionsOf } from "./p2";
import { DisposalDueBadge, PrivacyNote } from "./shared";
import { StatusBadge } from "@/components/bv/badges";
import { TOUCH } from "./hooks";

type ItemAction = "update" | "match" | "return" | "dispose";
const ACTIONS: { key: ItemAction; label: string; icon: string }[] = [
  { key: "return", label: "Serah terima", icon: "handshake" },
  { key: "match", label: "Cocokkan laporan", icon: "link" },
  { key: "update", label: "Edit", icon: "edit" },
  { key: "dispose", label: "Disposal", icon: "delete" },
];

export default function LostFoundDetailPage() {
  const { id } = useParams();
  const q = useOne<LostFoundItem>("lost-found/items", id);
  const attachments = useAttachments("lost_found_item", id);
  const [dlg, setDlg] = useState<ItemAction | null>(null);
  return (
    <AsyncState query={q} skeleton={<DetailSkeleton />}>
      {(x) => {
        const actions = ACTIONS.filter((a) => x.allowed_actions.includes(a.key));
        const all = attachments.data ?? [];
        const photos = all.filter((a) => a.attachment_type !== "signature");
        const signature = all.find((a) => a.id === x.signature_attachment_id) ?? null;
        const primary = actions[0] ?? null;
        return (
          <div>
            <PageHeader
              breadcrumb={<><Link to="/security/lost-found" className="hover:underline">Lost & Found</Link> / <span className="font-mono">{x.item_number}</span></>}
              title={<span><span className="font-mono">{x.item_number}</span> <span className="font-normal text-muted-foreground">·</span> {labelOf(LOST_FOUND_CATEGORIES, x.category)}</span>}
              badges={<><StatusBadge objectType="lost_found_item" status={x.status} /><DisposalDueBadge due={x.disposal_due} /></>}
              subtitle={<>Ditemukan <RelativeTime value={x.found_at} />{x.found_by_name ? <> · dicatat {x.found_by_name}</> : null}</>}
              actions={actions.length > 0 && (
                <span className="hidden flex-wrap gap-2 md:flex">
                  {actions.map((a) => <Button key={a.key} size="sm" icon={a.icon} variant={a.key === primary?.key ? "primary" : "secondary"} onClick={() => setDlg(a.key)}>{a.label}</Button>)}
                </span>
              )}
            />
            {x.disposal_due && x.status === "stored" && <p className="mb-4 rounded-[var(--radius-md)] bg-warning-container px-3 py-2 text-sm text-on-warning-container">Masa simpan berakhir {fmtDate(x.retention_until)}. Barang dapat diserahkan ke pemilik atau di-disposal (donasi, dimusnahkan, diserahkan ke polisi, dilelang).</p>}
            <div className="grid grid-cols-1 gap-5 lg:grid-cols-12">
              <div className="min-w-0 space-y-4 lg:col-span-8">
                <Card>
                  <CardHeader><CardTitle>Barang</CardTitle></CardHeader>
                  <CardContent className="space-y-4">
                    <p className="whitespace-pre-line text-body">{x.description}</p>
                    <KeyValue items={[
                      { label: "Kategori", value: labelOf(LOST_FOUND_CATEGORIES, x.category) },
                      { label: "Lokasi temuan", value: <LocationPath pathText={x.found_location.path_text} locationId={x.found_location.id} linkTo={(lid) => `/property/locations/${lid}`} /> },
                      { label: "Waktu ditemukan", value: fmtDateTime(x.found_at) },
                      { label: "Penemu", value: x.finder_name || x.found_by_name || "—" },
                      { label: "Dicatat oleh", value: x.found_by_name ?? "—" },
                      { label: "Lokasi simpan", value: x.storage_location || "—" },
                    ]} />
                  </CardContent>
                </Card>
                <Card>
                  <CardHeader><CardTitle>Foto barang</CardTitle><span className="text-xs text-on-surface-variant">{photos.length} foto</span></CardHeader>
                  <CardContent className="space-y-3">
                    {x.allowed_actions.includes("attach") && <PhotoEvidenceUploader objectType="lost_found_item" objectId={x.id} attachmentType="photo" label="Unggah foto barang" onUploaded={() => attachments.refetch()} />}
                    <AttachmentGrid items={photos} emptyLabel="Belum ada foto barang." />
                  </CardContent>
                </Card>
                {x.status === "returned" && (
                  <Card>
                    <CardHeader><CardTitle>Serah terima</CardTitle></CardHeader>
                    <CardContent className="space-y-3">
                      <PrivacyNote />
                      <KeyValue items={[
                        { label: "Penerima", value: x.claimant_name ?? "—" },
                        { label: "Kontak", value: x.claimant_contact ?? "—" },
                        ...(x.claimant_identity !== undefined ? [{ label: "Identitas", value: x.claimant_identity ?? "—" }] : []),
                        { label: "Diserahkan", value: <>{fmtDateTime(x.returned_at)}{x.returned_by_name && <span className="block text-xs text-on-surface-variant">oleh {x.returned_by_name}</span>}</> },
                        { label: "Tanda tangan", value: signature?.url ? <img src={signature.url} alt="Tanda tangan penerima" className="h-24 rounded-[var(--radius-md)] border border-border bg-surface object-contain" /> : x.signature_attachment_id ? "Tersimpan" : "Tanpa tanda tangan" },
                      ]} />
                    </CardContent>
                  </Card>
                )}
                {x.status === "disposed" && (
                  <Card>
                    <CardHeader><CardTitle>Disposal</CardTitle></CardHeader>
                    <CardContent>
                      <KeyValue items={[
                        { label: "Metode", value: labelOf(DISPOSAL_METHODS, x.disposal_method) },
                        { label: "Waktu", value: fmtDateTime(x.disposed_at) },
                        { label: "Catatan", value: x.disposal_note ? <span className="whitespace-pre-line">{x.disposal_note}</span> : "—" },
                      ]} />
                    </CardContent>
                  </Card>
                )}
              </div>
              <div className="min-w-0 space-y-4 lg:col-span-4">
                <Card>
                  <CardHeader><CardTitle>Masa simpan</CardTitle></CardHeader>
                  <CardContent className="space-y-2">
                    <div className="text-h3 font-bold tnum text-on-surface">{fmtDate(x.retention_until)}</div>
                    <p className="text-sm text-on-surface-variant">{x.status !== "stored" ? "Barang sudah tidak disimpan." : x.disposal_due ? "Masa simpan sudah lewat — disposal diperbolehkan." : "Disposal baru dapat dilakukan setelah tanggal ini."}</p>
                  </CardContent>
                </Card>
                <Card>
                  <CardHeader><CardTitle>Laporan kehilangan</CardTitle>{x.allowed_actions.includes("match") && <Button variant="link" size="sm" onClick={() => setDlg("match")}>{x.matched_report_id ? "Ganti" : "Cocokkan"}</Button>}</CardHeader>
                  <CardContent>
                    {x.matched_report_id ? (
                      <p className="text-sm"><Icon name="link" size={14} className="mr-1 inline align-middle text-on-surface-variant" aria-hidden />Cocok dengan laporan <Link to="/security/lost-found/reports" className="font-mono font-semibold text-primary hover:underline">{x.matched_report_number ?? "—"}</Link></p>
                    ) : (
                      <p className="text-sm text-on-surface-variant">Belum dicocokkan dengan laporan kehilangan.</p>
                    )}
                  </CardContent>
                </Card>
              </div>
            </div>
            {actions.length > 0 && (
              <MobileActionBar
                primary={primary ? { label: primary.label, icon: primary.icon, onSelect: () => setDlg(primary.key) } : null}
                secondary={actions.slice(1).map((a) => ({ label: a.label, icon: a.icon, onSelect: () => setDlg(a.key), destructive: a.key === "dispose" }))}
              />
            )}
            {dlg === "update" && <UpdateDialog item={x} onClose={() => setDlg(null)} />}
            {dlg === "match" && <MatchReportDialog item={x} onClose={() => setDlg(null)} />}
            {dlg === "return" && <ReturnDialog item={x} onClose={() => setDlg(null)} />}
            {dlg === "dispose" && <DisposeDialog item={x} onClose={() => setDlg(null)} />}
          </div>
        );
      }}
    </AsyncState>
  );
}

function useItemAction(item: LostFoundItem, action: ItemAction) {
  return useAction<Record<string, unknown>, LostFoundItem>(() => `lost-found/items/${item.id}/${action}`);
}

function UpdateDialog({ item, onClose }: { item: LostFoundItem; onClose: () => void }) {
  const toast = useToast();
  const act = useItemAction(item, "update");
  const [f, setF] = useState({ category: item.category, description: item.description, storage_location: item.storage_location ?? "" });
  const submit = () => act.mutateAsync({ category: f.category, description: f.description.trim(), storage_location: f.storage_location.trim() }).then(() => { toast.action("saved", item.item_number); onClose(); }).catch((e) => toast.failed("saved", e, item.item_number));
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent title={`Edit · ${item.item_number}`}>
        <div className="space-y-4">
          <Field label="Kategori"><NativeSelect value={f.category} onChange={(e) => setF({ ...f, category: e.target.value })}>{optionsOf(LOST_FOUND_CATEGORIES).map((o) => <option key={o.value} value={o.value}>{o.label}</option>)}</NativeSelect></Field>
          <Field label="Deskripsi" required><Textarea rows={3} value={f.description} onChange={(e) => setF({ ...f, description: e.target.value })} /></Field>
          <Field label="Lokasi simpan"><Input value={f.storage_location} onChange={(e) => setF({ ...f, storage_location: e.target.value })} /></Field>
        </div>
        <DialogFooter>
          <Button variant="secondary" onClick={onClose}>Batal</Button>
          <Button disabled={!f.description.trim()} loading={act.isPending} onClick={submit}>Simpan</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

/** Pilih laporan kehilangan yang masih open (property yang sama) → POST match {report_id}. */
function MatchReportDialog({ item, onClose }: { item: LostFoundItem; onClose: () => void }) {
  const toast = useToast();
  const act = useItemAction(item, "match");
  const reports = useAll<LostReport>("lost-found/reports", { property_id: item.property_id, status: "open" });
  const [reportId, setReportId] = useState<string | null>(null);
  // kategori sama tampil lebih dulu
  const list = [...(reports.data ?? [])].sort((a, b) => Number(b.category === item.category) - Number(a.category === item.category));
  const submit = () => reportId && act.mutateAsync({ report_id: reportId }).then((r) => { toast.success(`${r.item_number} dicocokkan dengan ${r.matched_report_number ?? "laporan"}`); onClose(); }).catch((e) => toast.failed("updated", e, item.item_number));
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent side="right" title={`Cocokkan laporan · ${item.item_number}`} description="Pilih laporan kehilangan yang masih terbuka. Laporan berubah menjadi Cocok; serah terima menutup laporan.">
        <AsyncState query={reports} empty={{ icon: "search", title: "Tidak ada laporan kehilangan terbuka", description: "Catat laporan kehilangan di tab Laporan Kehilangan terlebih dahulu." }}>
          {() => (
            <ul className="space-y-2" role="radiogroup" aria-label="Laporan kehilangan">
              {list.map((r) => (
                <li key={r.id}>
                  <button type="button" role="radio" aria-checked={reportId === r.id} onClick={() => setReportId(r.id)}
                    className={cn("w-full rounded-[var(--radius-lg)] border px-3 py-2.5 text-left transition-colors hover:bg-surface-container", reportId === r.id ? "border-primary bg-primary-soft" : "border-border bg-surface")}>
                    <div className="flex flex-wrap items-center justify-between gap-2">
                      <span className="font-mono text-[13px] font-semibold">{r.report_number}</span>
                      <span className={cn("text-xs", r.category === item.category ? "font-semibold text-primary" : "text-on-surface-variant")}>{labelOf(LOST_FOUND_CATEGORIES, r.category)}{r.category === item.category ? " · kategori sama" : ""}</span>
                    </div>
                    <p className="line-clamp-2 text-sm">{r.description}</p>
                    <div className="text-xs text-on-surface-variant">{r.reporter_name}{r.reporter_contact ? ` · ${r.reporter_contact}` : ""} · {r.lost_at ? fmtDateTime(r.lost_at) : `dilaporkan ${fmtDateTime(r.created_at)}`}</div>
                  </button>
                </li>
              ))}
            </ul>
          )}
        </AsyncState>
        <DialogFooter>
          <Button variant="secondary" onClick={onClose}>Batal</Button>
          <Button icon="link" disabled={!reportId} loading={act.isPending} onClick={submit}>Cocokkan</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

/** Serah terima ke pemilik: nama & identitas wajib; tanda tangan diunggah dulu sebagai attachment `signature`. */
function ReturnDialog({ item, onClose }: { item: LostFoundItem; onClose: () => void }) {
  const toast = useToast();
  const act = useItemAction(item, "return");
  const [f, setF] = useState({ claimant_name: "", claimant_identity: "", claimant_contact: "" });
  const [sig, setSig] = useState<Attachment | null>(null);
  const [saving, setSaving] = useState(false);
  const saveSignature = async (png: Blob) => {
    setSaving(true);
    try {
      // PNG tanpa kompresi JPEG agar garis tetap tajam (lib/signature)
      const a = await uploadAttachment(signatureFile(png), "lost_found_item", item.id, "signature", undefined, { compress: false });
      setSig(a);
      toast.action("saved", "Tanda tangan");
    } catch (e) {
      toast.failed("saved", e, "Tanda tangan");
    } finally {
      setSaving(false);
    }
  };
  const valid = !!f.claimant_name.trim() && !!f.claimant_identity.trim();
  const submit = () =>
    act
      .mutateAsync({ claimant_name: f.claimant_name.trim(), claimant_identity: f.claimant_identity.trim(), claimant_contact: f.claimant_contact.trim() || null, signature_attachment_id: sig?.id ?? null })
      .then((r) => { toast.success(`${r.item_number} diserahkan ke ${r.claimant_name ?? "pemilik"}`); onClose(); })
      .catch((e) => toast.failed("updated", e, item.item_number));
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent side="right" title={`Serah terima · ${item.item_number}`} description="Pastikan identitas penerima sesuai dengan laporan kehilangan / bukti kepemilikan.">
        <div className="space-y-4">
          <PrivacyNote>Identitas penerima adalah data pribadi — hanya terlihat oleh pengelola Lost & Found dan setiap akses dicatat di audit log.</PrivacyNote>
          <Field label="Nama penerima" required><Input value={f.claimant_name} autoFocus onChange={(e) => setF({ ...f, claimant_name: e.target.value })} /></Field>
          <Field label="Nomor identitas" required help="KTP / SIM / paspor / kartu akses gedung."><Input value={f.claimant_identity} onChange={(e) => setF({ ...f, claimant_identity: e.target.value })} autoComplete="off" /></Field>
          <Field label="Kontak penerima"><Input type="tel" inputMode="tel" value={f.claimant_contact} onChange={(e) => setF({ ...f, claimant_contact: e.target.value })} /></Field>
          <Field label="Tanda tangan penerima" help={sig ? undefined : "Minta penerima menandatangani lalu tekan Simpan. Opsional, namun dianjurkan sebagai bukti serah terima."}>
            {sig ? (
              <div className="flex flex-wrap items-center gap-3">
                {sig.url ? <img src={sig.url} alt="Tanda tangan penerima" className="h-24 rounded-[var(--radius-md)] border border-border bg-surface object-contain" /> : <span className="inline-flex items-center gap-1 text-sm text-on-surface"><Icon name="draw" size={16} aria-hidden />Tanda tangan tersimpan</span>}
                <Button size="sm" variant="ghost" icon="replay" className={TOUCH} onClick={() => setSig(null)}>Ulangi</Button>
              </div>
            ) : (
              <SignaturePad onSave={saveSignature} saving={saving} height={160} />
            )}
          </Field>
        </div>
        <DialogFooter>
          <Button variant="secondary" onClick={onClose}>Batal</Button>
          <Button icon="handshake" disabled={!valid || saving} loading={act.isPending} onClick={submit}>Serahkan barang</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function DisposeDialog({ item, onClose }: { item: LostFoundItem; onClose: () => void }) {
  const toast = useToast();
  const act = useItemAction(item, "dispose");
  const [f, setF] = useState({ disposal_method: "", disposal_note: "" });
  const submit = () =>
    act
      .mutateAsync({ disposal_method: f.disposal_method, disposal_note: f.disposal_note.trim() || null })
      .then((r) => { toast.success(`${r.item_number} di-disposal (${labelOf(DISPOSAL_METHODS, r.disposal_method)})`); onClose(); })
      .catch((e) => toast.failed("updated", e, item.item_number));
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent title={`Disposal · ${item.item_number}`} description={`Masa simpan berakhir ${fmtDate(item.retention_until)}. Status menjadi Disposal dan tidak dapat dikembalikan.`}>
        <div className="space-y-4">
          <Field label="Metode disposal" required><NativeSelect value={f.disposal_method} onChange={(e) => setF({ ...f, disposal_method: e.target.value })}><option value="">Pilih metode…</option>{optionsOf(DISPOSAL_METHODS).map((o) => <option key={o.value} value={o.value}>{o.label}</option>)}</NativeSelect></Field>
          <Field label="Catatan"><Textarea rows={3} value={f.disposal_note} onChange={(e) => setF({ ...f, disposal_note: e.target.value })} placeholder="mis. diserahkan ke Polsek Setiabudi, BA no. …" /></Field>
        </div>
        <DialogFooter>
          <Button variant="secondary" onClick={onClose}>Batal</Button>
          <Button variant="destructive" disabled={!f.disposal_method} loading={act.isPending} onClick={submit}>Disposal</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
