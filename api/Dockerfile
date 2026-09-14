# syntax=docker/dockerfile:1.7
# Multi-stage: build statis Go → distroless. Satu image untuk api / worker / bvctl (pilih lewat command).
FROM golang:1.26-alpine AS build
WORKDIR /src
RUN apk add --no-cache git ca-certificates tzdata
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
ARG VERSION=dev
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o /out/api ./cmd/api && \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o /out/worker ./cmd/worker && \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o /out/bvctl ./cmd/bvctl

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=build /out/api /out/worker /out/bvctl /app/
ENV TZ=UTC BV_HTTP_ADDR=:8080
EXPOSE 8080 9091
USER nonroot:nonroot
CMD ["/app/api"]
