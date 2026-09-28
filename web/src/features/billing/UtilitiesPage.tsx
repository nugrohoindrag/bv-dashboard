// Billing › Utilities (PRD P4 v2.1 §5.3 P4-UTL-01..05; NC "Utilities"): Meter · Pembacaan · Tarif. Tab di URL:
// /billing/meters (meter) · /billing/meters/:tab (meters | readings | tariffs) · /billing/meters/readings/:id (detail pembacaan —
// deep link notifikasi "pembacaan meter perlu review"). Pemakaian → item invoice utility lewat billing rule pemakaian meter.
import { useNavigate, useParams, useSearchParams } from "react-router-dom";
import { PageHeader } from "@/components/shell/AppShell";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/primitives";
import { useAuth } from "@/lib/auth";
import { MetersTab } from "./utilities-meters";
import { ReadingsTab } from "./utilities-readings";
import { TariffsTab } from "./utilities-tariffs";
import { TABS, type UtilTab } from "./utilities-model";

export default function UtilitiesPage({ tab: tabProp }: { tab?: string }) {
  const params = useParams();
  const nav = useNavigate();
  const [sp] = useSearchParams();
  const { can } = useAuth();
  const canMeters = can("billing.meters.view");
  const canReadings = can("billing.meter_readings.view");
  const allowed: UtilTab[] = TABS.filter((t) => (t === "readings" ? canReadings : canMeters));
  const want = (tabProp ?? params.tab ?? "meters") as UtilTab;
  const tab: UtilTab = allowed.includes(want) ? want : allowed[0] ?? "meters";
  const readingId = tabProp === "readings" ? params.id : undefined;
  const go = (t: string) => {
    const keep = sp.has("property_id") ? `?property_id=${sp.get("property_id") ?? ""}` : "";
    nav(t === "meters" ? `/billing/meters${keep}` : `/billing/meters/${t}${keep}`);
  };
  return (
    <div>
      <PageHeader title="Utilities" subtitle="Meter listrik & air, pembacaan bulanan (Web / Staff App, dengan foto & validasi angka mundur/lonjakan), dan tarif per kWh / m³." />
      <Tabs value={tab} onValueChange={go}>
        <TabsList>
          {canMeters && <TabsTrigger value="meters">Meter</TabsTrigger>}
          {canReadings && <TabsTrigger value="readings">Pembacaan</TabsTrigger>}
          {canMeters && <TabsTrigger value="tariffs">Tarif</TabsTrigger>}
        </TabsList>
        <TabsContent value="meters"><MetersTab /></TabsContent>
        <TabsContent value="readings"><ReadingsTab selectedId={readingId} /></TabsContent>
        <TabsContent value="tariffs"><TariffsTab /></TabsContent>
      </Tabs>
    </div>
  );
}
