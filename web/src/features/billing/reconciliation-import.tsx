// Rekonsiliasi › impor mutasi rekening (PRD P4 v2.1 P4-REC-01): berkas CSV/XLSX ekspor internet banking → base64 → server
// mendeteksi baris judul & kolom (dapat dipetakan manual: judul kolom atau #nomor), melewati duplikat, lalu menyarankan
// pencocokan. Hasil deteksi kolom & duplikat hanya ada di respons impor → diteruskan ke detail lewat state navigasi.
import { useRef, useState } from "react";
import { useNavigate } from "react-router-dom";
import { Icon } from "@buildingvision/ui";
import { Alert, Button, Dialog, DialogContent, DialogFooter, Field, Input, NativeSelect } from "@/components/ui/primitives";
import { useToast } from "@/components/bv/common";
import { useAll, useInvalidate } from "@/api/hooks";
import { api, uuid } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { fmtNumber } from "@/lib/format";
import { cn } from "@/lib/utils";
import { PropertySelect } from "@/features/finance/fin-ui";
import { fileToBase64 } from "@/features/finance/fin-utils";
import { ACCEPT_EXT, COLUMN_KEYS, DATE_FORMATS, MAX_FILE_BYTES, acceptedFile, cleanColumns, type BankAccount, type StatementImport } from "./reconciliation-model";

export function ImportDialog({ propertyId, onClose }: { propertyId: string | null; onClose: () => void }) {
  const toast = useToast();
  const nav = useNavigate();
  const invalidate = useInvalidate();
  const { can, properties } = useAuth();
  const [pid, setPid] = useState<string | null>(propertyId ?? (properties.length === 1 ? properties[0].id : null));
  // GET /billing/bank-accounts dijaga billing.settings.view di router (walau service juga menerima billing.reconciliation.view)
  const canBanks = !!pid && can("billing.settings.view", pid);
  const banks = useAll<BankAccount>("billing/bank-accounts", { property_id: pid ?? undefined }, { enabled: canBanks });
  const [bank, setBank] = useState("");
  const [file, setFile] = useState<File | null>(null);
  const [showMap, setShowMap] = useState(false);
  const [cols, setCols] = useState<Record<string, string>>({});
  const [dateFormat, setDateFormat] = useState("");
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const inputRef = useRef<HTMLInputElement>(null);
  const pick = (f: File | null | undefined) => {
    setErr(null);
    if (!f) return;
    if (!acceptedFile(f.name)) {
      setErr(`Format tidak didukung — gunakan ${ACCEPT_EXT.join(", ")} (Excel .xls lama: simpan ulang sebagai .xlsx atau CSV).`);
      return;
    }
    if (f.size > MAX_FILE_BYTES) {
      setErr("Ukuran berkas maksimal 8 MB.");
      return;
    }
    setFile(f);
  };
  const submit = async () => {
    if (!pid || !file) return;
    setBusy(true);
    setErr(null);
    try {
      const content_base64 = await fileToBase64(file);
      const out = await api<StatementImport>("billing/bank-statements", {
        body: { property_id: pid, bank_account_id: bank || null, file_name: file.name, content_base64, columns: cleanColumns(cols), date_format: dateFormat || undefined },
        idempotencyKey: uuid(),
      });
      invalidate("list", "one");
      toast.success(`${fmtNumber(out.line_count)} mutasi diimpor · ${fmtNumber(out.suggested_count)} saran pencocokan${out.duplicates_skipped ? ` · ${fmtNumber(out.duplicates_skipped)} duplikat dilewati` : ""}`);
      onClose();
      nav(`/billing/reconciliation/${out.id}`, { state: { detected_columns: out.detected_columns ?? [], duplicates_skipped: out.duplicates_skipped ?? 0 } });
    } catch (e) {
      const p = e as { problem?: { detail?: string; errors?: { field: string; message: string }[] }; message?: string };
      setErr(p.problem?.detail ?? p.message ?? "Impor gagal");
      if (p.problem?.errors?.some((x) => x.field.startsWith("columns"))) setShowMap(true);
    } finally {
      setBusy(false);
    }
  };
  const bankList = (banks.data ?? []).filter((b) => b.is_active);
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent side="right" title="Impor mutasi rekening" description="Unggah ekspor mutasi dari internet banking (CSV atau Excel). Baris judul & kolom dideteksi otomatis; mutasi kredit dicocokkan ke pembayaran menunggu verifikasi / invoice terbuka.">
        <div className="space-y-4">
          {properties.length > 1 && (
            <Field label="Property" required>
              <PropertySelect value={pid} onChange={(v) => { setPid(v); setBank(""); }} className="sm:w-full" />
            </Field>
          )}
          <Field label="Rekening bank" help={banks.isError || (pid && !canBanks) ? "Daftar rekening tidak dapat dimuat (butuh izin melihat pengaturan billing) — impor tetap dapat dilakukan tanpa rekening." : "Opsional — dipakai untuk deteksi duplikat per rekening."}>
            <NativeSelect value={bank} onChange={(e) => setBank(e.target.value)} disabled={!pid || banks.isLoading || banks.isError}>
              <option value="">{banks.isLoading ? "Memuat…" : "Tanpa rekening"}</option>
              {bankList.map((b) => <option key={b.id} value={b.id}>{b.bank_name} · {b.account_number} · {b.account_name}{b.property_id ? "" : " (organization)"}</option>)}
            </NativeSelect>
          </Field>
          <div>
            <div className="mb-1 text-xs font-semibold uppercase tracking-wide text-on-surface-variant">Berkas mutasi<span className="ml-0.5 text-error" aria-hidden>*</span></div>
            <button
              type="button"
              onClick={() => inputRef.current?.click()}
              onDragOver={(e) => e.preventDefault()}
              onDrop={(e) => { e.preventDefault(); pick(e.dataTransfer.files?.[0]); }}
              className={cn("flex w-full flex-col items-center justify-center gap-1 rounded-[var(--radius-md)] border border-dashed border-outline-variant px-4 py-6 text-sm text-on-surface-variant transition-colors hover:border-primary hover:bg-surface-container-low", file && "border-primary")}
            >
              <Icon name={file ? "description" : "upload_file"} size={28} aria-hidden />
              {file ? <span className="font-medium text-on-surface">{file.name} · {fmtNumber(Math.round(file.size / 1024))} KB</span> : <span>Klik atau seret berkas ke sini ({ACCEPT_EXT.join(", ")}, maks. 8 MB)</span>}
            </button>
            <input ref={inputRef} type="file" hidden accept={ACCEPT_EXT.join(",")} onChange={(e) => pick(e.target.files?.[0])} aria-label="Pilih berkas mutasi" />
          </div>
          <div className="rounded-[var(--radius-md)] border border-border">
            <button type="button" className="flex w-full items-center justify-between px-3 py-2 text-left text-sm font-semibold" onClick={() => setShowMap((s) => !s)} aria-expanded={showMap}>
              <span>Pemetaan kolom manual (opsional)</span>
              <Icon name={showMap ? "expand_less" : "expand_more"} size={18} aria-hidden />
            </button>
            {showMap && (
              <div className="space-y-3 border-t border-border p-3">
                <p className="text-xs text-on-surface-variant">Isi hanya bila deteksi otomatis gagal. Tulis judul kolom persis seperti di berkas (mis. <code>Tgl. Transaksi</code>) atau nomor kolom <code>#1</code>, <code>#2</code>, … Kolom tanggal dan kredit/jumlah wajib ada.</p>
                <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
                  {COLUMN_KEYS.map((c) => (
                    <Field key={c.key} label={c.label} help={c.hint || undefined}>
                      <Input value={cols[c.key] ?? ""} onChange={(e) => setCols({ ...cols, [c.key]: e.target.value })} placeholder="judul kolom atau #nomor" />
                    </Field>
                  ))}
                  <Field label="Format tanggal">
                    <NativeSelect value={dateFormat} onChange={(e) => setDateFormat(e.target.value)}>{DATE_FORMATS.map((d) => <option key={d.value} value={d.value}>{d.label}</option>)}</NativeSelect>
                  </Field>
                </div>
              </div>
            )}
          </div>
          {err && <Alert variant="critical" title="Impor gagal">{err}</Alert>}
        </div>
        <DialogFooter>
          <Button variant="secondary" onClick={onClose}>Batal</Button>
          <Button icon="upload" disabled={!pid || !file} loading={busy} onClick={submit}>Impor</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
