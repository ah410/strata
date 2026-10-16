# ─── stage 1: build the UI ───────────────────────────────────────────────────
FROM node:22-alpine AS ui

WORKDIR /ui
# Copy manifests first so the dependency layer caches across source edits.
COPY frontend/package.json frontend/package-lock.json* ./
RUN npm ci --no-audit --no-fund 2>/dev/null || npm install --no-audit --no-fund

COPY frontend/ ./
# Vite writes to ../backend/dist, so give it somewhere to land.
RUN mkdir -p /backend/dist && \
    npx vite build --outDir /backend/dist --emptyOutDir

# ─── stage 2: build the server ───────────────────────────────────────────────
FROM golang:1.24-alpine AS server

WORKDIR /src
COPY backend/go.mod backend/go.sum ./
RUN go mod download

COPY backend/ ./
# The UI is embedded into the binary, so it must be in place before `go build`.
COPY --from=ui /backend/dist ./dist

# Static build: no libc dependency, so the final image can be minimal.
RUN CGO_ENABLED=0 GOOS=linux go build \
      -trimpath -ldflags="-s -w" \
      -o /strata .

# ─── stage 3: runtime ────────────────────────────────────────────────────────
FROM alpine:3.20

# tzdata so file timestamps render in your local zone; ca-certificates is
# cheap insurance if you ever add outbound calls.
RUN apk add --no-cache tzdata ca-certificates

COPY --from=server /strata /usr/local/bin/strata

# The share is mounted here. Declared as a volume so a stray `docker run`
# without -v does not silently write into the container layer.
VOLUME ["/data"]

ENV STRATA_ROOT=/data \
    STRATA_ADDR=:8080 \
    TZ=America/Phoenix

EXPOSE 8080

# Runs as root by default so it can read files owned by any TrueNAS user.
# Override with `user:` in compose to restrict it — see README.
ENTRYPOINT ["/usr/local/bin/strata"]
