// Security Shift Management & Housekeeping Shift (PRD P2 v2.1 §8.1–§8.2; keputusan D-P2-05: shift PER DOMAIN, dikelola dari
// modul domainnya — /security/shifts dan /housekeeping/shifts, permission {domain}.shifts.view|manage). Satu komponen,
// parameter domain. Tab di URL (?tab=shifts|roster|on-duty|attendance|handovers) agar deep link notifikasi/dashboard bekerja.
import { useSearchParams } from "react-router-dom";
import { PageHeader } from "@/components/shell/AppShell";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/primitives";
import type { ShiftDomain } from "@/api/types";
import { ShiftDefinitionsTab } from "./ShiftDefinitionsTab";
import { RosterTab } from "./RosterTab";
import { AttendanceTab, OnDutyTab } from "./OnDutyTab";
import { HandoversTab } from "./HandoversTab";
import { SHIFT_PAGE_TITLE } from "./workforce";

const TABS = ["shifts", "roster", "on-duty", "attendance", "handovers"] as const;
type Tab = (typeof TABS)[number];

export default function ShiftsPage({ domain }: { domain: ShiftDomain }) {
  const [sp, setSp] = useSearchParams();
  const raw = sp.get("tab");
  const tab: Tab = (TABS as readonly string[]).includes(raw ?? "") ? (raw as Tab) : "shifts";
  const setTab = (v: string) => {
    const n = new URLSearchParams(sp);
    n.set("tab", v);
    n.delete("id");
    setSp(n, { replace: true });
  };
  return (
    <div>
      <PageHeader
        title={SHIFT_PAGE_TITLE[domain]}
        subtitle={domain === "security" ? "Shift, roster pos jaga, papan on-duty, kehadiran, dan serah terima shift Security." : "Shift, roster zona, papan on-duty, kehadiran, dan serah terima shift Housekeeping."}
      />
      <Tabs value={tab} onValueChange={setTab}>
        <TabsList>
          <TabsTrigger value="shifts" icon="schedule">Shift</TabsTrigger>
          <TabsTrigger value="roster" icon="calendar_month">Roster</TabsTrigger>
          <TabsTrigger value="on-duty" icon="how_to_reg">On-duty</TabsTrigger>
          <TabsTrigger value="attendance" icon="fact_check">Kehadiran</TabsTrigger>
          <TabsTrigger value="handovers" icon="assignment_return">Serah Terima</TabsTrigger>
        </TabsList>
        <TabsContent value="shifts"><ShiftDefinitionsTab domain={domain} /></TabsContent>
        <TabsContent value="roster"><RosterTab domain={domain} /></TabsContent>
        <TabsContent value="on-duty"><OnDutyTab domain={domain} /></TabsContent>
        <TabsContent value="attendance"><AttendanceTab domain={domain} /></TabsContent>
        <TabsContent value="handovers"><HandoversTab domain={domain} /></TabsContent>
      </Tabs>
    </div>
  );
}
