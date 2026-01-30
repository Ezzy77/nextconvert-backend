# Build stage
FROM golang:1.21-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git ca-certificates tzdata

WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the server binary
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /bin/server ./cmd/server

# Build the worker binary
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /bin/worker ./cmd/worker

# Server runtime stage
FROM alpine:3.19 AS server

# Install runtime dependencies - FFmpeg only
RUN apk add --no-cache \
    ca-certificates \
    tzdata \
    ffmpeg

# Create non-root user
RUN adduser -D -g '' appuser

WORKDIR /app

# Copy binary from builder
COPY --from=builder /bin/server /app/server

# Create data directories
RUN mkdir -p /app/data/upload /app/data/working /app/data/output && \
    chown -R appuser:appuser /app

USER appuser

EXPOSE 8080

CMD ["/app/server"]

# Worker runtime stage
FROM alpine:3.19 AS worker

# Install runtime dependencies - FFmpeg only
RUN apk add --no-cache \
    ca-certificates \
    tzdata \
    ffmpeg

# Create non-root user
RUN adduser -D -g '' appuser

WORKDIR /app

# Copy binary from builder
COPY --from=builder /bin/worker /app/worker

# Create data directories
RUN mkdir -p /app/data/upload /app/data/working /app/data/output && \
    chown -R appuser:appuser /app

USER appuser

CMD ["/app/worker"]
