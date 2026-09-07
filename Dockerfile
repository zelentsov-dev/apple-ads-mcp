FROM golang:1.27.1-bookworm@sha256:648f440f42a0958804efb24df176f806f9d353b41f1c0627f666428e40310f6b AS build
ARG VERSION=0.3.6
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
