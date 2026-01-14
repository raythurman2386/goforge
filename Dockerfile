# Build stage
FROM golang:1.25.5 AS builder

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

# Final stage
FROM alpine:latest

# Install runtime dependencies
RUN apk add --no-cache ca-certificates tzdata

# Set working directory
WORKDIR /app

# Copy binary from builder
COPY --from=builder /app/bin/gocool .
# Copy static files (needed for serving css/js/images)
COPY --from=builder /app/static ./static
# Copy templates (needed for service catalog templates)
COPY --from=builder /app/templates ./templates

# Expose port
EXPOSE 8080

# Environment variables defaults
ENV GOCOOL_HOST=0.0.0.0
ENV GOCOOL_PORT=8080
ENV TEMPLATES_DIR=./templates

# Run the binary
CMD ["./gocool", "serve"]
