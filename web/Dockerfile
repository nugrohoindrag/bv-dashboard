# Build SPA dashboard → static files disajikan oleh Caddy (ADR-008). Image ini hanya berisi hasil build di /srv.
# Build context = root monorepo karena web bergantung pada packages/ui (file:../packages/ui):
#   docker build -f web/Dockerfile -t bv-web .
FROM node:22-alpine AS build
WORKDIR /repo
COPY packages/ui ./packages/ui
RUN cd packages/ui && npm ci --legacy-peer-deps
COPY web ./web
WORKDIR /repo/web
RUN npm ci --legacy-peer-deps
RUN npm run build

FROM caddy:2-alpine
COPY --from=build /repo/web/dist /srv
COPY --from=build /repo/web/Caddyfile.static /etc/caddy/Caddyfile
EXPOSE 80
