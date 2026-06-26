FROM golang:1.25-alpine AS builder
WORKDIR /src

RUN apk add --no-cache ca-certificates

COPY . .

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -trimpath -ldflags '-s -w' -o /out/validator ./cmd/validator

FROM alpine:3.20
RUN apk add --no-cache ca-certificates wget \
    && addgroup -S app && adduser -S -G app app
WORKDIR /
COPY --from=builder /out/validator /usr/local/bin/validator
USER app:app
ENTRYPOINT ["/usr/local/bin/validator"]
