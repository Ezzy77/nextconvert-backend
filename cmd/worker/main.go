package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"strings"

	"github.com/nextconvert/backend/internal/modules/jobs"
	"github.com/nextconvert/backend/internal/modules/media"
	"github.com/nextconvert/backend/internal/modules/subscription"
	"github.com/nextconvert/backend/internal/shared/config"
	"github.com/nextconvert/backend/internal/shared/database"
	"github.com/nextconvert/backend/internal/shared/logging"
	"github.com/nextconvert/backend/internal/shared/storage"
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
		InputPath:        opts.InputPath,
		OutputPath:       opts.OutputPath,
		Operations:       operations,
		OnProgress:       opts.OnProgress,
		UseHardwareAccel: opts.UseHardwareAccel,
	})
}

func (a *mediaProcessorAdapter) ProcessMerge(ctx context.Context, opts jobs.MergeProcessOptions) error {
	return a.processor.ProcessMerge(ctx, media.MergeOptions{
		InputPaths:       opts.InputPaths,
		OutputPath:       opts.OutputPath,
		OnProgress:       opts.OnProgress,
		UseHardwareAccel: opts.UseHardwareAccel,
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

	logger.Info("Starting NextConvert Worker",
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
	logger.Info("Initializing storage",
		zap.String("backend", cfg.Storage.Backend),
		zap.String("base_path", cfg.Storage.BasePath),
	)
	storageService, err := storage.NewService(cfg.Storage)
	if err != nil {
		logger.Fatal("Failed to initialize storage", zap.Error(err))
	}
	logger.Info("Storage initialized successfully",
		zap.String("output_path_example", storageService.GetPath(storage.ZoneOutput, "test.mp4")),
	)

	// Initialize queue client (for potential re-queuing)
	queueClient := jobs.NewQueueClient(cfg.RedisURL, logger)
	defer queueClient.Close()

	// Initialize subscription service (for recording conversion minutes on job complete)
	subscriptionSvc := subscription.NewService(db)

	// Initialize jobs module (without WebSocket hub - worker doesn't need it)
	jobsModule := jobs.NewModule(db, redisClient, storageService, queueClient, nil, subscriptionSvc, logger)

	// Initialize media processor with CPU-friendly settings
	mediaProcessor := media.NewProcessorWithConfig(storageService, media.ProcessorConfig{
		FFmpegPath:        cfg.FFmpegPath,
		MaxThreads:        cfg.FFmpegMaxThreads,    // Limit CPU threads (default: 2)
		UseHardwareAccel:  cfg.FFmpegHardwareAccel, // Use VideoToolbox on macOS
		PreferFastPresets: cfg.FFmpegFastPresets,   // Use veryfast preset
	}, logger)

	logger.Info("Media processor initialized",
		zap.Int("max_threads", cfg.FFmpegMaxThreads),
		zap.Bool("hardware_accel", cfg.FFmpegHardwareAccel),
		zap.Bool("fast_presets", cfg.FFmpegFastPresets),
	)

	mediaAdapter := &mediaProcessorAdapter{processor: mediaProcessor}

	// Create job handler
	jobHandler := jobs.NewHandler(jobs.HandlerConfig{
		DB:             db,
		Redis:          redisClient,
		Storage:        storageService,
		MediaProcessor: mediaAdapter,
		JobsModule:     jobsModule,
		Logger:         logger,
	})

	// Configure Asynq server
	var redisOpt asynq.RedisConnOpt
	if strings.HasPrefix(cfg.RedisURL, "redis://") || strings.HasPrefix(cfg.RedisURL, "rediss://") {
		opt, err := asynq.ParseRedisURI(cfg.RedisURL)
		if err != nil {
			logger.Fatal("Failed to parse Redis URI", zap.Error(err))
		}
		redisOpt = opt
	} else {
		redisOpt = asynq.RedisClientOpt{Addr: cfg.RedisURL}
	}

	srv := asynq.NewServer(
		redisOpt,
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
	mux.HandleFunc(jobs.TypeCleanupStaleJobs, jobHandler.HandleCleanupStaleJobs)
	mux.HandleFunc(jobs.TypeCleanupAnonProfiles, jobHandler.HandleCleanupAnonProfiles)

	// Start cleanup scheduler (hourly - deletes files past 24h expiry)
	scheduler, err := queueClient.ScheduleCleanup(cfg.RedisURL)
	if err != nil {
		logger.Fatal("Failed to create cleanup scheduler", zap.Error(err))
	}
	go func() {
		logger.Info("Cleanup scheduler started (runs hourly)")
		if err := scheduler.Run(); err != nil {
			logger.Error("Cleanup scheduler failed", zap.Error(err))
		}
	}()

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
