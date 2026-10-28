FROM node:22-alpine AS frontend
WORKDIR /src/web
COPY web/package*.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM golang:1.26.10-alpine AS backend
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ cmd/
COPY internal/ internal/
COPY migrations/ migrations/
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /opsdeck ./cmd/opsdeck

FROM alpine:3.23
RUN apk add --no-cache ca-certificates tzdata && addgroup -g 10001 opsdeck && adduser -D -u 10001 -G opsdeck opsdeck
WORKDIR /app
COPY --from=backend /opsdeck /app/opsdeck
COPY --from=frontend /src/web/dist /app/web/dist
USER 10001:10001
ENV LISTEN_ADDR=0.0.0.0:8080 WEB_DIR=/app/web/dist MASTER_KEY_FILE=/run/opsdeck-secrets/master.key
EXPOSE 8080
ENTRYPOINT ["/app/opsdeck"]
CMD ["all"]
