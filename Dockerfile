# Base images are pinned by digest so a rebuild can't silently pull different
# code. Bump tag and digest together, deliberately.

# ─── stage 1: build the UI ───────────────────────────────────────────────────
FROM node:24-alpine@sha256:44fce696f38ba8c987c4db89954051000bbd05ffd8d8659ba15ed4a8602e937f AS ui

WORKDIR /ui
# Copy manifests first so the dependency layer caches across source edits.
COPY frontend/package.json frontend/package-lock.json ./
# Lockfile only, and no install scripts: a dependency can't run code here.
RUN npm ci --no-audit --no-fund --ignore-scripts

COPY frontend/ ./
# Vite writes to ../backend/dist, so give it somewhere to land.
RUN mkdir -p /backend/dist && \
    npx --no-install vite build --outDir /backend/dist --emptyOutDir

# ─── stage 2: build the server ───────────────────────────────────────────────
FROM golang:1.27-alpine@sha256:f92b6ef800e499660581efdabdf25d9d817a9d124eaf900924f0504e7e27e12d AS server

ENV GOTOOLCHAIN=local
WORKDIR /src
COPY backend/go.mod backend/go.sum ./
RUN go mod download && go mod verify

COPY backend/ ./
# The UI is embedded into the binary, so it must be in place before `go build`.
COPY --from=ui /backend/dist ./dist

# Static build: no libc dependency, so the runtime image can be distroless.
RUN CGO_ENABLED=0 GOOS=linux go build \
      -trimpath -buildvcs=false -ldflags="-s -w" \
      -o /strata .

# ─── stage 3: runtime ────────────────────────────────────────────────────────
# Distroless static: no shell, no package manager, just CA certs, tzdata and
# a nonroot user. Nothing for an attacker to pivot with.
FROM gcr.io/distroless/static-debian13:nonroot@sha256:e2e927ec666bae08560abb3c55d0659eceabb657f56b6782ab500a9fc7f555e3

COPY --from=server /strata /usr/local/bin/strata

# The share is mounted here. Declared as a volume so a stray `docker run`
# without -v does not silently write into the container layer.
VOLUME ["/data"]

ENV STRATA_ROOT=/data \
    STRATA_ADDR=:8080 \
    TZ=America/Phoenix

EXPOSE 8080

# Non-root by default (distroless "nonroot", uid 65532). Set `user:` in
# compose to a uid that has an ACL entry on your dataset; see README.
USER 65532:65532

HEALTHCHECK --interval=30s --timeout=4s --start-period=5s --retries=3 \
  CMD ["/usr/local/bin/strata", "healthcheck"]

ENTRYPOINT ["/usr/local/bin/strata"]
