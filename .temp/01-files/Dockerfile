FROM golang:1.24-alpine AS builder

WORKDIR /app

# Install necessary build tools
RUN apk add --no-cache git

# Copy go mod and sum files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy the source code
COPY . .

# Build argument to specify which service to build
ARG SERVICE=server

# Build the specified service
RUN CGO_ENABLED=0 GOOS=linux go build -o /app/bin/service ./cmd/${SERVICE}/main.go

# Create a minimal image for running the service
FROM alpine:latest

WORKDIR /app

# Install ca-certificates for HTTPS requests
RUN apk --no-cache add ca-certificates

# Copy the binary from the builder stage
COPY --from=builder /app/bin/service /app/service

# Copy SQL init scripts if needed
COPY sql /app/sql

COPY .env /app/.env
COPY config.yml /app/config.yml

# Run the service
CMD ["/app/service"]
