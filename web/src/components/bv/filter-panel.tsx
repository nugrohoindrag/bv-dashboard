// Filter lanjutan (29 Sep 2026, referensi "Advance Filter"): tombol Filter membuka panel dua kolom — kategori di kiri,
// pilihan di kanan (daftar yang bisa dicari, picker, atau rentang tanggal). Filter langsung tersimpan di URL (useUrlFilters)
// sehingga daftar di belakang panel ikut diperbarui; footer: Hapus semua · Selesai.
import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { Icon } from "@buildingvision/ui";
import { Button, DatePicker, Popover, SearchInput } from "@/components/ui/primitives";
import { cn } from "@/lib/utils";
import { activeIn, paramsOf, type FilterCategory, type UrlFilters } from "./filter-helpers";

export function FilterPanelButton({ cats, f, activeCount }: { cats: FilterCategory[]; f: UrlFilters; activeCount: number }) {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  const [sel, setSel] = useState(cats[0]?.key ?? "");
  const [q, setQ] = useState("");
  const cur = cats.find((c) => c.key === sel) ?? cats[0];
  const clearAll = () => f.set(Object.fromEntries(cats.flatMap(paramsOf).map((p) => [p, null])));
  const shown = useMemo(() => (cur?.kind === "options" ? cur.options.filter((o) => o.label.toLowerCase().includes(q.trim().toLowerCase())) : []), [cur, q]);
  if (!cats.length) return null;
  return (
    <Popover
      open={open}
      onOpenChange={setOpen}
      width={620}
      className="!p-0 overflow-hidden"
      trigger={
        <Button variant="secondary" size="sm" icon="filter_list" aria-expanded={open} aria-haspopup="dialog">
          {t("label.filter")}
          {activeCount > 0 && <span className="ml-0.5 inline-flex h-5 min-w-5 items-center justify-center rounded-full bg-primary px-1.5 text-[11px] font-semibold text-on-primary">{activeCount}</span>}
        </Button>
      }
    >
      <div role="dialog" aria-label="Filter lanjutan" className="flex max-h-[min(460px,70vh)] flex-col">
        <div className="flex items-center justify-between border-b border-border px-4 py-3">
          <span className="text-body font-semibold">Filter lanjutan</span>
          <button type="button" className="inline-flex h-7 w-7 items-center justify-center rounded-full text-on-surface-variant hover:bg-surface-container" onClick={() => setOpen(false)} aria-label="Tutup">
            <Icon name="close" size={18} />
          </button>
        </div>
        <div className="flex min-h-0 flex-1">
          {/* kategori */}
          <ul className="w-48 shrink-0 space-y-0.5 overflow-y-auto border-r border-border p-2" role="tablist" aria-orientation="vertical">
            {cats.map((c) => {
              const n = activeIn(c, f);
              const on = c.key === cur?.key;
              return (
                <li key={c.key}>
                  <button
                    type="button"
                    role="tab"
                    aria-selected={on}
                    onClick={() => { setSel(c.key); setQ(""); }}
                    className={cn("flex w-full items-center gap-2 rounded-xl px-2.5 py-2 text-left text-sm transition-colors", on ? "bg-surface-container font-semibold text-on-surface" : "text-on-surface-variant hover:bg-surface-container-low hover:text-on-surface")}
                  >
                    <Icon name={c.icon} size={17} className="shrink-0" />
                    <span className="min-w-0 flex-1 truncate">{c.label}</span>
                    {n > 0 ? <span className="inline-flex h-5 min-w-5 items-center justify-center rounded-full bg-primary px-1.5 text-[11px] font-semibold text-on-primary">{n}</span> : <Icon name="chevron_right" size={16} className="shrink-0 opacity-50" />}
                  </button>
                </li>
              );
            })}
          </ul>
          {/* pilihan */}
          <div className="flex min-w-0 flex-1 flex-col">
            {cur?.kind === "options" && (
              <>
                {cur.options.length > 6 && (
                  <div className="p-3 pb-1">
                    <SearchInput value={q} onChange={(e) => setQ(e.target.value)} placeholder={`Cari ${cur.label.toLowerCase()}…`} aria-label={`Cari ${cur.label}`} />
                  </div>
                )}
                <ul className="min-h-0 flex-1 overflow-y-auto p-2" role="radiogroup" aria-label={cur.label}>
                  {[{ value: "", label: t("label.all") }, ...shown].map((o) => {
                    const on = (f.get(cur.param) || "") === o.value;
                    return (
                      <li key={o.value || "__all"}>
                        <button type="button" role="radio" aria-checked={on} onClick={() => f.set({ [cur.param]: o.value || null })} className={cn("flex w-full items-center gap-3 rounded-xl px-3 py-2 text-left text-sm transition-colors hover:bg-surface-container-low", on && "font-semibold")}>
                          <span className={cn("inline-flex h-[18px] w-[18px] shrink-0 items-center justify-center rounded-full border-2", on ? "border-primary" : "border-outline-variant")}>
                            {on && <span className="h-2 w-2 rounded-full bg-primary" />}
                          </span>
                          <span className="min-w-0 flex-1 truncate">{o.label}</span>
                        </button>
                      </li>
                    );
                  })}
                  {shown.length === 0 && q && <li className="px-3 py-6 text-center text-sm text-on-surface-variant">Tidak ada pilihan yang cocok.</li>}
                </ul>
              </>
            )}
            {cur?.kind === "date" && (
              <div className="space-y-3 p-4">
                <label className="block text-sm"><span className="mb-1 block text-on-surface-variant">Dari tanggal</span><DatePicker value={f.get(cur.from).slice(0, 10)} onChange={(v) => f.set({ [cur.from]: v })} aria-label="Dari tanggal" /></label>
                <label className="block text-sm"><span className="mb-1 block text-on-surface-variant">Sampai tanggal</span><DatePicker value={f.get(cur.to).slice(0, 10)} onChange={(v) => f.set({ [cur.to]: v ? v + "T23:59:59Z" : "" })} aria-label="Sampai tanggal" /></label>
              </div>
            )}
            {cur?.kind === "node" && <div className="p-4">{cur.node}</div>}
          </div>
        </div>
        <div className="flex items-center justify-between gap-2 border-t border-border px-4 py-3">
          <button type="button" className="text-sm font-medium text-on-surface-variant underline-offset-4 hover:text-on-surface hover:underline" onClick={clearAll}>Hapus semua</button>
          <Button size="sm" onClick={() => setOpen(false)}>Selesai</Button>
        </div>
      </div>
    </Popover>
  );
}
