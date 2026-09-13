# syntax=docker/dockerfile:1

# ---- web UI ------------------------------------------------------------------
FROM --platform=$BUILDPLATFORM node:24-alpine AS web
WORKDIR /web
COPY web/package.json web/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY web/ ./
RUN npm run build

# ---- Go binary -----------------------------------------------------------------
FROM --platform=$BUILDPLATFORM golang:1.27-alpine AS build
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
ARG COMMIT=unknown
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /web/dist ./web/dist
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath \
    -ldflags "-s -w -X github.com/Asion001/mangarr/internal/version.Version=${VERSION} -X github.com/Asion001/mangarr/internal/version.Commit=${COMMIT}" \
    -o /out/mangarr ./cmd/mangarr

# ---- runtime ----------------------------------------------------------------------
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/mangarr /mangarr
ENV MANGARR_DATA_DIR=/config \
    MANGARR_LISTEN=:8787
EXPOSE 8787
VOLUME ["/config"]
HEALTHCHECK --interval=30s --timeout=5s --start-period=20s CMD ["/mangarr", "healthcheck"]
ENTRYPOINT ["/mangarr"]
