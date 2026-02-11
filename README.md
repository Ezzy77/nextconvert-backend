# NextConvert Backend

A powerful media conversion API built with Go, wrapping FFmpeg for video, audio, and image processing.

## Features

- **Video Processing**: Convert, compress, trim, resize, rotate, watermark
- **Audio Processing**: Extract, convert, adjust bitrate, normalize
- **Image Processing**: Resize, convert, generate thumbnails, create GIFs
- **Job Queue**: Background processing with priority queues and progress tracking
- **Real-time Updates**: WebSocket support for job progress notifications
- **Chunked Uploads**: Support for large file uploads with resumable chunks
- **Presets**: Pre-configured operation chains for common use cases

## Prerequisites

- Go 1.21+
- Docker & Docker Compose
- FFmpeg 6.x (included in Docker image)

## Quick Start

### Using Docker (Recommended)

```bash
# Start all services
docker compose up -d

# View logs
docker compose logs -f

# Stop services
docker compose down
```

The API will be available at `http://localhost:8080`

### Using Supabase as Database

See [COMMANDS.md](../COMMANDS.md#supabase-setup) for full instructions. Summary:

1. Create a Supabase project and get the direct connection string
2. Run migrations via SQL Editor or `./scripts/migrate-supabase.sh`
3. Set `DATABASE_URL` in `.env` (include `?sslmode=require`)
4. Start with: `docker compose -f docker-compose.supabase.yml up -d`

### Local Development

1. **Install dependencies**

   ```bash
   # Install Go dependencies
   go mod download

   # Install FFmpeg (macOS)
   brew install ffmpeg
   ```

2. **Start infrastructure**

   ```bash
   # Start PostgreSQL and Redis only
   docker compose up -d postgres redis
   ```

3. **Configure environment**

   ```bash
   cp .env.example .env
   # Edit .env as needed
   # For Supabase: set DATABASE_URL to your Supabase direct connection string (with sslmode=require)
   ```

4. **Run the server**

   ```bash
   make run-server
   ```

5. **Run the worker (in another terminal)**
   ```bash
   make run-worker
   ```

## API Endpoints

### Health

- `GET /api/v1/health` - Basic health check
- `GET /api/v1/ready` - Readiness check with dependencies

### Files

- `POST /api/v1/files/upload` - Initiate file upload
- `POST /api/v1/files/upload/chunk` - Upload file chunk
- `POST /api/v1/files/upload/complete` - Complete chunked upload
- `GET /api/v1/files/:id` - Get file metadata
- `GET /api/v1/files/:id/download` - Download file
- `GET /api/v1/files/:id/thumbnail` - Get thumbnail
- `DELETE /api/v1/files/:id` - Delete file

### Media (FFmpeg)

- `POST /api/v1/media/probe` - Extract media metadata
- `GET /api/v1/media/presets` - List available presets
- `GET /api/v1/media/presets/:id` - Get preset details
- `POST /api/v1/media/validate` - Validate operations
- `GET /api/v1/media/formats` - List supported formats
- `GET /api/v1/media/codecs` - List available codecs

### Jobs

- `POST /api/v1/jobs` - Create new job
- `GET /api/v1/jobs` - List user's jobs
- `GET /api/v1/jobs/:id` - Get job details
- `DELETE /api/v1/jobs/:id` - Cancel job
- `POST /api/v1/jobs/:id/retry` - Retry failed job
- `GET /api/v1/jobs/:id/logs` - Get job logs

### WebSocket

- `GET /api/v1/ws` - WebSocket connection for real-time updates

## Supported Operations

### Video

- `trim` - Cut video segments (start/end time)
- `resize` - Change resolution (width, height, maintain aspect)
- `compress` - Reduce file size (quality 1-100 or target size)
- `rotate` - Rotate video (90, 180, 270 degrees)
- `crop` - Crop video region
- `convertFormat` - Change container/codec (MP4, WebM, MOV, AVI)
- `addWatermark` - Add image/text watermark
- `changeSpeed` - Speed up/slow down
- `createGif` - Convert to animated GIF
- `extractAudio` - Extract audio track

### Audio

- `convertFormat` - Change format (MP3, WAV, AAC, FLAC, OGG)
- `changeBitrate` - Adjust audio bitrate
- `adjustVolume` - Change volume level
- `fadeInOut` - Add fade effects
- `trim` - Cut audio segments
- `merge` - Combine multiple audio files

### Image

- `resize` - Change dimensions
- `convertFormat` - Change format (PNG, JPG, WebP, GIF)
- `compress` - Reduce file size
- `rotate` - Rotate image

## Project Structure

```
backend/
├── cmd/
│   ├── server/         # API server entry point
│   └── worker/         # Worker entry point
├── internal/
│   ├── api/
│   │   ├── handlers/   # HTTP handlers
│   │   ├── middleware/ # HTTP middleware
│   │   └── websocket/  # WebSocket hub
│   ├── modules/
│   │   ├── media/      # FFmpeg wrapper
│   │   └── jobs/       # Job queue management
│   └── shared/
│       ├── config/     # Configuration
│       ├── database/   # Database connections
│       ├── logging/    # Logging utilities
│       └── storage/    # File storage
├── migrations/         # Database migrations
├── docker-compose.yml
├── Dockerfile
└── Makefile
```

## Configuration

| Variable             | Description                                                                              | Default          |
| -------------------- | ---------------------------------------------------------------------------------------- | ---------------- |
| `PORT`               | Server port                                                                              | `8080`           |
| `ENVIRONMENT`        | Environment name                                                                         | `development`    |
| `DATABASE_URL`       | PostgreSQL connection string. For Supabase use direct connection with `?sslmode=require` | Local default    |
| `REDIS_URL`          | Redis address                                                                            | `localhost:6379` |
| `STORAGE_BACKEND`    | Storage backend (local/s3)                                                               | `local`          |
| `FFMPEG_PATH`        | Path to FFmpeg binary                                                                    | `ffmpeg`         |
| `FFPROBE_PATH`       | Path to FFprobe binary                                                                   | `ffprobe`        |
| `WORKER_CONCURRENCY` | Worker concurrency                                                                       | `2`              |
| `MAX_UPLOAD_SIZE`    | Max upload size in bytes                                                                 | `5GB`            |

## License

MIT
