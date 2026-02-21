FROM golang:1.25-alpine AS builder
WORKDIR /src

RUN apk add --no-cache ca-certificates

COPY . .

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -trimpath -ldflags '-s -w' -o /out/migrate ./cmd/migrate

FROM alpine:3.20
RUN apk add --no-cache ca-certificates \
    && addgroup -S app && adduser -S -G app app
WORKDIR /app
COPY --from=builder /out/migrate /usr/local/bin/migrate
COPY --from=builder /src/sql/timescale/migrations /app/sql/timescale/migrations
COPY --from=builder /src/sql/clickhouse/migrations /app/sql/clickhouse/migrations
USER app:app
ENTRYPOINT ["/usr/local/bin/migrate"]
