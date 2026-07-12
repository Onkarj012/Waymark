# ---- build ----
FROM golang:1.25-alpine AS build
WORKDIR /src

# Cache modules first.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
# CGO disabled: modernc.org/sqlite is pure Go, so this builds a static binary.
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server

# ---- runtime ----
FROM alpine:3.20
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=build /out/server /app/server

# SQLite database lives on a mounted volume (Railway mounts it at /data, owned
# by root — so the server runs as root to be able to write there).
ENV PORT=8080 \
    DB_PATH=/data/waymark.db

EXPOSE 8080
CMD ["/app/server"]
