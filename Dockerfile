# ---------------------------
# 1) Build stage
# ---------------------------
FROM golang:1.25-alpine AS builder

# Work in a clean directory
WORKDIR /src

# Small base tools (certs for HTTPS downloads, tzdata optional, git for modules)
RUN apk add --no-cache ca-certificates tzdata git

# Copy only go.mod first to leverage Docker layer caching for deps
# NOTE: We intentionally do NOT copy go.sum here (it may not exist yet).
COPY go.mod ./

# Pre-download modules based on go.mod (fast rebuilds when code changes but deps don't)
RUN go mod download

# Now bring in the rest of the source
COPY . .

# If go.sum doesn't exist yet (e.g., first build), create it so builds are reproducible
# This is a no-op if go.sum already exists.
RUN [ -f go.sum ] || go mod tidy

# Produce a static binary (no libc) for a tiny runtime image
ENV CGO_ENABLED=0 GOOS=linux GOARCH=amd64
RUN go build -trimpath -o /out/blinkdb ./cmd/server

# ---------------------------
# 2) Runtime stage
# ---------------------------
FROM alpine:3.20

WORKDIR /app

# Non-root user for safer defaults
RUN adduser -D -H -u 10001 appuser

# Copy the compiled static binary
COPY --from=builder /out/blinkdb /app/blinkdb

USER appuser
EXPOSE 8080

# Run the server (configure flags via env/args if needed)
ENTRYPOINT ["/app/blinkdb"]
