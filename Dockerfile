# 源码构建与 CI 发布镜像共用运行层；发布镜像复用已校验的程序。
FROM --platform=$BUILDPLATFORM node:22-alpine AS frontend
WORKDIR /src/web
COPY web/package*.json ./
RUN npm ci --no-audit --no-fund
COPY web/ ./
RUN npm run build
WORKDIR /src
COPY scripts/ ./scripts/
RUN node scripts/prepare-webui.mjs

FROM --platform=$BUILDPLATFORM golang:1.26.10-alpine AS backend
WORKDIR /src
ENV CGO_ENABLED=0
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ cmd/
COPY internal/ internal/
COPY migrations/ migrations/
COPY --from=frontend /src/internal/webui/dist ./internal/webui/dist
ARG TARGETOS
ARG TARGETARCH
ARG TARGETVARIANT
RUN case "${TARGETARCH}/${TARGETVARIANT}" in \
      arm/v5) export GOARM=5 ;; \
      arm/v6) export GOARM=6 ;; \
      arm/*) echo "不支持的 ARM 变体" >&2; exit 1 ;; \
    esac \
    && GOOS=$TARGETOS GOARCH=$TARGETARCH go build -tags=with_frontend -trimpath -ldflags="-s -w" -o /out/opsdeck ./cmd/opsdeck

FROM --platform=$BUILDPLATFORM alpine:3.23 AS runtime-files
RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -g 10001 opsdeck \
    && adduser -D -H -u 10001 -G opsdeck opsdeck \
    && mkdir -p /run/opsdeck-secrets \
    && chown 10001:10001 /run/opsdeck-secrets \
    && chmod 0700 /run/opsdeck-secrets

FROM alpine:3.23 AS runtime
COPY --from=runtime-files /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=runtime-files /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=runtime-files /etc/passwd /etc/passwd
COPY --from=runtime-files /etc/group /etc/group
COPY --from=runtime-files --chown=10001:10001 /run/opsdeck-secrets/ /run/opsdeck-secrets/
WORKDIR /app
COPY --chmod=0755 deploy/docker-entrypoint.sh /usr/local/bin/opsdeck-entrypoint
USER 10001:10001
ENV LISTEN_ADDR=0.0.0.0:8080 MASTER_KEY_FILE=/run/opsdeck-secrets/master.key
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s --start-period=15s --retries=3 \
    CMD wget -q -O /dev/null http://127.0.0.1:8080/healthz || exit 1
ENTRYPOINT ["/usr/local/bin/opsdeck-entrypoint"]
CMD ["all"]

FROM --platform=$BUILDPLATFORM alpine:3.23 AS release-binary
ARG TARGETARCH
ARG TARGETVARIANT
COPY .release/binaries/ /binaries/
RUN platform="${TARGETARCH}" \
    && if [ "$TARGETARCH" = arm ]; then platform="arm${TARGETVARIANT}"; fi \
    && cp "/binaries/opsdeck-linux-${platform}/opsdeck" /opsdeck \
    && chmod 0755 /opsdeck

FROM runtime AS release
COPY --from=release-binary /opsdeck /app/opsdeck

FROM runtime AS standalone
COPY --from=backend /out/opsdeck /app/opsdeck
