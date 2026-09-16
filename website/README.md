# BuildingVision Website (public)

Landing, solutions, platform, pricing, resources, download apps, book a demo (Website PRD v1.1). Static site (prerendered React)
that consumes only the public API; Login and Start Free Trial send visitors to the dashboard (`web/`).

```bash
npm ci --legacy-peer-deps
npm run dev            # http://localhost:5175 (proxy /api → :8080)
npm run build          # tsc + vite build (client) + vite build --ssr + prerender 29 routes → dist/ (+ sitemap.xml, robots.txt)
npm run check:copy     # PRD §38: fails if any user-facing copy contains an em dash
npm run images         # re-download and re-encode photos from src/content/images.json (see ASSETS.md)
npm run preview        # serve dist/ on :4173
```

Environment (build time): `VITE_APP_URL` (dashboard, default `http://localhost:5173`), `VITE_SITE_URL` (canonical URL),
`VITE_API_URL` (empty = same origin `/api`).

Structure: `src/content/*` (site map, solutions, platform pages, FAQ, image manifest) · `src/components/*` (layout, blocks,
mocks, Picture) · `src/pages/*` · `src/lib/{config,head,api,analytics}.ts` · `scripts/{prerender,images,check-copy}.mjs`.

Design: the same tokens as the dashboard (`@buildingvision/ui` tokens + `bv/palette.css`), Tailwind only as aliases to tokens,
Material Symbols subset shared with the dashboard (`packages/ui` `npm run fonts:vendor` also scans `website/src`).

Deploy: `website/Dockerfile` (context = repo root) → Caddy static image; `infra/docker-compose.yml` service `website` +
Caddy site block on `BV_WEBSITE_DOMAIN`. Changing an app download link does not require a website deploy: the page reads
`GET /api/v1/public/app-downloads` at runtime.
