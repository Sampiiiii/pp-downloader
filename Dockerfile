# Build stage
FROM golang:1.24-alpine AS builder

# Install build dependencies needed for CGO (gcc, musl-dev) and SQLite headers
RUN apk add --no-cache build-base sqlite-dev

WORKDIR /app

# Copy go mod files first to leverage Docker cache
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy the rest of the source code
COPY . .

# Build the application (CGO is enabled for sqlite3 support)
RUN CGO_ENABLED=1 \
    CGO_CFLAGS="-D_GNU_SOURCE -DSQLITE_DISABLE_LFS" \
    go build -tags "sqlite_omit_load_extension,libsqlite3" -ldflags="-w -s" \
    -o /app/pp-downloader ./cmd/pp-downloader

# Final stage
FROM alpine:3.19

# Set environment variables
ENV CONFIG_PATH=/app/config/playlists.json \
    DB_PATH=/app/data/downloads.db \
    DOWNLOAD_DIR=/app/downloads \
    MUSIC_PARENT_DIR=/music \
    FFMPEG_PATH=/usr/bin/ffmpeg \
    JSON_PATH=/config/playlists.json \
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
    mkdir -p /music /config && \
    chown -R appuser:appuser /app /music /config

# Switch to non-root user
USER appuser

# Copy the binary from the builder
COPY --from=builder /app/pp-downloader .

# Set volume mounts
VOLUME ["/music", "/config"]

# Set up healthcheck
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD ["pgrep", "pp-downloader"] || exit 1

# Set the entrypoint and default command
ENTRYPOINT ["/app/pp-downloader"]
