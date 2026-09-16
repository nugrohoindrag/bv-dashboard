// Billing › Payments (PRD P1 v1.3 §23; NC §36): daftar pembayaran, verifikasi pembayaran manual, status callback gateway.
import { useMemo, useState } from "react";
import { Link } from "react-router-dom";
import { useTranslation } from "react-i18next";
import type { ColumnDef } from "@tanstack/react-table";
import { PageHeader } from "@/components/shell/AppShell";
import { Badge, Button, Dialog, DialogContent, DialogFooter, Field, NativeSelect, Textarea } from "@/components/ui/primitives";
import { DataGrid } from "@/components/bv/datagrid";
import { RelativeTime, useToast } from "@/components/bv/common";
import { useAction, useList } from "@/api/hooks";
import { useAuth } from "@/lib/auth";
import { fmtDateTime } from "@/lib/format";
import { PAYMENT_STATUS, rp, type Payment } from "./InvoicesPage";

export default function PaymentsPage() {
  const { t } = useTranslation();
  const { propertyId } = useAuth();
  const toast = useToast();
  const [status, setStatus] = useState("");
  const [provider, setProvider] = useState("");
  const [failFor, setFailFor] = useState<Payment | null>(null);
  const [reason, setReason] = useState("");
  const list = useList<Payment>("payments", { property_id: propertyId ?? undefined, status: status || undefined, provider: provider || undefined });
  const rows = list.data?.pages.flatMap((p) => p.data) ?? [];
  const act = useAction<{ id: string; action: string; reason?: string }, Payment>((i) => `payments/${i.id}/${i.action}`, { body: (i) => (i.action === "fail" ? { reason: i.reason } : {}), invalidate: ["list", "one", "all"] });
  const columns = useMemo<ColumnDef<Payment, unknown>[]>(() => [
    { id: "number", header: "Payment", cell: ({ row }) => <span className="font-mono text-[13px] font-semibold">{row.original.payment_number}</span>, size: 150 },
    { id: "invoice", header: "Invoice", cell: ({ row }) => <Link to={`/billing/invoices/${row.original.invoice_id}`} className="font-mono text-[13px] text-primary hover:underline">{row.original.invoice_number}</Link>, size: 150 },
    { id: "tenant", header: t("label.tenant"), cell: ({ row }) => <span className="text-sm">{row.original.tenant_name ?? "—"}</span> },
    { id: "amount", header: "Nominal", cell: ({ row }) => <span className="tnum font-semibold">{rp(row.original.amount)}</span>, size: 140 },
    { id: "provider", header: "Provider", cell: ({ row }) => <span className="text-xs">{row.original.provider_code} · {row.original.method}{row.original.provider_ref ? ` · ${row.original.provider_ref}` : ""}</span>, size: 220 },
    { id: "status", header: t("label.status"), cell: ({ row }) => <div><Badge tone={PAYMENT_STATUS[row.original.status]?.tone ?? "neutral"}>{PAYMENT_STATUS[row.original.status]?.label ?? row.original.status}</Badge>{row.original.verification && <div className="mt-0.5 text-[11px] text-muted-foreground">{row.original.verification === "gateway_callback" ? "callback gateway" : `manual · ${row.original.verified_by_name ?? ""}`}</div>}</div>, size: 170 },
    { id: "paid_at", header: "Dibayar", cell: ({ row }) => <span className="text-xs text-muted-foreground">{row.original.paid_at ? fmtDateTime(row.original.paid_at) : row.original.expires_at ? `berlaku s/d ${fmtDateTime(row.original.expires_at)}` : "—"}</span>, size: 170 },
    { id: "created_at", header: "Dibuat", cell: ({ row }) => <span className="text-xs text-muted-foreground"><RelativeTime value={row.original.created_at} /></span>, size: 110 },
  ], [t]);
  return (
    <div>
      <PageHeader title="Payments" subtitle="Pembayaran gateway ditetapkan hanya oleh callback terverifikasi; pembayaran manual diverifikasi Finance.">
        <div className="flex items-center gap-2">
          <NativeSelect className="w-44" value={status} onChange={(e) => setStatus(e.target.value)}><option value="">Status: {t("label.all")}</option>{Object.entries(PAYMENT_STATUS).map(([k, v]) => <option key={k} value={k}>{v.label}</option>)}</NativeSelect>
          <NativeSelect className="w-44" value={provider} onChange={(e) => setProvider(e.target.value)}><option value="">Provider: {t("label.all")}</option><option value="manual">manual</option><option value="mock_gateway">mock_gateway</option><option value="midtrans">midtrans</option><option value="xendit">xendit</option></NativeSelect>
        </div>
      </PageHeader>
      <DataGrid columns={columns} rows={rows} rowId={(r) => r.id} loading={list.isLoading} isFiltered={!!status || !!provider} empty={{ message: "Belum ada pembayaran." }} hasMore={list.hasNextPage} onLoadMore={() => list.fetchNextPage()} loadingMore={list.isFetchingNextPage}
        rowActions={(r) => [
          ...(r.allowed_actions.includes("verify") ? [{ label: "Verifikasi (bukti diterima)", icon: "task_alt", onSelect: () => act.mutateAsync({ id: r.id, action: "verify" }).then(() => toast.success("Pembayaran diverifikasi")).catch(toast.error) }] : []),
          ...(r.allowed_actions.includes("fail") ? [{ label: "Tandai gagal…", icon: "block", destructive: true, onSelect: () => setFailFor(r) }] : []),
        ]} />
      {failFor && (
        <Dialog open onOpenChange={(o) => !o && setFailFor(null)}>
          <DialogContent title={`Tandai gagal ${failFor.payment_number}`}>
            <Field label="Alasan"><Textarea rows={3} value={reason} onChange={(e) => setReason(e.target.value)} /></Field>
            <DialogFooter><Button variant="secondary" onClick={() => setFailFor(null)}>Batal</Button><Button variant="destructive" loading={act.isPending} onClick={() => act.mutateAsync({ id: failFor.id, action: "fail", reason }).then(() => { setFailFor(null); setReason(""); }).catch(toast.error)}>Konfirmasi</Button></DialogFooter>
          </DialogContent>
        </Dialog>
      )}
    </div>
  );
}
