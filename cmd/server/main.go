package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/convert-studio/backend/internal/api"
	"github.com/convert-studio/backend/internal/api/websocket"
	"github.com/convert-studio/backend/internal/modules/jobs"
	"github.com/convert-studio/backend/internal/modules/media"
	"github.com/convert-studio/backend/internal/modules/subscription"
	"github.com/convert-studio/backend/internal/shared/config"
	"github.com/convert-studio/backend/internal/shared/database"
	"github.com/convert-studio/backend/internal/shared/logging"
	"github.com/convert-studio/backend/internal/shared/storage"
	"go.uber.org/zap"
)

var (
	Version   = "dev"
	BuildTime = "unknown"
)

func main() {
	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		os.Exit(1)
	}

	// Initialize logger
	logger, err := logging.NewLogger(cfg.LogLevel, cfg.Environment)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize logger: %v\n", err)
		os.Exit(1)
	}
	defer logger.Sync()

	logger.Info("Starting Convert Studio API Server",
		zap.String("version", Version),
		zap.String("build_time", BuildTime),
		zap.String("environment", cfg.Environment),
	)

	// Initialize database
	db, err := database.NewPostgres(cfg.DatabaseURL)
	if err != nil {
		logger.Fatal("Failed to connect to database", zap.Error(err))
	}
	defer db.Close()

	// Initialize Redis
	redisClient, err := database.NewRedis(cfg.RedisURL)
	if err != nil {
		logger.Fatal("Failed to connect to Redis", zap.Error(err))
	}
	defer redisClient.Close()

	// Initialize storage
	storageService, err := storage.NewService(cfg.Storage)
	if err != nil {
		logger.Fatal("Failed to initialize storage", zap.Error(err))
	}

	// Initialize WebSocket hub
	wsHub := websocket.NewHub(logger)
	go wsHub.Run()

	// Initialize job queue client
	jobQueue := jobs.NewQueueClient(cfg.RedisURL, logger)

	// Initialize modules
	subscriptionSvc := subscription.NewService(db)
	mediaModule := media.NewModule(db, storageService, jobQueue, logger)
	jobsModule := jobs.NewModule(db, redisClient, storageService, jobQueue, wsHub, subscriptionSvc, logger)

	// Create API server
	server := api.NewServer(api.ServerConfig{
		Config:          cfg,
		Logger:          logger,
		DB:              db,
		Redis:           redisClient,
		Storage:         storageService,
		WSHub:           wsHub,
		MediaModule:     mediaModule,
		JobsModule:      jobsModule,
		SubscriptionSvc: subscriptionSvc,
	})

	// Create HTTP server
	httpServer := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.Port),
		Handler:      server.Router(),
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// Start server in goroutine
	go func() {
		logger.Info("API server listening", zap.Int("port", cfg.Port))
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatal("Server failed", zap.Error(err))
		}
	}()

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("Shutting down server...")

	// Graceful shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := httpServer.Shutdown(ctx); err != nil {
		logger.Error("Server forced to shutdown", zap.Error(err))
	}

	logger.Info("Server stopped")
}
