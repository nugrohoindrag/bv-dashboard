# BuildingVision Web (Dashboard)

React 19 SPA (Vite, TypeScript) untuk Property Manager / Building Manager / supervisor. UI dibangun di atas design system
**Morphic / Nexus** (mirror di [`../packages/ui`](../packages/ui), sistem yang sama dengan Factory Vision) dengan palet
**teal BuildingVision** — satu identitas dengan Staff App dan Tenant PWA.

Aturan UI: [docs/DESIGN-SYSTEM-GUIDELINE.md](docs/DESIGN-SYSTEM-GUIDELINE.md) (wajib dibaca sebelum menyentuh UI).

## Menjalankan

```bash
npm ci --legacy-peer-deps       # @buildingvision/ui = file:../packages/ui (symlink)
npm run dev                     # http://localhost:5173, proxy /api & /public → BV_API_URL (default :8080)
```

Login demo: lihat README monorepo. Tema terang/gelap: toggle di top bar (tersimpan di `localStorage bv.theme`).

## Perintah

| Area | Perintah |
|---|---|
| Typecheck / lint / test / build | `npm run typecheck` · `npm run lint` · `npm test` · `npm run build` |
| Design system mirror + ikon | `npm run ds:check` (jalankan `npm run ds:pull` / `npm run fonts:vendor` di `../packages/ui`) |
| Token status map dari contracts | `npm run gen` (menulis `src/lib/status-map.ts`; warna kini dari `packages/ui/src/bv/palette.css`) |

## Struktur

```text
src/
├── app/App.tsx              # router + auth guard
├── components/shell/        # AppShell (sidebar panel teal ala konsol FV, top bar, tema), GlobalSearch, NotificationInbox
├── components/ui/primitives.tsx  # Button/Card/Dialog/Tabs/Field/... = kompat API di atas komponen DS + token
├── components/bv/           # DataGrid (.bv-table), badges (tone pair), cards (SurfaceCard rail), pickers, timeline, checklist
├── features/                # overview (BuildingHero + KPI), operations, engineering, security, housekeeping, property, tenant, assets, settings
├── styles/theme.css         # Tailwind v4: HANYA layout; nama warna = alias token DS
└── lib/                     # api client, auth, format Indonesia, status-map (GENERATED)
```

Impor CSS di `main.tsx` berurutan: `tokens.css` → `bv/palette.css` → `bv/*.css` → `styles/theme.css`.
