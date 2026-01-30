package jobs

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/convert-studio/backend/internal/shared/database"
	"github.com/convert-studio/backend/internal/shared/storage"
	"github.com/hibiken/asynq"
	"go.uber.org/zap"
)

// MediaProcessorInterface defines the interface for media processing
type MediaProcessorInterface interface {
	Process(ctx context.Context, opts MediaProcessOptions) error
}

// MediaProcessOptions mirrors media.ProcessOptions to avoid import
type MediaProcessOptions struct {
	InputPath  string
	OutputPath string
	Operations []Operation
	OnProgress func(percent int, operation string)
}

// HandlerConfig contains dependencies for the job handler
type HandlerConfig struct {
	DB             *database.Postgres
	Redis          *database.Redis
	Storage        *storage.Service
	MediaProcessor MediaProcessorInterface
	Logger         *zap.Logger
}

// Handler handles job task execution
type Handler struct {
	db             *database.Postgres
	redis          *database.Redis
	storage        *storage.Service
	mediaProcessor MediaProcessorInterface
	logger         *zap.Logger
}

// NewHandler creates a new job handler
func NewHandler(cfg HandlerConfig) *Handler {
	return &Handler{
		db:             cfg.DB,
		redis:          cfg.Redis,
		storage:        cfg.Storage,
		mediaProcessor: cfg.MediaProcessor,
		logger:         cfg.Logger,
	}
}

// HandleMediaProcess handles media processing tasks
func (h *Handler) HandleMediaProcess(ctx context.Context, task *asynq.Task) error {
	var payload MediaProcessPayload
	if err := json.Unmarshal(task.Payload(), &payload); err != nil {
		return fmt.Errorf("failed to unmarshal payload: %w", err)
	}

	h.logger.Info("Processing media job",
		zap.String("job_id", payload.JobID),
		zap.String("input", payload.InputPath),
	)

	// Execute media processing
	err := h.mediaProcessor.Process(ctx, MediaProcessOptions{
		InputPath:  payload.InputPath,
		OutputPath: payload.OutputPath,
		Operations: payload.Operations,
		OnProgress: func(percent int, operation string) {
			// Update progress via Redis pub/sub
			h.logger.Debug("Media processing progress",
				zap.String("job_id", payload.JobID),
				zap.Int("percent", percent),
				zap.String("operation", operation),
			)
		},
	})

	if err != nil {
		h.logger.Error("Media processing failed",
			zap.String("job_id", payload.JobID),
			zap.Error(err),
		)
		return err
	}

	h.logger.Info("Media processing completed",
		zap.String("job_id", payload.JobID),
		zap.String("output", payload.OutputPath),
	)

	return nil
}

// HandleCleanupFiles handles file cleanup tasks
func (h *Handler) HandleCleanupFiles(ctx context.Context, task *asynq.Task) error {
	var payload CleanupPayload
	if err := json.Unmarshal(task.Payload(), &payload); err != nil {
		return fmt.Errorf("failed to unmarshal payload: %w", err)
	}

	h.logger.Info("Cleaning up files",
		zap.String("zone", payload.Zone),
		zap.Int64("older_than", payload.OlderThan),
	)

	// TODO: Implement file cleanup
	// 1. List files in zone older than timestamp
	// 2. Delete files and DB records

	return nil
}
