package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/convert-studio/backend/internal/modules/jobs"
	"github.com/convert-studio/backend/internal/modules/media"
	"github.com/convert-studio/backend/internal/shared/config"
	"github.com/convert-studio/backend/internal/shared/database"
	"github.com/convert-studio/backend/internal/shared/logging"
	"github.com/convert-studio/backend/internal/shared/storage"
	"github.com/hibiken/asynq"
	"go.uber.org/zap"
)

var (
	Version   = "dev"
	BuildTime = "unknown"
)

// mediaProcessorAdapter adapts media.Processor to jobs.MediaProcessorInterface
type mediaProcessorAdapter struct {
	processor *media.Processor
}

func (a *mediaProcessorAdapter) Process(ctx context.Context, opts jobs.MediaProcessOptions) error {
	// Convert jobs.Operation to media.Operation
	operations := make([]media.Operation, len(opts.Operations))
	for i, op := range opts.Operations {
		operations[i] = media.Operation{
			Type:   op.Type,
			Params: op.Params,
		}
	}

	return a.processor.Process(ctx, media.ProcessOptions{
		InputPath:  opts.InputPath,
		OutputPath: opts.OutputPath,
		Operations: operations,
		OnProgress: opts.OnProgress,
	})
}

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

	logger.Info("Starting Convert Studio Worker",
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

	// Initialize media processor
	mediaProcessor := media.NewProcessor(storageService, cfg.FFmpegPath, logger)
	mediaAdapter := &mediaProcessorAdapter{processor: mediaProcessor}

	// Create job handler
	jobHandler := jobs.NewHandler(jobs.HandlerConfig{
		DB:             db,
		Redis:          redisClient,
		Storage:        storageService,
		MediaProcessor: mediaAdapter,
		Logger:         logger,
	})

	// Configure Asynq server
	srv := asynq.NewServer(
		asynq.RedisClientOpt{Addr: cfg.RedisURL},
		asynq.Config{
			Concurrency: cfg.WorkerConcurrency,
			Queues: map[string]int{
				"critical": 6,
				"default":  3,
				"low":      1,
			},
			ErrorHandler: asynq.ErrorHandlerFunc(func(ctx context.Context, task *asynq.Task, err error) {
				logger.Error("Task failed",
					zap.String("type", task.Type()),
					zap.Error(err),
				)
			}),
		},
	)

	// Register task handlers
	mux := asynq.NewServeMux()
	mux.HandleFunc(jobs.TypeMediaProcess, jobHandler.HandleMediaProcess)
	mux.HandleFunc(jobs.TypeCleanupFiles, jobHandler.HandleCleanupFiles)

	// Start worker
	go func() {
		logger.Info("Worker started", zap.Int("concurrency", cfg.WorkerConcurrency))
		if err := srv.Run(mux); err != nil {
			logger.Fatal("Worker failed", zap.Error(err))
		}
	}()

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("Shutting down worker...")
	srv.Shutdown()
	logger.Info("Worker stopped")
}
