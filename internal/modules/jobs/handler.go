package jobs

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/convert-studio/backend/internal/modules/document"
	"github.com/convert-studio/backend/internal/modules/media"
	"github.com/convert-studio/backend/internal/shared/database"
	"github.com/convert-studio/backend/internal/shared/storage"
	"github.com/hibiken/asynq"
	"go.uber.org/zap"
)

// HandlerConfig contains dependencies for the job handler
type HandlerConfig struct {
	DB                *database.Postgres
	Redis             *database.Redis
	Storage           *storage.Service
	MediaProcessor    *media.Processor
	DocumentProcessor *document.Processor
	Logger            *zap.Logger
}

// Handler handles job task execution
type Handler struct {
	db                *database.Postgres
	redis             *database.Redis
	storage           *storage.Service
	mediaProcessor    *media.Processor
	documentProcessor *document.Processor
	logger            *zap.Logger
}

// NewHandler creates a new job handler
func NewHandler(cfg HandlerConfig) *Handler {
	return &Handler{
		db:                cfg.DB,
		redis:             cfg.Redis,
		storage:           cfg.Storage,
		mediaProcessor:    cfg.MediaProcessor,
		documentProcessor: cfg.DocumentProcessor,
		logger:            cfg.Logger,
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

	// Convert operations
	operations := make([]media.Operation, len(payload.Operations))
	for i, op := range payload.Operations {
		operations[i] = media.Operation{
			Type:   op.Type,
			Params: op.Params,
		}
	}

	// Execute media processing
	err := h.mediaProcessor.Process(ctx, media.ProcessOptions{
		InputPath:  payload.InputPath,
		OutputPath: payload.OutputPath,
		Operations: operations,
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

// HandleDocumentConvert handles document conversion tasks
func (h *Handler) HandleDocumentConvert(ctx context.Context, task *asynq.Task) error {
	var payload DocumentConvertPayload
	if err := json.Unmarshal(task.Payload(), &payload); err != nil {
		return fmt.Errorf("failed to unmarshal payload: %w", err)
	}

	h.logger.Info("Converting document",
		zap.String("job_id", payload.JobID),
		zap.String("from", payload.FromFormat),
		zap.String("to", payload.ToFormat),
	)

	// Build conversion options
	options := document.ConversionOptions{
		TargetFormat: payload.ToFormat,
	}

	// Map options from payload
	if payload.Options != nil {
		if metadata, ok := payload.Options["metadata"].(map[string]interface{}); ok {
			options.Metadata = &document.Metadata{}
			if title, ok := metadata["title"].(string); ok {
				options.Metadata.Title = title
			}
			if author, ok := metadata["author"].(string); ok {
				options.Metadata.Author = author
			}
		}

		if toc, ok := payload.Options["tableOfContents"].(map[string]interface{}); ok {
			options.TableOfContents = &document.TOCOptions{}
			if enabled, ok := toc["enabled"].(bool); ok {
				options.TableOfContents.Enabled = enabled
			}
			if depth, ok := toc["depth"].(float64); ok {
				options.TableOfContents.Depth = int(depth)
			}
		}
	}

	// Execute document conversion
	err := h.documentProcessor.Process(ctx, document.ProcessOptions{
		InputPath:  payload.InputPath,
		OutputPath: payload.OutputPath,
		FromFormat: payload.FromFormat,
		ToFormat:   payload.ToFormat,
		Options:    options,
	})

	if err != nil {
		h.logger.Error("Document conversion failed",
			zap.String("job_id", payload.JobID),
			zap.Error(err),
		)
		return err
	}

	h.logger.Info("Document conversion completed",
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
