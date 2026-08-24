FROM golang:1.27.0-bookworm@sha256:484ef6066fa69acb059fdfeda7ba2b8f7391f2ef6abc6f9b8411e669ebd56466 AS build
ARG VERSION=0.1.0
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X github.com/zelentsov-dev/apple-ads-mcp/internal/mcpserver.Version=${VERSION}" -o /out/apple-ads-mcp ./cmd/apple-ads-mcp

FROM scratch
LABEL org.opencontainers.image.source="https://github.com/zelentsov-dev/apple-ads-mcp"
LABEL io.modelcontextprotocol.server.name="io.github.zelentsov-dev/apple-ads-mcp"
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /out/apple-ads-mcp /apple-ads-mcp
USER 65532:65532
ENTRYPOINT ["/apple-ads-mcp"]
CMD ["serve", "--stdio"]
