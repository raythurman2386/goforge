# Build stage
FROM golang:1.26.10 AS builder

# Install build dependencies
RUN apt-get update && apt-get install -y git make curl

# Install TailwindCSS
# Using the same method as in the Makefile
RUN curl -sLO https://github.com/tailwindlabs/tailwindcss/releases/download/v3.4.17/tailwindcss-linux-x64 && \
    chmod +x tailwindcss-linux-x64 && \
    mv tailwindcss-linux-x64 /usr/local/bin/tailwindcss

# Set working directory
WORKDIR /app

# Copy go mod and sum files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Install tools (templ) from tools.go
# This ensures we use the version specified in go.mod
RUN go install github.com/a-h/templ/cmd/templ

# Copy source code
COPY . .

# Build the application
# We run `make build` which handles `templ generate` and `css` generation
RUN make build

# MCP server build (target: mcp). Defined before the final stage so a plain
# `docker build .` still produces the GoForge app image.
FROM builder AS mcp-builder
RUN CGO_ENABLED=0 go build -ldflags "-s -w" -o /app/bin/goforge-mcp ./cmd/goforge-mcp

# MCP server image: `docker build --target mcp -t goforge-mcp .`
# Speaks MCP over stdin/stdout and calls GoForge over HTTP. It needs no
# Docker socket, no database, and no published port.
FROM alpine:latest AS mcp
RUN apk add --no-cache ca-certificates && adduser -D -H -u 10001 mcp
COPY --from=mcp-builder /app/bin/goforge-mcp /usr/local/bin/goforge-mcp
USER mcp
ENV GOFORGE_MCP_URL=http://goforge:8080
ENTRYPOINT ["/usr/local/bin/goforge-mcp"]

# Final stage
FROM alpine:latest

# Install runtime dependencies. postgresql17-client provides pg_dump for the
# nightly app DB backup (it reads servers up to PostgreSQL 17).
RUN apk add --no-cache ca-certificates tzdata postgresql17-client

# Set working directory
WORKDIR /app

# Copy binary from builder
COPY --from=builder /app/bin/goforge .
# Copy static files (needed for serving css/js/images)
COPY --from=builder /app/static ./static
# Copy templates (needed for service catalog templates)
COPY --from=builder /app/templates ./templates

# Expose port
EXPOSE 8080

# Environment variables defaults
ENV GOFORGE_HOST=0.0.0.0
ENV GOFORGE_PORT=8080
ENV TEMPLATES_DIR=./templates

# Run the binary
CMD ["./goforge", "serve"]
