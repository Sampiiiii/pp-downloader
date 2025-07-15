# Plex Playlist Downloader (pp-downloader)

A container-native Go application that monitors YouTube playlists and downloads new audio tracks with proper metadata for Plex.

## Features

- Monitors multiple YouTube playlists for new content
- Downloads only new tracks (tracks are tracked to avoid duplicates)
- Extracts audio and embeds proper metadata (title, artist, etc.)
- Runs as a lightweight container
- Configurable via environment variables and JSON config

## Prerequisites

- Docker
- Docker Compose (optional)

## Quick Start

1. Create a `config` directory and a `playlists.json` file:

```bash
mkdir -p config
cat > config/playlists.json << 'EOF'
{
  "playlists": {
    "jazz": "https://www.youtube.com/playlist?list=YOUR_PLAYLIST_ID",
    "chill": "https://www.youtube.com/playlist?list=ANOTHER_PLAYLIST_ID"
  },
  "sleep_time": 86400
}
EOF
```

2. Create a `.env` file (or copy from example):

```bash
cp example.env .env
# Edit the .env file if needed
```

3. Build and run with Docker Compose:

```bash
docker-compose up -d --build
```

## Configuration

### Environment Variables

- `MUSIC_PARENT_DIR`: Directory where music will be saved (default: `/music` in container)
- `FFMPEG_PATH`: Path to ffmpeg binary (default: `/usr/bin/ffmpeg`)
- `JSON_PATH`: Path to playlists.json (default: `/config/playlists.json`)

### Playlist Configuration

The `playlists.json` file has the following structure:

```json
{
  "playlists": {
    "playlist_name": "youtube_playlist_url_or_id",
    ...
  },
  "sleep_time": 86400
}
```

- `playlist_name`: A friendly name for the playlist (used for logging)
- `youtube_playlist_url_or_id`: Full YouTube playlist URL or just the playlist ID
- `sleep_time`: Time in seconds between checks for new content (default: 86400 = 24 hours)

## Building from Source

1. Clone the repository:

```bash
git clone https://github.com/yourusername/pp-downloader.git
cd pp-downloader
```

2. Build the binary:

```bash
go build -o pp-downloader ./cmd/pp-downloader
```

3. Run the application:

```bash
MUSIC_PARENT_DIR=./music \
FFMPEG_PATH=/path/to/ffmpeg \
JSON_PATH=./config/playlists.json \
./pp-downloader
```

## Docker Compose

A sample `docker-compose.yml` is provided for easy deployment:

```yaml
version: '3.8'

services:
  pp-downloader:
    build: .
    volumes:
      - ./config:/config
      - ./music:/music
    environment:
      - MUSIC_PARENT_DIR=/music
      - FFMPEG_PATH=/usr/bin/ffmpeg
      - JSON_PATH=/config/playlists.json
    restart: unless-stopped
```

## License

MIT
