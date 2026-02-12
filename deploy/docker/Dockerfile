FROM golang:1.23-alpine AS builder
WORKDIR /workspace

RUN apk add --no-cache ca-certificates tzdata

COPY go.work ./
COPY cmd/hello-app/go.mod ./cmd/hello-app/go.mod
COPY pkg/hello-lib/go.mod ./pkg/hello-lib/go.mod

RUN go work sync

COPY . .

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -trimpath -ldflags "-s -w" -o /out/hello-app ./cmd/hello-app

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /
COPY --from=builder /out/hello-app /hello-app
USER nonroot:nonroot
ENTRYPOINT ["/hello-app"]
