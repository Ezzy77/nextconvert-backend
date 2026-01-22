# Convert Studio Backend

A powerful media and document conversion API built with Go, wrapping FFmpeg and Pandoc CLI tools.

## Features

- **Media Processing**: Video/audio conversion, compression, trimming, watermarking, GIF creation
- **Document Conversion**: Markdown, DOCX, HTML, PDF, EPUB, LaTeX conversions via Pandoc
- **Job Queue**: Background processing with priority queues and progress tracking
- **Real-time Updates**: WebSocket support for job progress notifications
- **Chunked Uploads**: Support for large file uploads with resumable chunks
- **Presets**: Pre-configured operation chains for common use cases

## Prerequisites

- Go 1.21+
- Docker & Docker Compose
- FFmpeg 6.x
- Pandoc 3.x

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

### Local Development

1. **Install dependencies**
   ```bash
   # Install Go dependencies
   go mod download

   # Install FFmpeg (macOS)
   brew install ffmpeg

   # Install Pandoc (macOS)
   brew install pandoc
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
- `DELETE /api/v1/files/:id` - Delete file

### Media
- `POST /api/v1/media/probe` - Extract media metadata
- `GET /api/v1/media/presets` - List available presets
- `GET /api/v1/media/formats` - List supported formats
- `GET /api/v1/media/codecs` - List available codecs
- `POST /api/v1/media/validate` - Validate operations

### Documents
- `GET /api/v1/documents/formats` - Get format conversion matrix
- `GET /api/v1/documents/templates` - List templates
- `GET /api/v1/documents/citation-styles` - List citation styles
- `POST /api/v1/documents/validate` - Validate conversion

### Jobs
- `POST /api/v1/jobs` - Create new job
- `GET /api/v1/jobs` - List user's jobs
- `GET /api/v1/jobs/:id` - Get job details
- `DELETE /api/v1/jobs/:id` - Cancel job
- `POST /api/v1/jobs/:id/retry` - Retry failed job

### WebSocket
- `GET /api/v1/ws` - WebSocket connection for real-time updates

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
│   │   ├── document/   # Pandoc wrapper
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

Environment variables:

| Variable | Description | Default |
|----------|-------------|---------|
| `PORT` | Server port | `8080` |
| `ENVIRONMENT` | Environment name | `development` |
| `DATABASE_URL` | PostgreSQL connection string | - |
| `REDIS_URL` | Redis address | `localhost:6379` |
| `STORAGE_BACKEND` | Storage backend (local/s3) | `local` |
| `FFMPEG_PATH` | Path to FFmpeg binary | `ffmpeg` |
| `PANDOC_PATH` | Path to Pandoc binary | `pandoc` |
| `WORKER_CONCURRENCY` | Worker concurrency | `2` |

## License

MIT
