# Website publik BuildingVision: prerender statis (SEO) disajikan Caddy. Build context = root monorepo (butuh packages/ui).
#   docker build -f website/Dockerfile -t bv-website --build-arg VITE_APP_URL=https://app.buildingvision.id --build-arg VITE_SITE_URL=https://buildingvision.id .
FROM node:22-alpine AS build
ARG VITE_APP_URL=https://app.buildingvision.id
ARG VITE_SITE_URL=https://buildingvision.id
ARG VITE_API_URL=
WORKDIR /repo
COPY packages/ui ./packages/ui
# packages/ui butuh node_modules sendiri (react/@types untuk tsc & motion) karena diimpor sebagai sumber, bukan paket terbit
RUN cd packages/ui && npm ci --legacy-peer-deps
COPY website ./website
WORKDIR /repo/website
RUN npm ci --legacy-peer-deps
ENV VITE_APP_URL=$VITE_APP_URL VITE_SITE_URL=$VITE_SITE_URL VITE_API_URL=$VITE_API_URL
RUN npm run build && node scripts/check-copy.mjs

FROM caddy:2-alpine
COPY --from=build /repo/website/dist /srv
COPY --from=build /repo/website/Caddyfile.static /etc/caddy/Caddyfile
EXPOSE 80
