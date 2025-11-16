# Build stage
FROM golang:1.24-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git ca-certificates tzdata

WORKDIR /build

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the binary
# Note: goreleaser will pass the binary, so this is mainly for manual builds
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o latr ./cmd/latr

# Runtime stage
FROM gcr.io/distroless/static:nonroot

# Copy CA certificates and timezone data
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo

# Copy the binary from builder
COPY --from=builder /build/latr /usr/local/bin/latr

# Use non-root user
USER nonroot:nonroot

# Default command
ENTRYPOINT ["/usr/local/bin/latr"]
CMD ["--help"]
