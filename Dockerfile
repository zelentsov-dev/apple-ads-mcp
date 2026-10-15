FROM golang:1.26.9-bookworm@sha256:d9c68c2c51161e12fd77e4c6320687c9cd86e1af1e3ad6e6cd63ff970641453c AS build
ARG VERSION=0.3.7
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
