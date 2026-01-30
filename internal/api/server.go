package api

import (
	"github.com/convert-studio/backend/internal/api/handlers"
	"github.com/convert-studio/backend/internal/api/middleware"
	"github.com/convert-studio/backend/internal/api/websocket"
	"github.com/convert-studio/backend/internal/modules/jobs"
	"github.com/convert-studio/backend/internal/modules/media"
	"github.com/convert-studio/backend/internal/shared/config"
	"github.com/convert-studio/backend/internal/shared/database"
	"github.com/convert-studio/backend/internal/shared/storage"
	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"go.uber.org/zap"
)

// ServerConfig holds dependencies for the API server
type ServerConfig struct {
	Config      *config.Config
	Logger      *zap.Logger
	DB          *database.Postgres
	Redis       *database.Redis
	Storage     *storage.Service
	WSHub       *websocket.Hub
	MediaModule *media.Module
	JobsModule  *jobs.Module
}

// Server represents the API server
type Server struct {
	config      *config.Config
	logger      *zap.Logger
	db          *database.Postgres
	redis       *database.Redis
	storage     *storage.Service
	wsHub       *websocket.Hub
	mediaModule *media.Module
	jobsModule  *jobs.Module
}

// NewServer creates a new API server
func NewServer(cfg ServerConfig) *Server {
	return &Server{
		config:      cfg.Config,
		logger:      cfg.Logger,
		db:          cfg.DB,
		redis:       cfg.Redis,
		storage:     cfg.Storage,
		wsHub:       cfg.WSHub,
		mediaModule: cfg.MediaModule,
		jobsModule:  cfg.JobsModule,
	}
}

// Router returns the configured HTTP router
func (s *Server) Router() *chi.Mux {
	r := chi.NewRouter()

	// Global middleware
	r.Use(chimiddleware.RequestID)
	r.Use(chimiddleware.RealIP)
	r.Use(middleware.Logger(s.logger))
	r.Use(chimiddleware.Recoverer)
	r.Use(chimiddleware.Compress(5))

	// CORS
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   s.config.AllowedOrigins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-Request-ID"},
		ExposedHeaders:   []string{"Link", "X-Request-ID"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	// Create handlers
	healthHandler := handlers.NewHealthHandler(s.db, s.redis)
	fileHandler := handlers.NewFileHandler(s.storage, s.db, s.logger)
	mediaHandler := handlers.NewMediaHandler(s.mediaModule, s.logger)
	jobHandler := handlers.NewJobHandler(s.jobsModule, s.logger)
	wsHandler := handlers.NewWebSocketHandler(s.wsHub, s.logger)

	// API routes
	r.Route("/api/v1", func(r chi.Router) {
		// Health check
		r.Get("/health", healthHandler.Health)
		r.Get("/ready", healthHandler.Ready)

		// File management
		r.Route("/files", func(r chi.Router) {
			r.Post("/upload", fileHandler.InitiateUpload)
			r.Post("/upload/simple", fileHandler.SimpleUpload)
			r.Post("/upload/chunk", fileHandler.UploadChunk)
			r.Post("/upload/complete", fileHandler.CompleteUpload)
			r.Get("/{id}", fileHandler.GetFile)
			r.Get("/{id}/download", fileHandler.DownloadFile)
			r.Get("/{id}/thumbnail", fileHandler.GetThumbnail)
			r.Delete("/{id}", fileHandler.DeleteFile)
		})

		// Media operations (FFmpeg)
		r.Route("/media", func(r chi.Router) {
			r.Post("/probe", mediaHandler.Probe)
			r.Get("/presets", mediaHandler.GetPresets)
			r.Get("/presets/{id}", mediaHandler.GetPreset)
			r.Post("/validate", mediaHandler.ValidateOperations)
			r.Get("/formats", mediaHandler.GetFormats)
			r.Get("/codecs", mediaHandler.GetCodecs)
		})

		// Job management
		r.Route("/jobs", func(r chi.Router) {
			r.Post("/", jobHandler.CreateJob)
			r.Get("/", jobHandler.ListJobs)
			r.Get("/{id}", jobHandler.GetJob)
			r.Delete("/{id}", jobHandler.CancelJob)
			r.Post("/{id}/retry", jobHandler.RetryJob)
			r.Get("/{id}/logs", jobHandler.GetJobLogs)
		})

		// WebSocket
		r.Get("/ws", wsHandler.HandleConnection)
	})

	return r
}
