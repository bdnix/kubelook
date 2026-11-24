# syntax=docker/dockerfile:1
FROM --platform=$BUILDPLATFORM node:26.13.1-trixie@sha256:e18c3ccdf7d71619990b0e85639f506e2028a95410999cab24cc373cbd10e0ac AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN --mount=type=cache,target=/root/.npm npm ci
COPY web/ ./
RUN npm run build

FROM --platform=$BUILDPLATFORM golang:1.27.4-trixie@sha256:f18c3d73a184fb0582828510722e723b63a93f7188d11dd223d9be6f6880e65d AS build
ARG TARGETOS
ARG TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY cmd/ cmd/
COPY internal/ internal/
COPY --from=web /src/web/dist/ internal/webui/dist/
# Declared here, not at the top of the stage: they change with every commit, and every RUN after a changed ARG misses
# the layer cache.
ARG VERSION=dev
ARG COMMIT=unknown
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath \
    -ldflags "-s -w -X github.com/bdnix/kubelook/internal/version.Version=${VERSION} -X github.com/bdnix/kubelook/internal/version.Commit=${COMMIT}" \
    -o /out/kubelook ./cmd/kubelook

FROM gcr.io/distroless/static-debian13:nonroot@sha256:aafe993665bf6200db38a02c11a9351e782a84b6a45e7f33b6695638c88e3931
COPY --from=build /out/kubelook /kubelook
USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["/kubelook"]
CMD ["serve"]
