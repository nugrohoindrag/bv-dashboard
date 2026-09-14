// Master Data: kategori Service Request (read-only dari seed, PRD §18.2), tautan ke Equipment & lokasi.
import { Link } from "react-router-dom";
import { Card, CardContent, CardHeader, CardTitle, THead, TBody, TD, TH, TR, Table } from "@/components/ui/primitives";
import { AsyncState } from "@/components/bv/common";
import { PriorityBadge } from "@/components/bv/badges";
import { useSRCategories } from "@/features/operations/FindingDialogs";

export default function MasterDataSection() {
  const cats = useSRCategories();
  return (
    <div className="space-y-5">
      <Card>
        <CardHeader><CardTitle>Kategori Service Request</CardTitle></CardHeader>
        <CardContent className="px-0">
          <AsyncState query={cats}>
            {(list) => (
              <Table>
                <THead><tr><TH>Kode</TH><TH>Nama</TH><TH>Domain default</TH><TH>Prioritas default</TH><TH>Aktif</TH></tr></THead>
                <TBody>
                  {list.map((c) => (
                    <TR key={c.id}><TD className="font-mono text-xs">{c.code}</TD><TD>{c.name}</TD><TD>{c.default_domain ?? "—"}</TD><TD><PriorityBadge priority={c.default_priority} /></TD><TD>{c.is_active ? "Ya" : "Tidak"}</TD></TR>
                  ))}
                </TBody>
              </Table>
            )}
          </AsyncState>
          <p className="px-4 pt-3 text-xs text-muted-foreground">Kategori dikelola lewat seed/import (bvctl import) pada P0.</p>
        </CardContent>
      </Card>
      <Card>
        <CardHeader><CardTitle>Master lainnya</CardTitle></CardHeader>
        <CardContent className="grid grid-cols-3 gap-3 text-sm">
          <Link to="/assets/equipment" className="rounded-md border border-border p-3 hover:bg-muted"><div className="font-medium">Equipment</div><div className="text-xs text-muted-foreground">Kategori & tipe peralatan untuk aset</div></Link>
          <Link to="/property/properties" className="rounded-md border border-border p-3 hover:bg-muted"><div className="font-medium">Lokasi</div><div className="text-xs text-muted-foreground">Property → Building → Floor → Unit</div></Link>
          <Link to="/settings/checklists" className="rounded-md border border-border p-3 hover:bg-muted"><div className="font-medium">Checklist Template</div><div className="text-xs text-muted-foreground">Template inspeksi/patrol/cleaning</div></Link>
          <Link to="/security/patrol/checkpoints" className="rounded-md border border-border p-3 hover:bg-muted"><div className="font-medium">Checkpoint & Rute Patrol</div><div className="text-xs text-muted-foreground">Titik QR dan urutan patroli</div></Link>
          <Link to="/housekeeping/schedule" className="rounded-md border border-border p-3 hover:bg-muted"><div className="font-medium">Jadwal Cleaning</div><div className="text-xs text-muted-foreground">Jadwal berulang per lokasi</div></Link>
          <Link to="/settings/sla-policies" className="rounded-md border border-border p-3 hover:bg-muted"><div className="font-medium">SLA Policy</div><div className="text-xs text-muted-foreground">Target response/resolution</div></Link>
        </CardContent>
      </Card>
    </div>
  );
}
