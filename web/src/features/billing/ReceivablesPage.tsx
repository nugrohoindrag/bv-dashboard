// Billing › Receivables (PRD P4 v2.1 §7; NC §34 "Receivables"): Aging (P4-AGE-01) · Penagihan — daftar kerja, log & janji
// bayar (P4-COL-03..04) · Statement of account (P4-OUT-02). Tab di URL: /billing/aging · /billing/collections · /billing/statement
// (/billing/receivables = tab pertama yang diizinkan); ?property_id dipertahankan saat berpindah tab.
import { useNavigate, useSearchParams } from "react-router-dom";
import { PageHeader } from "@/components/shell/AppShell";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/primitives";
import { useAuth } from "@/lib/auth";
import { AgingTab } from "./receivables-aging";
import { CollectionsTab } from "./receivables-collections";
import { StatementTab } from "./receivables-statement";

type Tab = "aging" | "collections" | "statement";

export default function ReceivablesPage({ tab }: { tab?: string }) {
  const nav = useNavigate();
  const [sp] = useSearchParams();
  const { can } = useAuth();
  const canInvoices = can("billing.invoices.view");
  const canCollections = can("billing.collections.view");
  const allowed: Tab[] = [...(canInvoices ? (["aging"] as const) : []), ...(canCollections ? (["collections"] as const) : []), ...(canInvoices ? (["statement"] as const) : [])];
  const current: Tab = allowed.includes(tab as Tab) ? (tab as Tab) : allowed[0] ?? "aging";
  const go = (t: string) => {
    const pid = sp.get("property_id");
    nav(`/billing/${t}${sp.has("property_id") ? `?property_id=${pid ?? ""}` : ""}`);
  };
  return (
    <div>
      <PageHeader title="Receivables" subtitle="Umur piutang, penagihan & janji bayar, dan statement of account per tenant/unit. Semua angka dihitung server di zona waktu property." />
      <Tabs value={current} onValueChange={go}>
        <TabsList>
          {canInvoices && <TabsTrigger value="aging">Aging</TabsTrigger>}
          {canCollections && <TabsTrigger value="collections">Penagihan</TabsTrigger>}
          {canInvoices && <TabsTrigger value="statement">Statement</TabsTrigger>}
        </TabsList>
        <TabsContent value="aging"><AgingTab /></TabsContent>
        <TabsContent value="collections"><CollectionsTab /></TabsContent>
        <TabsContent value="statement"><StatementTab /></TabsContent>
      </Tabs>
    </div>
  );
}
