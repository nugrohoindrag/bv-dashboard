// Impor invoice dari CSV/Excel (PRD P4 v2.1 P4-BRL-05; migrasi/koreksi massal): pilih file → periksa (dry run, error per baris)
// → impor (semua-atau-tidak: bila ada error tidak ada invoice yang dibuat). Baris dengan `group` sama → satu invoice banyak item.
import { useRef, useState } from "react";
import { Icon } from "@buildingvision/ui";
import { Alert, Button, Checkbox, DialogFooter, Drawer, Table, TBody, TD, TH, THead, TR } from "@/components/ui/primitives";
import { CellText } from "@/components/bv/cells";
import { useToast } from "@/components/bv/common";
import { useInvalidate } from "@/api/hooks";
import { api, uuid } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { PropertySelect, SectionLabel, SummaryTile } from "./shared";
import { fileToBase64, money, usePropertyChoice, type InvoiceImportResult } from "./types";

const COLUMNS: { name: string; note: string }[] = [
  { name: "tenant_code | unit", note: "Minimal salah satu: kode tenant atau nomor unit" },
  { name: "invoice_type", note: "service_charge, ipl, utility, electricity, water, parking, sinking_fund, deposit, rental, facility, additional_charge, other (default service_charge)" },
  { name: "description", note: "Wajib — deskripsi item" },
  { name: "quantity", note: "Opsional, default 1 (desimal pakai titik/koma)" },
  { name: "unit_price | amount", note: "Harga satuan, atau jumlah (qty = 1)" },
  { name: "due_date", note: "Wajib — YYYY-MM-DD atau DD/MM/YYYY" },
  { name: "period_start, period_end", note: "Opsional — tanggal periode" },
  { name: "external_ref", note: "Opsional — nomor dokumen akuntansi" },
  { name: "group", note: "Opsional — baris dengan group sama menjadi satu invoice (item berbeda)" },
];

const TEMPLATE = "tenant_code,unit,invoice_type,description,quantity,unit_price,due_date,period_start,period_end,external_ref,group\nTN-0001,,service_charge,Service charge Oktober 2026,1,1500000,2026-10-15,2026-10-01,2026-10-31,,A1\nTN-0001,,sinking_fund,Sinking fund Oktober 2026,1,150000,2026-10-15,2026-10-01,2026-10-31,,A1\n,1203,water,Air Oktober 2026,12.5,15000,2026-10-15,2026-10-01,2026-10-31,,\n";

export function ImportInvoicesDialog({ onClose }: { onClose: () => void }) {
  const toast = useToast();
  const invalidate = useInvalidate();
  const { can } = useAuth();
  const [pid, setPid] = usePropertyChoice();
  const [file, setFile] = useState<File | null>(null);
  const [result, setResult] = useState<InvoiceImportResult | null>(null);
  const [issueNow, setIssueNow] = useState(false);
  const [busy, setBusy] = useState<"check" | "import" | null>(null);
  const [error, setError] = useState<string | null>(null);
  const inputRef = useRef<HTMLInputElement>(null);
  const canIssue = can("billing.invoices.issue", pid || null);
  const checked = !!result && result.dry_run && result.errors.length === 0 && result.invoices > 0;

  const run = async (dryRun: boolean) => {
    if (!file || !pid) return;
    setBusy(dryRun ? "check" : "import");
    setError(null);
    try {
      const content = await fileToBase64(file);
      const out = await api<InvoiceImportResult>("invoices/import", { body: { property_id: pid, file_name: file.name, content_base64: content, dry_run: dryRun, issue_now: !dryRun && issueNow && canIssue }, idempotencyKey: uuid() });
      setResult(out);
      if (!dryRun && out.errors.length === 0) {
        invalidate("list", "one", "all");
        toast.success(`${out.invoices} invoice diimpor${issueNow ? " & diterbitkan" : " sebagai draft"} (${money(out.total_amount)})`);
        onClose();
      }
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(null);
    }
  };

  const downloadTemplate = () => {
    const a = document.createElement("a");
    a.href = URL.createObjectURL(new Blob([TEMPLATE], { type: "text/csv;charset=utf-8" }));
    a.download = "template-impor-invoice.csv";
    a.click();
    setTimeout(() => URL.revokeObjectURL(a.href), 1000);
  };

  return (
    <Drawer open onClose={onClose} width={720} title="Impor invoice dari CSV" description="Untuk migrasi atau koreksi massal. Periksa dulu (dry run); bila ada error per baris, tidak ada invoice yang dibuat.">
      <div className="space-y-4">
        <PropertySelect value={pid} onChange={(v) => { setPid(v); setResult(null); }} required />
        <div className="rounded-[var(--radius-md)] border border-dashed border-border p-4 text-center">
          <input ref={inputRef} type="file" accept=".csv,.txt,.xlsx,.xlsm,text/csv" className="hidden" onChange={(e) => { setFile(e.target.files?.[0] ?? null); setResult(null); setError(null); }} />
          <Icon name="upload_file" size={28} className="text-on-surface-variant" />
          <div className="mt-1 text-sm">{file ? <><b>{file.name}</b> · {Math.max(1, Math.round(file.size / 1024))} KB</> : "CSV (pemisah , ; tab |) atau Excel .xlsx — sheet pertama"}</div>
          <div className="mt-2 flex flex-wrap justify-center gap-2">
            <Button variant="secondary" size="sm" icon="folder_open" onClick={() => inputRef.current?.click()}>{file ? "Ganti file" : "Pilih file"}</Button>
            <Button variant="ghost" size="sm" icon="download" onClick={downloadTemplate}>Template CSV</Button>
          </div>
        </div>

        <details className="rounded-[var(--radius-md)] border border-border px-3 py-2 text-sm" open={!file}>
          <summary className="cursor-pointer font-semibold">Kolom yang diharapkan (baris pertama = judul kolom)</summary>
          <ul className="mt-2 space-y-1">
            {COLUMNS.map((c) => <li key={c.name}><code className="font-mono text-xs">{c.name}</code> — <span className="text-on-surface-variant">{c.note}</span></li>)}
          </ul>
        </details>

        {error && <Alert variant="critical" title="Impor gagal">{error}</Alert>}

        {result && (
          <div className="space-y-3">
            <div className="grid grid-cols-3 gap-2">
              <SummaryTile label="Baris" value={result.rows} />
              <SummaryTile label="Invoice" value={result.invoices} />
              <SummaryTile label="Total (sebelum pajak)" value={money(result.total_amount)} />
            </div>
            {result.errors.length > 0 ? (
              <div>
                <SectionLabel>{result.errors.length} error — perbaiki file lalu periksa ulang</SectionLabel>
                <Table>
                  <THead><tr><TH className="bv-num">Baris</TH><TH>Kolom</TH><TH>Pesan</TH></tr></THead>
                  <TBody>
                    {result.errors.map((e, i) => <TR key={i}><TD className="bv-num whitespace-nowrap">{e.row}</TD><TD className="whitespace-nowrap"><code className="font-mono text-xs">{e.field}</code></TD><TD><CellText max={360}>{e.message}</CellText></TD></TR>)}
                  </TBody>
                </Table>
              </div>
            ) : result.dry_run ? (
              <Alert variant="success" title="File valid">{result.invoices} invoice siap dibuat dari {result.rows} baris. Pajak dihitung dari pengaturan billing property saat impor.</Alert>
            ) : null}
          </div>
        )}

        {checked && canIssue && <Checkbox label="Terbitkan langsung (nomor INV diberikan & tenant menerima notifikasi)" checked={issueNow} onCheckedChange={setIssueNow} />}

        <DialogFooter>
          <Button variant="secondary" onClick={onClose}>Tutup</Button>
          <Button variant={checked ? "secondary" : "primary"} icon="fact_check" loading={busy === "check"} disabled={!file || !pid || !!busy} onClick={() => run(true)}>Periksa</Button>
          <Button icon="upload" loading={busy === "import"} disabled={!checked || !!busy} onClick={() => run(false)}>Impor {result?.invoices ? `${result.invoices} invoice` : ""}</Button>
        </DialogFooter>
      </div>
    </Drawer>
  );
}
