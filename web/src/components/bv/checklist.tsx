// ChecklistRunner (web: review + edit oleh supervisor, DS §4.6) dan PhotoEvidenceUploader (DS §4.7)
import { useCallback, useState } from "react";
import { useDropzone } from "react-dropzone";
import { Camera, CloudUpload, FileText, ImageOff, MapPin, MapPinOff } from "lucide-react";
import { Badge, Input, Segmented, Textarea } from "@/components/ui/primitives";
import { useAnswerItem, uploadAttachment } from "@/api/hooks";
import { useToast } from "./common";
import { fmtDateTime } from "@/lib/format";
import { cn } from "@/lib/utils";
import type { Attachment, ChecklistRun, ChecklistRunItem } from "@/api/types";

export function ChecklistSummaryLine({ run }: { run: ChecklistRun }) {
  const photoMissing = run.items.filter((i) => i.photo_required && !i.attachment_id).length;
  return (
    <p className="text-sm text-muted-foreground">
      <span className="font-medium text-foreground">{run.answered_items}/{run.total_items} selesai</span>
      {run.not_ok_items > 0 && <span className="text-critical-text"> · {run.not_ok_items} Not OK</span>}
      {photoMissing > 0 && <span className="text-warning-text"> · {photoMissing} foto kurang</span>}
      <span> · {run.template_name} v{run.template_version}</span>
    </p>
  );
}

function ResultBadge({ item }: { item: ChecklistRunItem }) {
  if (!item.answered_at) return <Badge className="bg-neutral-soft text-neutral-text">Belum dijawab</Badge>;
  switch (item.item_type) {
    case "ok_notok_na":
      return <Badge className={item.result_value === "ok" ? "bg-success-soft text-success-text" : item.result_value === "not_ok" ? "bg-critical-soft text-critical-text" : "bg-neutral-soft text-neutral-text"}>{{ ok: "OK", not_ok: "Not OK", na: "N/A" }[item.result_value ?? ""] ?? item.result_value}</Badge>;
    case "yes_no":
      return <Badge className={item.result_value === "yes" ? "bg-success-soft text-success-text" : "bg-neutral-soft text-neutral-text"}>{item.result_value === "yes" ? "Ya" : "Tidak"}</Badge>;
    case "numeric":
      return <Badge className={item.out_of_range ? "bg-warning-soft text-warning-text" : "bg-neutral-soft text-neutral-text"}>{item.result_number} {item.numeric_unit ?? ""}{item.out_of_range ? " · di luar rentang" : ""}</Badge>;
    case "text":
      return <span className="text-sm">{item.result_text}</span>;
    case "photo":
      return item.attachment_id ? <Badge className="bg-success-soft text-success-text">Foto ✓</Badge> : <Badge className="bg-warning-soft text-warning-text"><ImageOff className="h-3 w-3" /> Foto kurang</Badge>;
  }
}

export function ChecklistRunner({ run, editable, attachmentsById }: { run: ChecklistRun; editable: boolean; objectType?: string; objectId?: string; attachmentsById?: Record<string, Attachment> }) {
  const answer = useAnswerItem();
  const toast = useToast();
  const [drafts, setDrafts] = useState<Record<string, { number?: string; text?: string }>>({});
  const submit = (item: ChecklistRunItem, ans: Record<string, unknown>) => answer.mutateAsync({ itemId: item.id, answer: ans }).catch(toast.error);
  const sections = Array.from(new Set(run.items.map((i) => i.section ?? "")));
  return (
    <div className="space-y-4">
      <ChecklistSummaryLine run={run} />
      {sections.map((sec) => (
        <div key={sec} className="rounded-lg border border-border">
          {sec && <div className="border-b border-border bg-muted px-4 py-2 text-xs font-semibold uppercase tracking-wide text-muted-foreground">{sec}</div>}
          <ul className="divide-y divide-border">
            {run.items.filter((i) => (i.section ?? "") === sec).map((item) => (
              <li key={item.id} className="grid grid-cols-12 items-start gap-3 px-4 py-3">
                <div className="col-span-5">
                  <div className="text-body">{item.label}{item.is_required && <span className="ml-0.5 text-critical" aria-hidden>*</span>}</div>
                  {item.answered_at && (
                    <div className="mt-0.5 text-xs text-muted-foreground">
                      {item.answered_by_name} · {fmtDateTime(item.answered_at)}
                      {item.answered_source === "sync" && <Badge className="ml-1 bg-warning-soft text-warning-text">offline</Badge>}
                    </div>
                  )}
                  {item.note && <div className="mt-0.5 text-xs text-muted-foreground">Catatan: {item.note}</div>}
                  {item.finding_id && <a href={`/findings/${item.finding_id}`} className="text-xs text-brand-600 hover:underline">Finding dibuat →</a>}
                </div>
                <div className="col-span-7">
                  {!editable ? (
                    <div className="flex items-center gap-2">
                      <ResultBadge item={item} />
                      {item.attachment_id && attachmentsById?.[item.attachment_id]?.thumb_url && <img src={attachmentsById[item.attachment_id].thumb_url} alt="" className="h-10 w-10 rounded border border-border object-cover" />}
                    </div>
                  ) : (
                    <div className="space-y-2">
                      {item.item_type === "ok_notok_na" && (
                        <Segmented value={item.result_value as "ok" | "not_ok" | "na" | null} onChange={(v) => submit(item, { result_value: v })} options={[{ value: "ok", label: "OK", tone: "success" }, { value: "not_ok", label: "Not OK", tone: "critical" }, { value: "na", label: "N/A", tone: "neutral" }]} />
                      )}
                      {item.item_type === "yes_no" && <Segmented value={item.result_value as "yes" | "no" | null} onChange={(v) => submit(item, { result_value: v })} options={[{ value: "yes", label: "Ya", tone: "success" }, { value: "no", label: "Tidak", tone: "neutral" }]} />}
                      {item.item_type === "numeric" && (
                        <div className="flex items-center gap-2">
                          <Input type="number" className="w-32" defaultValue={item.result_number ?? ""} onChange={(e) => setDrafts((d) => ({ ...d, [item.id]: { ...d[item.id], number: e.target.value } }))} onBlur={() => drafts[item.id]?.number !== undefined && submit(item, { result_number: Number(drafts[item.id].number) })} />
                          <span className="text-sm text-muted-foreground">{item.numeric_unit}{item.numeric_min !== null || item.numeric_max !== null ? ` (normal ${item.numeric_min ?? "…"}–${item.numeric_max ?? "…"})` : ""}</span>
                          {item.out_of_range && <Badge className="bg-warning-soft text-warning-text">di luar rentang</Badge>}
                        </div>
                      )}
                      {item.item_type === "text" && <Textarea rows={2} defaultValue={item.result_text ?? ""} onChange={(e) => setDrafts((d) => ({ ...d, [item.id]: { ...d[item.id], text: e.target.value } }))} onBlur={() => drafts[item.id]?.text !== undefined && submit(item, { result_text: drafts[item.id].text })} />}
                      {(item.item_type === "photo" || item.photo_required) && (
                        <div className="flex items-center gap-2">
                          {item.attachment_id && attachmentsById?.[item.attachment_id]?.thumb_url && <img src={attachmentsById[item.attachment_id].thumb_url} alt="" className="h-12 w-12 rounded border border-border object-cover" />}
                          <PhotoEvidenceUploader objectType="checklist_run_item" objectId={item.id} attachmentType="checklist_item_photo" compact onUploaded={(a) => submit(item, { attachment_id: a.id, result_value: item.result_value ?? (item.item_type === "photo" ? "ok" : undefined) })} />
                        </div>
                      )}
                    </div>
                  )}
                </div>
              </li>
            ))}
          </ul>
        </div>
      ))}
    </div>
  );
}

// ---------- PhotoEvidenceUploader: slot bertipe before/after/photo/document; status pending/ready/failed; metadata GPS ----------
export function PhotoEvidenceUploader({ objectType, objectId, attachmentType = "photo", onUploaded, compact, label, disabled }: { objectType: string; objectId: string; attachmentType?: string; onUploaded?: (a: Attachment) => void; compact?: boolean; label?: string; disabled?: boolean }) {
  const [progress, setProgress] = useState<number | null>(null);
  const toast = useToast();
  const onDrop = useCallback(
    async (files: File[]) => {
      for (const f of files) {
        if (f.size > 10 * 1024 * 1024) {
          toast.error(new Error("Ukuran file maksimal 10 MB"));
          continue;
        }
        setProgress(0);
        try {
          const a = await uploadAttachment(f, objectType, objectId, attachmentType, setProgress);
          onUploaded?.(a);
          toast.success("Foto tersimpan");
        } catch (e) {
          toast.error(e);
        } finally {
          setProgress(null);
        }
      }
    },
    [objectType, objectId, attachmentType, onUploaded, toast],
  );
  const { getRootProps, getInputProps, isDragActive } = useDropzone({ onDrop, accept: { "image/jpeg": [], "image/png": [], "image/webp": [], "application/pdf": [] }, multiple: !compact, disabled: disabled || progress !== null });
  return (
    <div {...getRootProps()} className={cn("flex cursor-pointer items-center justify-center gap-2 rounded-md border border-dashed text-sm text-muted-foreground transition-colors hover:border-brand-500 hover:bg-brand-50", isDragActive && "border-brand-500 bg-brand-50", compact ? "h-12 px-3" : "h-28 px-4", disabled && "cursor-not-allowed opacity-50")}>
      <input {...getInputProps()} aria-label={label ?? "Unggah foto"} />
      {progress !== null ? (
        <span className="inline-flex items-center gap-2"><CloudUpload className="h-4 w-4 animate-pulse" /> Mengunggah… {progress}%</span>
      ) : (
        <span className="inline-flex items-center gap-2">{attachmentType === "document" ? <FileText className="h-4 w-4" /> : <Camera className="h-4 w-4" />} {label ?? (compact ? "Unggah foto" : "Seret foto ke sini atau klik untuk memilih (≤10 MB, dikompres ≤1600px)")}</span>
      )}
    </div>
  );
}

export function AttachmentGrid({ items, emptyLabel = "Belum ada foto." }: { items: Attachment[]; emptyLabel?: string }) {
  if (!items.length) return <p className="text-sm text-muted-foreground">{emptyLabel}</p>;
  return (
    <div className="grid grid-cols-4 gap-3">
      {items.map((a) => (
        <figure key={a.id} className="overflow-hidden rounded-md border border-border bg-card">
          {a.content_type.startsWith("image/") && a.url ? (
            <a href={a.url} target="_blank" rel="noreferrer">
              <img src={a.thumb_url ?? a.url} alt={a.caption ?? a.attachment_type} className="h-28 w-full object-cover" />
            </a>
          ) : (
            <div className="flex h-28 items-center justify-center text-muted-foreground"><FileText className="h-8 w-8" /></div>
          )}
          <figcaption className="space-y-0.5 px-2 py-1.5 text-xs text-muted-foreground">
            <div className="flex items-center justify-between">
              <Badge className={a.attachment_type === "photo_before" ? "bg-info-soft text-info-text" : a.attachment_type === "photo_after" ? "bg-success-soft text-success-text" : "bg-neutral-soft text-neutral-text"}>{{ photo_before: "Before", photo_after: "After", checklist_item_photo: "Checklist", document: "Dokumen", signature: "Tanda tangan" }[a.attachment_type] ?? "Foto"}</Badge>
              <Badge className={a.status === "ready" ? "bg-success-soft text-success-text" : a.status === "failed" ? "bg-critical-soft text-critical-text" : "bg-warning-soft text-warning-text"}>{a.status}</Badge>
            </div>
            <div className="truncate">{a.uploaded_by_name}</div>
            <div className="flex items-center gap-1 tnum">{fmtDateTime(a.captured_at ?? a.uploaded_at)} {a.gps_status === "captured" ? <MapPin className="h-3 w-3 text-success" aria-label="GPS captured" /> : <MapPinOff className="h-3 w-3" aria-label={`GPS ${a.gps_status}`} />}</div>
          </figcaption>
        </figure>
      ))}
    </div>
  );
}
