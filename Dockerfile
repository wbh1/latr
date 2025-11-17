# ABOUTME: Dockerfile for latr container images - used by GoReleaser for releases
# ABOUTME: GoReleaser provides pre-built binary; for manual builds see README.md

# Use minimal distroless image for security (no shell, minimal attack surface)
FROM gcr.io/distroless/static:nonroot

# Copy the pre-built binary from GoReleaser build context
# GoReleaser automatically places the built binary here
COPY latr /usr/local/bin/latr

# Run as non-root user (uid=65532) for security
USER nonroot:nonroot

# Set the binary as entrypoint
ENTRYPOINT ["/usr/local/bin/latr"]
CMD ["--help"]
