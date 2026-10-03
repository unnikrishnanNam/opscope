# Opscope: one image that serves both the API and the web UI.
#
# Built in three stages. Only the last one ends up in the final image;
# the first two are thrown away after their output is copied out.
#
# The image can be built for several platforms at once (CI builds amd64 and
# arm64; see .github/workflows/release.yml). The first two stages run on the
# build machine's own platform ($BUILDPLATFORM), so nothing is emulated:
# the React build is the same files everywhere, and Go cross-compiles for
# the platform being built ($TARGETOS/$TARGETARCH). A plain `docker build`
# sets all of these by itself.

# ---- Stage 1: build the React app into static files ----
FROM --platform=$BUILDPLATFORM node:24-alpine AS frontend
WORKDIR /src
# Copy package files first so `npm ci` is cached until dependencies change.
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci
COPY frontend/ ./
RUN npm run build

# ---- Stage 2: build the Go server into a single binary ----
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS backend
WORKDIR /src
# Copy only the dependency list first, so the download is cached until it changes.
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
ARG VERSION=dev
ARG TARGETOS
ARG TARGETARCH
# CGO_ENABLED=0 gives a static binary that runs on a minimal base image.
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -ldflags "-s -w -X main.version=${VERSION}" -o /opscope .
# An empty folder for saved clusters. The runtime image has no shell to run
# mkdir, so we create it here and copy it over with the right owner.
RUN mkdir /empty-data

# ---- Stage 3: the small runtime image ----
# distroless/static has no shell or package manager, only what a static binary needs.
FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=backend /opscope /app/opscope
COPY --from=frontend /src/dist /app/web
# Owned by the nonroot user (uid 65532) so Opscope can write saved clusters.
# Mount a volume here to keep them across restarts.
COPY --from=backend --chown=65532:65532 /empty-data /data
ENV PORT=8080 \
    STATIC_DIR=/app/web \
    DATA_DIR=/data
EXPOSE 8080
VOLUME /data
USER nonroot
ENTRYPOINT ["/app/opscope"]
