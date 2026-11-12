# The KudOS image: the Go server with the web build embedded.
# Both build stages run on the build machine's platform and cross-compile, so
# arm64 images build without emulation.

FROM --platform=$BUILDPLATFORM node:22.23.4-bookworm-slim AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY api/openapi.yaml /src/api/openapi.yaml
COPY web/ ./
RUN npm run build

FROM --platform=$BUILDPLATFORM golang:1.26.11-bookworm AS server
ARG TARGETOS TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/web/dist web/dist
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w" -o /out/kudos ./cmd/server \
    && mkdir -p /out/data

# Static binary on distroless: CA certificates, no shell, runs as nonroot (65532).
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=server /out/kudos /usr/local/bin/kudos
# A fresh volume mounted here takes this owner, so the server can write originals.
COPY --from=server --chown=65532:65532 /out/data /data
VOLUME /data
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/kudos"]
