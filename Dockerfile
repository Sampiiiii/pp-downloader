# Build stage
FROM golang:1.24 AS builder

WORKDIR /app

# Copy go mod and sum files first to leverage Docker cache
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the application
RUN CGO_ENABLED=1 GOOS=linux GOARCH=amd64 \
    go build -ldflags="-w -s" \
    -o /app/pp-downloader ./cmd/pp-downloader

# Download all dependencies
RUN go mod download

# Copy source code
COPY --chown=appuser:appuser . .

# Create necessary directories
RUN mkdir -p /app/data /app/downloads /app/config

# Build the application
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -ldflags="-w -s" \
    -o /app/pp-downloader ./cmd/pp-downloader

# Final stage
FROM alpine:3.19

# Set environment variables
ENV CONFIG_PATH=/app/config/playlists.json \
    DB_PATH=/app/data/downloads.db \
    DOWNLOAD_DIR=/app/downloads \
    MUSIC_PARENT_DIR=/app/data \
    FFMPEG_PATH=/usr/bin/ffmpeg \
    JSON_PATH=/app/config/playlists.json \
    WATCH_INTERVAL=15m \
    PATH="$PATH:/usr/local/bin"

# Create app directory and set up non-root user
WORKDIR /app
RUN addgroup --system appuser && \
    adduser --system --ingroup appuser appuser && \
    # Install runtime dependencies
    apk add --no-cache \
        ca-certificates \
        tzdata \
        ffmpeg \
        yt-dlp \
        sqlite && \
    # Create necessary directories
    mkdir -p /app/data /app/downloads /app/config && \
    chown -R appuser:appuser /app

# Switch to non-root user
USER appuser

# Copy the binary from the builder
COPY --from=builder /app/pp-downloader .

# Set volume mounts
VOLUME ["/app/data", "/app/downloads", "/app/config"]

# Set up healthcheck
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD ["pgrep", "pp-downloader"] || exit 1

# Set the entrypoint and default command
ENTRYPOINT ["/app/pp-downloader"]
CMD ["--help"]
