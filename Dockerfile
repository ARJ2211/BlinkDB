# ---- build stage ----
FROM golang:1.22-alpine AS builder
WORKDIR /src
RUN apk add --no-cache ca-certificates tzdata git

# cache go modules first
COPY go.mod go.sum ./
RUN go mod download

# copy the rest
COPY . .

# build static binary
ARG VERSION=dev
ARG COMMIT=none
ARG DATE=unknown
ENV CGO_ENABLED=0 GOOS=linux GOARCH=amd64
RUN go build -trimpath -ldflags "-s -w \
    -X main.version=${VERSION} \
    -X main.commit=${COMMIT} \
    -X main.date=${DATE}" \
    -o /out/blinkdb ./cmd/server

# ---- runtime stage ----
# choose alpine for a tiny shell + easy env flag wiring; switch to distroless further below
FROM alpine:3.20
WORKDIR /app

# non-root user
RUN adduser -D -H -u 10001 appuser
COPY --from=builder /out/blinkdb /app/blinkdb
COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

# default env (override at runtime)
ENV PORT=8080
ENV SWEEP_ENABLED=true
ENV SWEEP_INTERVAL=30s
# future-proof: path for snapshots/WAL later
ENV DATA_DIR=/data

# data dir for future persistence; harmless today
RUN mkdir -p ${DATA_DIR} && chown -R appuser:appuser ${DATA_DIR}
USER appuser

EXPOSE 8080

# small shim to map env -> flags
# alpine has /bin/sh so we can expand env vars
ENTRYPOINT ["/bin/sh","-c","/app/blinkdb --port ${PORT} --sweep-enabled ${SWEEP_ENABLED} --sweep-interval ${SWEEP_INTERVAL}"]

# Optional healthcheck (uncomment once you’re happy with the endpoint)
# HEALTHCHECK --interval=30s --timeout=3s --retries=3 CMD wget -qO- http://127.0.0.1:${PORT}/v1/kv || exit 1
