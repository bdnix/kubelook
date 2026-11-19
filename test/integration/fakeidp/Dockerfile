# Identity stand-in for the integration environment. Build context: the repository root.
FROM golang:1.27.4-trixie AS build
WORKDIR /src
COPY go.mod go.sum ./
COPY test/integration/fakeidp/ ./test/integration/fakeidp/
RUN CGO_ENABLED=0 go build -trimpath -o /out/fakeidp ./test/integration/fakeidp

FROM gcr.io/distroless/static-debian13:nonroot
COPY --from=build /out/fakeidp /fakeidp
EXPOSE 8300
ENTRYPOINT ["/fakeidp"]
