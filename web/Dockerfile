# Build SPA → static files disajikan oleh Caddy (ADR-008). Image ini hanya berisi hasil build di /srv.
FROM node:22-alpine AS build
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci --legacy-peer-deps
COPY . .
RUN npm run build

FROM caddy:2-alpine
COPY --from=build /app/dist /srv
COPY --from=build /app/Caddyfile.static /etc/caddy/Caddyfile
EXPOSE 80
