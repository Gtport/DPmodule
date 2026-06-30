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

RUN apk add --no-cache ca-certificates tzdata && \
    mkdir -p /var/log/iqport

WORKDIR /app

COPY --from=builder /app/bin/server .

EXPOSE 8080 9090

ENTRYPOINT ["./server"]
CMD ["-config", "/etc/app/config.yaml"]
