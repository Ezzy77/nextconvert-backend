package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

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
	JobsModule     *Module
	Logger         *zap.Logger
}

// Handler handles job task execution
type Handler struct {
	db             *database.Postgres
	redis          *database.Redis
	storage        *storage.Service
	mediaProcessor MediaProcessorInterface
	jobsModule     *Module
	logger         *zap.Logger
}

// NewHandler creates a new job handler
func NewHandler(cfg HandlerConfig) *Handler {
	return &Handler{
		db:             cfg.DB,
		redis:          cfg.Redis,
		storage:        cfg.Storage,
		mediaProcessor: cfg.MediaProcessor,
		jobsModule:     cfg.JobsModule,
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
		zap.String("output", payload.OutputPath),
		zap.Int("operations", len(payload.Operations)),
	)

	// Update job status to processing
	if h.jobsModule != nil {
		h.jobsModule.UpdateProgress(ctx, payload.JobID, 0, "Starting...", 0)
	}

	// Ensure output directory exists
	outputDir := filepath.Dir(payload.OutputPath)
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		h.logger.Error("Failed to create output directory", zap.Error(err))
		if h.jobsModule != nil {
			h.jobsModule.FailJob(ctx, payload.JobID, err, true)
		}
		return err
	}

	// Execute media processing with progress callback
	err := h.mediaProcessor.Process(ctx, MediaProcessOptions{
		InputPath:  payload.InputPath,
		OutputPath: payload.OutputPath,
		Operations: payload.Operations,
		OnProgress: func(percent int, operation string) {
			h.logger.Debug("Media processing progress",
				zap.String("job_id", payload.JobID),
				zap.Int("percent", percent),
				zap.String("operation", operation),
			)
			// Update progress in jobs module (which broadcasts via WebSocket)
			if h.jobsModule != nil {
				h.jobsModule.UpdateProgress(ctx, payload.JobID, percent, operation, 0)
			}
		},
	})

	if err != nil {
		h.logger.Error("Media processing failed",
			zap.String("job_id", payload.JobID),
			zap.Error(err),
		)
		if h.jobsModule != nil {
			h.jobsModule.FailJob(ctx, payload.JobID, err, true)
		}
		return err
	}

	// Get output file info
	outputInfo, err := os.Stat(payload.OutputPath)
	if err != nil {
		h.logger.Error("Failed to stat output file", zap.Error(err))
		if h.jobsModule != nil {
			h.jobsModule.FailJob(ctx, payload.JobID, fmt.Errorf("output file not found"), false)
		}
		return err
	}

	// Insert output file into database
	outputFileID := payload.JobID // Use job ID as the output file ID for simplicity
	outputFileName := filepath.Base(payload.OutputPath)
	mimeType := "video/mp4" // Default, could detect based on extension

	_, err = h.db.Pool.Exec(ctx, `
		INSERT INTO files (id, original_name, storage_path, mime_type, size_bytes, zone, media_type, expires_at, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NOW())
	`, outputFileID, outputFileName, payload.OutputPath, mimeType, outputInfo.Size(), "output", "video", time.Now().Add(7*24*time.Hour))
	if err != nil {
		h.logger.Error("Failed to insert output file into database", zap.Error(err))
		// Don't fail the job, the file exists
	}

	// Mark job as completed
	if h.jobsModule != nil {
		h.logger.Info("Marking job as completed",
			zap.String("job_id", payload.JobID),
			zap.String("output_file_id", outputFileID),
		)
		if err := h.jobsModule.CompleteJob(ctx, payload.JobID, outputFileID); err != nil {
			h.logger.Error("Failed to mark job as completed", zap.Error(err))
		} else {
			h.logger.Info("Job marked as completed successfully", zap.String("job_id", payload.JobID))
		}
	} else {
		h.logger.Warn("JobsModule is nil, cannot mark job as completed")
	}

	h.logger.Info("Media processing completed",
		zap.String("job_id", payload.JobID),
		zap.String("output", payload.OutputPath),
		zap.Int64("output_size", outputInfo.Size()),
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

	// Query files to delete
	rows, err := h.db.Pool.Query(ctx, `
		SELECT id, storage_path FROM files 
		WHERE zone = $1 AND created_at < to_timestamp($2)
	`, payload.Zone, payload.OlderThan)
	if err != nil {
		return fmt.Errorf("failed to query files: %w", err)
	}
	defer rows.Close()

	var deletedCount int
	for rows.Next() {
		var fileID, storagePath string
		if err := rows.Scan(&fileID, &storagePath); err != nil {
			continue
		}

		// Delete from storage
		if err := h.storage.Delete(ctx, storagePath); err != nil {
			h.logger.Warn("Failed to delete file from storage", zap.Error(err), zap.String("path", storagePath))
		}

		// Delete from database
		if _, err := h.db.Pool.Exec(ctx, "DELETE FROM files WHERE id = $1", fileID); err != nil {
			h.logger.Warn("Failed to delete file from database", zap.Error(err), zap.String("id", fileID))
		}

		deletedCount++
	}

	h.logger.Info("Cleanup completed", zap.Int("deleted", deletedCount))
	return nil
}
