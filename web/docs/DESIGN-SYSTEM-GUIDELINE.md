# BuildingVision Web — Design System Guideline

**Status:** mengikat untuk semua pekerjaan UI di `web/`.
**Sistem hulu:** Morphic / Nexus UI (Material 3 + soft morphic surfaces), sumber di `D:\Design System` — sistem yang sama dengan Factory Vision.
**Identitas:** palet teal BuildingVision (`packages/ui/src/bv/palette.css`), diturunkan dari `design-tokens/tokens.json` sehingga dashboard, Staff App (Flutter), dan Tenant PWA memakai satu brand.

Dokumen ini mengadaptasi `Factory Vision/Docs/DESIGN-SYSTEM-GUIDELINE.md`; aturannya sama, hanya nama lapisan (`bv`, bukan `fv`) dan warnanya yang berbeda.

## 1. Di mana design system berada

| Lapisan | Path | Pemilik |
|---|---|---|
| Sistem hulu | `D:\Design System\src` | Design system itu sendiri. Perbaikan komponen dilakukan **di sini**, lalu `ds:pull`. |
| Mirror | `packages/ui/src` (semua kecuali `src/bv`) | **Tidak ada.** Salinan byte-per-byte hulu; jangan diedit. |
| Ekstensi produk | `packages/ui/src/bv` | BuildingVision. Hanya token: palet, logo, hero, `SurfaceCard`, `MetricCard`, `DataTable`, `FilterChip`, `Dialog`, `RowActionMenu`, `DateField`, CSS shell (`sidebar.css`, `layout.css`, `table-header.css`, `mirror-fixes.css`), font self-hosted. |
| Primitif produk | `web/src/components/ui/primitives.tsx` | Kompat API lama (`Button`, `Card`, `Dialog`, `Tabs`, `Field`, …) yang **mengompos** komponen DS + token. |
| Layar | `web/src/features/*` | Menyusun, tidak me-restyle. |

```bash
npm run ds:check   # (di web/ atau packages/ui) mirror bersih + semua ikon punya glyph
npm run ds:pull    # (packages/ui) tarik ulang mirror dari hulu; src/bv tidak disentuh
npm run fonts:vendor  # (packages/ui) regenerasi subset Material Symbols setelah menambah ikon baru
```

`@buildingvision/ui` dipasang ke `web` sebagai `file:../packages/ui` (symlink). `vite.config.ts` memakai `resolve.dedupe` untuk react/motion dan `server.fs.allow` agar font di `packages/ui` bisa disajikan saat dev; `tsconfig.app.json` memetakan `react`/`motion` ke satu salinan tipe.

## 2. Aturan

### 2.1 Tidak ada warna literal
Tidak ada hex/`rgba()` di `web/src` (kecuali `lib/status-map.ts` yang GENERATED dan tidak dipakai untuk warna). Warna lewat:
- alias kontrak `--color-*` (`var(--color-primary)`, `var(--color-on-surface-variant)`, `var(--color-border)`), atau
- kelas Tailwind yang sudah di-alias ke token DS di `src/styles/theme.css` (`bg-surface`, `text-on-surface-variant`, `border-border`, `bg-primary text-on-primary`, `bg-success-container text-on-success-container`), atau
- helper `Tone` dari `@buildingvision/ui/bv` (`toneContainer[tone]`, `toneOnContainer[tone]`, `toneColor[tone]`).

Tailwind di repo ini **hanya utilitas layout/tipografi**; nama warnanya adalah alias token, bukan palet Tailwind. Radius `rounded-*` membaca `--radius-*` DS.

### 2.2 Makna, bukan dekorasi
| Makna | Tone | Aksen | Fill solid |
|---|---|---|---|
| Brand / pilihan aktif | `primary` | `--color-primary` | `--color-primary-container` |
| Baik / selesai | `success` | `--color-success` | `--color-success-container` |
| Perhatian / SLA risk | `warning` | `--color-warning` | `--color-warning-container` |
| Gagal / overdue / breach | `error` | `--color-error` | `--color-error-container` |
| Informasi / hitungan | `info` | `--color-info` | `--color-info-container` |
| Redup | `neutral` | `--color-on-surface-variant` | `--color-surface-container-high` |

Status object memakai `contracts/status-map.yaml` → `StatusBadge` memetakan `semantic` ke `Tone` (critical → error). Brand ≠ status (PRD §25.1).

### 2.3 Terisi dan solid secara default
- Kartu = `Card` (primitives) = `SurfaceCard` (surface + hairline `--color-border` + `--radius-xl` + `--elevation-1`). Rail kiri lewat `railTone`, bukan `border-l-4`.
- Chip/badge/pill: pasangan `container` + `on-container`, tidak pernah wash translusen.
- Pilihan aktif = satu fill: `--color-primary`/`--color-on-primary` (tab aktif, `FilterChip` terpilih, header tabel, hero, CTA).
- Tabel: header solid primary via `.bv-table` (`bv/table-header.css`); `DataGrid` (TanStack) sudah memakainya; tabel tulis tangan memakai `className="bv-table"` di dalam `.bv-table-scroll`, `bv-num` untuk kolom angka.
- Garis hanya `--color-border`; `--color-outline-variant` untuk ikon/teks sekunder.
- Hero `BuildingHero` (bv) solid primary dengan skyline di-stroke `on-primary`; gradient tidak dipakai di UI produk.

### 2.4 Kompos, jangan restyle
Ambil komponen DS dulu (`@buildingvision/ui`), lalu ekstensi `@buildingvision/ui/bv`, lalu primitif produk. Kekurangan komponen → tambah di `src/bv` (token-only) atau perbaiki di hulu, bukan inline di mirror. Override CSS global terhadap markup mirror hanya di `src/bv/*.css` (contoh: header `AdvancedDataTable`).

### 2.5 Ikon: satu keluarga
Material Symbols Rounded via `<Icon name="…" />` (`@buildingvision/ui`). `lucide-react` sudah dihapus; jangan ditambahkan lagi. Font ikon **di-subset** ke nama yang dipakai produk — setelah menambah nama ikon baru jalankan `npm run fonts:vendor` di `packages/ui` (butuh internet), lalu `ds:check`.

### 2.6 Tipografi
`--font-family` = Roboto Flex / Inter (self-hosted `bv/fonts.css`). Angka tabular (`tnum`) untuk metrik dan kolom angka.

### 2.7 Tema
`data-theme` pada `<html>` = `light` | `dark` (toggle di top bar, tersimpan `bv.theme`). Tidak ada `data-accent`. Setiap layar harus benar di kedua tema — otomatis bila tidak ada warna literal.

## 3. Checklist sebelum merge UI
1. `npm run ds:check` (packages/ui) lulus.
2. `grep -rnE "#[0-9a-fA-F]{3,8}\b|rgba\(" web/src --include=*.tsx` kosong.
3. Tidak ada `var(--md-sys-*)` di `web/src` (pakai alias `--color-*`).
4. Tidak ada `lucide-react`/ikon lain; ikon baru sudah di-vendor.
5. `npm run typecheck`, `npm run lint`, `npm test`, `npm run build` lulus.
6. Layar dilihat di light **dan** dark.
