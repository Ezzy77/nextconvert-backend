package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nextconvert/backend/internal/shared/database"
	"github.com/nextconvert/backend/internal/shared/storage"
	"github.com/hibiken/asynq"
	"go.uber.org/zap"
)

// MediaProcessorInterface defines the interface for media processing
type MediaProcessorInterface interface {
	Process(ctx context.Context, opts MediaProcessOptions) error
	ProcessMerge(ctx context.Context, opts MergeProcessOptions) error
}

// MediaProcessOptions mirrors media.ProcessOptions to avoid import
type MediaProcessOptions struct {
	InputPath        string
	OutputPath       string
	Operations       []Operation
	OnProgress       func(percent int, operation string)
	UseHardwareAccel *bool
}

// MergeProcessOptions contains options for merging multiple videos
type MergeProcessOptions struct {
	InputPaths       []string
	OutputPath       string
	OnProgress       func(percent int, operation string)
	UseHardwareAccel *bool
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

	// Check if this is a merge operation
	isMerge := len(payload.InputPaths) > 1

	h.logger.Info("Processing media job",
		zap.String("job_id", payload.JobID),
		zap.String("input", payload.InputPath),
		zap.Int("input_count", len(payload.InputPaths)),
		zap.String("output", payload.OutputPath),
		zap.Int("operations", len(payload.Operations)),
		zap.Bool("is_merge", isMerge),
	)

	// Update job status to processing
	if h.jobsModule != nil {
		h.jobsModule.UpdateProgress(ctx, payload.JobID, 0, "Starting...", 0)
	}

	var inputPath, outputPath string
	var cleanups []func()

	// For remote storage: download inputs to temp, use temp for output
	if h.storage.IsRemote() {
		if isMerge {
			// Download all inputs for merge
			localInputs := make([]string, len(payload.InputPaths))
			for i, p := range payload.InputPaths {
				local, cleanup, err := h.storage.PrepareInputForProcessing(ctx, p)
				if err != nil {
					for _, c := range cleanups {
						c()
					}
					if h.jobsModule != nil {
						h.jobsModule.FailJob(ctx, payload.JobID, err, true)
					}
					return err
				}
				cleanups = append(cleanups, cleanup)
				localInputs[i] = local
			}
			payload.InputPaths = localInputs
		} else {
			local, cleanup, err := h.storage.PrepareInputForProcessing(ctx, payload.InputPath)
			if err != nil {
				if h.jobsModule != nil {
					h.jobsModule.FailJob(ctx, payload.JobID, err, true)
				}
				return err
			}
			cleanups = append(cleanups, cleanup)
			inputPath = local
		}
		// Temp output path for FFmpeg
		tmpDir := filepath.Join(os.TempDir(), "conv")
		if err := os.MkdirAll(tmpDir, 0755); err != nil {
			for _, c := range cleanups {
				c()
			}
			if h.jobsModule != nil {
				h.jobsModule.FailJob(ctx, payload.JobID, err, true)
			}
			return err
		}
		outputPath = filepath.Join(tmpDir, filepath.Base(payload.OutputPath))
		if !isMerge {
			payload.InputPath = inputPath
		}
		payload.OutputPath = outputPath
	} else {
		// Local: ensure output directory exists
		outputDir := filepath.Dir(payload.OutputPath)
		if err := os.MkdirAll(outputDir, 0755); err != nil {
			h.logger.Error("Failed to create output directory", zap.Error(err))
			if h.jobsModule != nil {
				h.jobsModule.FailJob(ctx, payload.JobID, err, true)
			}
			return err
		}
	}

	defer func() {
		for _, c := range cleanups {
			c()
		}
	}()

	var err error

	useGPU := payload.UseGPU
	if isMerge {
		// Execute merge operation
		err = h.mediaProcessor.ProcessMerge(ctx, MergeProcessOptions{
			InputPaths:       payload.InputPaths,
			OutputPath:       payload.OutputPath,
			UseHardwareAccel: &useGPU,
			OnProgress: func(percent int, operation string) {
				h.logger.Debug("Merge processing progress",
					zap.String("job_id", payload.JobID),
					zap.Int("percent", percent),
					zap.String("operation", operation),
				)
				if h.jobsModule != nil {
					h.jobsModule.UpdateProgress(ctx, payload.JobID, percent, operation, 0)
				}
			},
		})
	} else {
		// Execute regular media processing with progress callback
		err = h.mediaProcessor.Process(ctx, MediaProcessOptions{
			InputPath:        payload.InputPath,
			OutputPath:       payload.OutputPath,
			Operations:       payload.Operations,
			UseHardwareAccel: &useGPU,
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
	}

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

	// For remote storage: upload output to Supabase, get size from local first
	storagePath := payload.OutputPath
	var outputSize int64

	if h.storage.IsRemote() {
		localOutputPath := payload.OutputPath
		outputInfo, statErr := os.Stat(localOutputPath)
		if statErr != nil {
			h.logger.Error("Failed to stat output file", zap.Error(statErr))
			if h.jobsModule != nil {
				h.jobsModule.FailJob(ctx, payload.JobID, fmt.Errorf("output file not found"), false)
			}
			return statErr
		}
		outputSize = outputInfo.Size()
		storagePath = h.storage.GetPath(storage.ZoneOutput, filepath.Base(payload.OutputPath))
		if err := h.storage.FinalizeOutputFromLocal(ctx, storagePath, localOutputPath); err != nil {
			h.logger.Error("Failed to upload output to storage", zap.Error(err))
			if h.jobsModule != nil {
				h.jobsModule.FailJob(ctx, payload.JobID, err, true)
			}
			return err
		}
		os.Remove(localOutputPath) // Clean up temp
	} else {
		outputInfo, statErr := os.Stat(payload.OutputPath)
		if statErr != nil {
			h.logger.Error("Failed to stat output file", zap.Error(statErr))
			if h.jobsModule != nil {
				h.jobsModule.FailJob(ctx, payload.JobID, fmt.Errorf("output file not found"), false)
			}
			return statErr
		}
		outputSize = outputInfo.Size()
	}

	// Insert output file into database
	outputFileID := payload.JobID
	outputFileName := filepath.Base(storagePath)
	ext := strings.ToLower(filepath.Ext(outputFileName))

	// Detect mime type and media type based on extension
	mimeType, mediaType := detectMimeType(ext)

	_, err = h.db.Pool.Exec(ctx, `
		INSERT INTO files (id, original_name, storage_path, mime_type, size_bytes, zone, media_type, expires_at, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NOW())
	`, outputFileID, outputFileName, storagePath, mimeType, outputSize, "output", mediaType, time.Now().Add(24*time.Hour))
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
		zap.String("output", storagePath),
		zap.Int64("output_size", outputSize),
	)

	return nil
}

// HandleCleanupFiles handles file cleanup tasks - permanently deletes expired files from storage and DB
func (h *Handler) HandleCleanupFiles(ctx context.Context, task *asynq.Task) error {
	var payload CleanupPayload
	if err := json.Unmarshal(task.Payload(), &payload); err != nil {
		return fmt.Errorf("failed to unmarshal payload: %w", err)
	}

	h.logger.Info("Cleaning up expired files", zap.String("zone", payload.Zone))

	// Query expired files: expires_at < NOW() (24h policy - all files expire after 24h)
	query := `SELECT id, storage_path FROM files WHERE expires_at IS NOT NULL AND expires_at < NOW()`
	args := []interface{}{}
	if payload.Zone != "" && payload.Zone != "all" {
		query = `SELECT id, storage_path FROM files WHERE zone = $1 AND expires_at IS NOT NULL AND expires_at < NOW()`
		args = append(args, payload.Zone)
	}
	rows, err := h.db.Pool.Query(ctx, query, args...)
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

// detectMimeType returns the mime type and media type for a file extension
func detectMimeType(ext string) (mimeType string, mediaType string) {
	switch ext {
	// Video formats
	case ".mp4":
		return "video/mp4", "video"
	case ".mov":
		return "video/quicktime", "video"
	case ".avi":
		return "video/x-msvideo", "video"
	case ".mkv":
		return "video/x-matroska", "video"
	case ".webm":
		return "video/webm", "video"
	case ".flv":
		return "video/x-flv", "video"
	case ".wmv":
		return "video/x-ms-wmv", "video"
	case ".m4v":
		return "video/x-m4v", "video"
	case ".mpg", ".mpeg":
		return "video/mpeg", "video"
	case ".3gp":
		return "video/3gpp", "video"
	case ".ogv":
		return "video/ogg", "video"

	// Audio formats
	case ".mp3":
		return "audio/mpeg", "audio"
	case ".wav":
		return "audio/wav", "audio"
	case ".aac":
		return "audio/aac", "audio"
	case ".ogg", ".oga":
		return "audio/ogg", "audio"
	case ".flac":
		return "audio/flac", "audio"
	case ".m4a":
		return "audio/mp4", "audio"
	case ".wma":
		return "audio/x-ms-wma", "audio"
	case ".opus":
		return "audio/opus", "audio"
	case ".amr":
		return "audio/amr", "audio"
	case ".aiff", ".aif":
		return "audio/aiff", "audio"

	// Image formats (for thumbnails, GIFs)
	case ".gif":
		return "image/gif", "image"
	case ".jpg", ".jpeg":
		return "image/jpeg", "image"
	case ".png":
		return "image/png", "image"
	case ".webp":
		return "image/webp", "image"
	case ".bmp":
		return "image/bmp", "image"

	// Default fallback
	default:
		return "application/octet-stream", "video"
	}
}
