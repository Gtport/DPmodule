# ---- build stage ----
FROM golang:1.26-alpine AS builder

WORKDIR /app

# Download deps first — cached as long as go.mod/go.sum don't change.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN go build -o bin/server ./cmd/server/...

# ---- runtime stage ----
FROM alpine:3.20

# /var/log/iqport — каталог под лог-файл (log.file в конфиге). Пишется он
# ПАРАЛЛЕЛЬНО со stdout, ротацию (100 МБ × 5, 30 дней, сжатие) делает само
# приложение — logrotate снаружи не нужен.
RUN apk add --no-cache ca-certificates tzdata && \
    mkdir -p /var/log/iqport

WORKDIR /app

COPY --from=builder /app/bin/server .
COPY config.yaml /etc/app/config.yaml

# Порты должны совпадать с http.port / metrics.port в конфиге стенда.
ENV APP_PORT=8580
EXPOSE 8580 9580

# Healthcheck бьёт в /ready — он, в отличие от /health, пингует базу и отдаёт
# 503, когда её нет. Значит «unhealthy» означает «трафик принимать рано», а не
# просто «процесс жив». wget есть в alpine (busybox), curl ставить не нужно.
HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 \
    CMD wget -qO- "http://127.0.0.1:${APP_PORT}/ready" >/dev/null 2>&1 || exit 1

ENTRYPOINT ["./server"]
CMD ["-config", "/etc/app/config.yaml"]
