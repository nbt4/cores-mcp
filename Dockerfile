# syntax=docker/dockerfile:1
FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/cores-mcp ./cmd/server

FROM alpine:3.22
RUN apk add --no-cache ca-certificates wget && addgroup -S cores && adduser -S -G cores cores
WORKDIR /app
COPY --from=build /out/cores-mcp /usr/local/bin/cores-mcp
COPY knowledge /app/knowledge
RUN mkdir -p /var/lib/cores-mcp/oauth && chown -R cores:cores /var/lib/cores-mcp
USER cores
EXPOSE 8090
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 CMD wget -q -O /dev/null http://127.0.0.1:8090/health || exit 1
ENTRYPOINT ["/usr/local/bin/cores-mcp"]
