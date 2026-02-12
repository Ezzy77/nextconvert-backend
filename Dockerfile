# Build stage
FROM --platform=linux/amd64 golang:1.24-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git ca-certificates tzdata

WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the server binary
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-w -s" -o /bin/server ./cmd/server

# Build the worker binary
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-w -s" -o /bin/worker ./cmd/worker

# Server runtime stage
FROM --platform=linux/amd64 alpine:3.19 AS server

# Install runtime dependencies - FFmpeg only
RUN apk add --no-cache \
    ca-certificates \
    tzdata \
    ffmpeg

WORKDIR /app

# Copy binary from builder
COPY --from=builder /bin/server /app/server

# Copy entrypoint script
COPY entrypoint.sh /app/entrypoint.sh
RUN chmod +x /app/entrypoint.sh

# Note: Entrypoint handles directory creation with proper permissions

EXPOSE 8080

ENTRYPOINT ["/app/entrypoint.sh"]
CMD ["/app/server"]

# Worker runtime stage
FROM --platform=linux/amd64 alpine:3.19 AS worker

# Install runtime dependencies - FFmpeg and fonts for watermarks
RUN apk add --no-cache \
    ca-certificates \
    tzdata \
    ffmpeg \
    ttf-dejavu \
    fontconfig

WORKDIR /app

# Copy binary from builder
COPY --from=builder /bin/worker /app/worker

# Copy entrypoint script
COPY entrypoint.sh /app/entrypoint.sh
RUN chmod +x /app/entrypoint.sh

# Note: Entrypoint handles directory creation with proper permissions

ENTRYPOINT ["/app/entrypoint.sh"]
CMD ["/app/worker"]
