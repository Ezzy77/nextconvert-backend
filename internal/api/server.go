package api

import (
	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/nextconvert/backend/internal/api/handlers"
	"github.com/nextconvert/backend/internal/api/middleware"
	"github.com/nextconvert/backend/internal/api/websocket"
	"github.com/nextconvert/backend/internal/modules/jobs"
	"github.com/nextconvert/backend/internal/modules/media"
	"github.com/nextconvert/backend/internal/modules/subscription"
	"github.com/nextconvert/backend/internal/shared/config"
	"github.com/nextconvert/backend/internal/shared/database"
	"github.com/nextconvert/backend/internal/shared/storage"
	"go.uber.org/zap"
)

// ServerConfig holds dependencies for the API server
type ServerConfig struct {
	Config          *config.Config
	Logger          *zap.Logger
	DB              *database.Postgres
	Redis           *database.Redis
	Storage         *storage.Service
	WSHub           *websocket.Hub
	MediaModule     *media.Module
	JobsModule      *jobs.Module
	SubscriptionSvc *subscription.Service
}

// Server represents the API server
type Server struct {
	config          *config.Config
	logger          *zap.Logger
	db              *database.Postgres
	redis           *database.Redis
	storage         *storage.Service
	wsHub           *websocket.Hub
	mediaModule     *media.Module
	jobsModule      *jobs.Module
	subscriptionSvc *subscription.Service
}

// NewServer creates a new API server
func NewServer(cfg ServerConfig) *Server {
	return &Server{
		config:          cfg.Config,
		logger:          cfg.Logger,
		db:              cfg.DB,
		redis:           cfg.Redis,
		storage:         cfg.Storage,
		wsHub:           cfg.WSHub,
		mediaModule:     cfg.MediaModule,
		jobsModule:      cfg.JobsModule,
		subscriptionSvc: cfg.SubscriptionSvc,
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

	// CORS - allow all origins for now, enable credentials for anonymous cookie tracking.
	// With AllowedOrigins=["*"] and AllowCredentials=true, go-chi/cors will reflect the
	// request's Origin header back (not send literal "*") which is required by browsers.
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS", "HEAD"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-Request-ID", "Range"},
		ExposedHeaders:   []string{"Link", "X-Request-ID", "Content-Length", "Content-Range", "Content-Disposition"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	// Create rate limiter
	rateLimiter := middleware.NewRateLimiter(s.redis.Client, s.logger)

	// Apply global rate limit (100 req/min per IP) - before auth so it catches everything
	r.Use(rateLimiter.Limit(middleware.GlobalRateLimit))

	// Create Clerk auth middleware (subscription service provides tier lookup)
	isSecure := s.config.Environment == "production"
	clerkAuth := middleware.NewClerkAuthMiddlewareWithOptions(s.config.ClerkSecretKey, s.subscriptionSvc, isSecure)

	// Create handlers
	healthHandler := handlers.NewHealthHandler(s.db, s.redis)
	fileHandler := handlers.NewFileHandler(s.storage, s.db, s.subscriptionSvc, s.logger)
	mediaHandler := handlers.NewMediaHandler(s.mediaModule, s.logger)
	jobHandler := handlers.NewJobHandler(s.jobsModule, s.logger)
	presetsHandler := handlers.NewPresetsHandler(s.db, s.logger)
	wsHandler := handlers.NewWebSocketHandler(s.wsHub, s.logger)

	priceIDs := map[string]string{
		"basic":    s.config.StripeBasicPriceID,
		"standard": s.config.StripeStandardPriceID,
		"pro":      s.config.StripeProPriceID,
	}
	stripeHandler := handlers.NewStripeHandler(s.subscriptionSvc, s.config.StripeSecretKey, s.config.StripeWebhookSecret, priceIDs, s.config.StripeSuccessURL, s.config.StripeCancelURL, s.logger)
	subscriptionHandler := handlers.NewSubscriptionHandler(s.subscriptionSvc, stripeHandler, s.logger)

	// API routes
	r.Route("/api/v1", func(r chi.Router) {
		// Health check (public)
		r.Get("/health", healthHandler.Health)
		r.Get("/ready", healthHandler.Ready)

		// Stripe webhook (no auth - verified by signature, rate limited)
		r.With(rateLimiter.Limit(middleware.WebhookRateLimit)).
			Post("/webhooks/stripe", stripeHandler.HandleWebhook)

		// Protected routes - apply Clerk auth middleware
		r.Group(func(r chi.Router) {
			r.Use(clerkAuth.Handler)

			// File management
			r.Route("/files", func(r chi.Router) {
				// Upload routes: rate limited per user + stricter anonymous IP limit
				r.With(
					rateLimiter.Limit(middleware.FileUploadRateLimit),
					rateLimiter.Limit(middleware.AnonFileUploadRateLimit),
				).Post("/upload", fileHandler.InitiateUpload)
				r.With(
					rateLimiter.Limit(middleware.FileUploadRateLimit),
					rateLimiter.Limit(middleware.AnonFileUploadRateLimit),
				).Post("/upload/simple", fileHandler.SimpleUpload)
				r.With(
					rateLimiter.Limit(middleware.FileUploadRateLimit),
					rateLimiter.Limit(middleware.AnonFileUploadRateLimit),
				).Post("/upload/presign", fileHandler.GetPresignedUploadURL)
				r.Post("/upload/confirm", fileHandler.ConfirmPresignedUpload)
				r.Post("/upload/chunk", fileHandler.UploadChunk)
				r.Post("/upload/complete", fileHandler.CompleteUpload)
				r.Get("/", fileHandler.ListFiles)
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

			// User presets
			r.Route("/presets", func(r chi.Router) {
				r.Get("/", presetsHandler.ListPresets)
				r.Post("/", presetsHandler.CreatePreset)
				r.Delete("/{id}", presetsHandler.DeletePreset)
			})

			// Job management
			r.Route("/jobs", func(r chi.Router) {
				// Job creation: rate limited per user + stricter anonymous IP limit
				r.With(
					rateLimiter.Limit(middleware.JobCreationRateLimit),
					rateLimiter.Limit(middleware.AnonJobCreationRateLimit),
				).Post("/", jobHandler.CreateJob)
				r.Get("/", jobHandler.ListJobs)
				r.Get("/{id}", jobHandler.GetJob)
				r.Delete("/{id}", jobHandler.DeleteJob)
				r.Post("/{id}/cancel", jobHandler.CancelJob)
				r.Post("/{id}/retry", jobHandler.RetryJob)
				r.Get("/{id}/logs", jobHandler.GetJobLogs)
			})

			// WebSocket
			r.Get("/ws", wsHandler.HandleConnection)

			// Subscription
			r.Route("/subscription", func(r chi.Router) {
				r.Get("/me", subscriptionHandler.GetMe)
				r.Post("/checkout", subscriptionHandler.CreateCheckout)
				r.Post("/portal", subscriptionHandler.CreatePortal)
			})
		})
	})

	return r
}
