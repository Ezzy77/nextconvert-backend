package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/convert-studio/backend/internal/api/websocket"
	"github.com/convert-studio/backend/internal/modules/subscription"
	"github.com/convert-studio/backend/internal/shared/database"
	"github.com/convert-studio/backend/internal/shared/storage"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// Job statuses
const (
	StatusPending    = "pending"
	StatusQueued     = "queued"
	StatusProcessing = "processing"
	StatusCompleted  = "completed"
	StatusFailed     = "failed"
	StatusCancelled  = "cancelled"
)

// Job represents a media processing job
type Job struct {
	ID             string      `json:"id"`
	UserID         string      `json:"userId"`
	Status         string      `json:"status"`
	Priority       int         `json:"priority"`
	InputFileID    string      `json:"inputFileId"`
	InputFilePath  string      `json:"inputFilePath,omitempty"`
	OutputFileID   string      `json:"outputFileId,omitempty"`
	Operations     []Operation `json:"operations"`
	OutputFormat   string      `json:"outputFormat"`
	OutputFileName string      `json:"outputFileName"`
	Progress       Progress    `json:"progress"`
	Error          *JobError   `json:"error,omitempty"`
	CreatedAt      time.Time   `json:"createdAt"`
	StartedAt      *time.Time  `json:"startedAt,omitempty"`
	CompletedAt    *time.Time  `json:"completedAt,omitempty"`
}

// Operation represents a media operation
type Operation struct {
	Type   string                 `json:"type"`
	Params map[string]interface{} `json:"params"`
}

// Progress represents job progress
type Progress struct {
	Percent          int    `json:"percent"`
	CurrentOperation string `json:"currentOperation,omitempty"`
	ETA              int    `json:"eta,omitempty"`
}

// JobError represents a job error
type JobError struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable"`
}

// CreateJobParams contains parameters for creating a job
type CreateJobParams struct {
	UserID                string
	InputFileID           string
	InputFileIDs          []string // For merge operations (multiple files)
	Operations            []Operation
	OutputFormat          string
	OutputFileName        string
	InputDurationSeconds  float64 // From probe, for conversion minutes
	ConversionMinutes     int     // ceil(duration/60), or 1 for audio/image
}

// Module handles job management
type Module struct {
	db        *database.Postgres
	redis     *database.Redis
	storage   *storage.Service
	queue     *QueueClient
	wsHub     *websocket.Hub
	subSvc    *subscription.Service
	logger    *zap.Logger
	jobs      map[string]*Job // In-memory cache (also stored in DB)
}

// NewModule creates a new jobs module
func NewModule(db *database.Postgres, redis *database.Redis, storage *storage.Service, queue *QueueClient, wsHub *websocket.Hub, subSvc *subscription.Service, logger *zap.Logger) *Module {
	return &Module{
		db:      db,
		redis:   redis,
		storage: storage,
		queue:   queue,
		wsHub:   wsHub,
		subSvc:  subSvc,
		logger:  logger,
		jobs:    make(map[string]*Job),
	}
}

// CreateJob creates a new media processing job
func (m *Module) CreateJob(ctx context.Context, params CreateJobParams) (*Job, error) {
	// Check conversion minutes limit before creating job
	if m.subSvc != nil && params.UserID != "" {
		convMin := params.ConversionMinutes
		if convMin <= 0 {
			convMin = 1 // Default 1 min for unknown/audio/image
		}
		if err := m.subSvc.CheckLimit(ctx, params.UserID, "conversion_minutes", int64(convMin)); err != nil {
			return nil, fmt.Errorf("conversion minutes limit exceeded: %w", err)
		}
	}

	// Check if this is a merge operation (multiple input files)
	isMerge := len(params.InputFileIDs) > 1

	var inputFilePath string
	var inputFilePaths []string
	var originalName string

	if isMerge {
		// Look up all input files for merge
		inputFilePaths = make([]string, 0, len(params.InputFileIDs))
		for i, fileID := range params.InputFileIDs {
			var storagePath, name string
			err := m.db.Pool.QueryRow(ctx, `
				SELECT storage_path, original_name FROM files WHERE id = $1
			`, fileID).Scan(&storagePath, &name)
			if err != nil {
				return nil, fmt.Errorf("input file %d not found: %w", i+1, err)
			}
			inputFilePaths = append(inputFilePaths, storagePath)
			if i == 0 {
				originalName = name
				inputFilePath = storagePath
			}
		}
		// Use first file ID as primary input
		if params.InputFileID == "" && len(params.InputFileIDs) > 0 {
			params.InputFileID = params.InputFileIDs[0]
		}
	} else {
		// Single input file
		err := m.db.Pool.QueryRow(ctx, `
			SELECT storage_path, original_name FROM files WHERE id = $1
		`, params.InputFileID).Scan(&inputFilePath, &originalName)
		if err != nil {
			return nil, fmt.Errorf("input file not found: %w", err)
		}
	}

	// Generate output filename if not provided
	outputFileName := params.OutputFileName
	if outputFileName == "" {
		ext := filepath.Ext(originalName)
		baseName := strings.TrimSuffix(originalName, ext)
		if isMerge {
			outputFileName = fmt.Sprintf("%s_merged.%s", baseName, params.OutputFormat)
		} else {
			outputFileName = fmt.Sprintf("%s_converted.%s", baseName, params.OutputFormat)
		}
	}

	// Resolve file references in operation params (e.g., audioPath for addAudio)
	for i, op := range params.Operations {
		if op.Type == "addAudio" {
			if audioFileID, ok := op.Params["audioPath"].(string); ok && audioFileID != "" {
				var audioStoragePath string
				err := m.db.Pool.QueryRow(ctx, `
					SELECT storage_path FROM files WHERE id = $1
				`, audioFileID).Scan(&audioStoragePath)
				if err != nil {
					return nil, fmt.Errorf("audio file not found: %w", err)
				}
				// Update the operation params with resolved path
				params.Operations[i].Params["audioPath"] = audioStoragePath
			}
		}
	}

	jobID := uuid.New().String()
	now := time.Now()

	convMin := params.ConversionMinutes
	if convMin <= 0 {
		convMin = 1
	}
	priority := 5
	queuePriority := "default"
	if m.subSvc != nil && params.UserID != "" {
		limits := subscription.GetTierLimits(m.subSvc.GetTier(ctx, params.UserID))
		switch limits.Priority {
		case "critical":
			priority = 1
			queuePriority = "critical"
		case "high":
			priority = 3
			queuePriority = "high"
		}
	}

	job := &Job{
		ID:             jobID,
		UserID:         params.UserID,
		Status:         StatusQueued,
		Priority:       priority,
		InputFileID:    params.InputFileID,
		InputFilePath:  inputFilePath,
		Operations:     params.Operations,
		OutputFormat:   params.OutputFormat,
		OutputFileName: outputFileName,
		Progress:       Progress{Percent: 0},
		CreatedAt:      now,
	}

	// Store in database
	operationsJSON, _ := json.Marshal(params.Operations)
	progressJSON, _ := json.Marshal(job.Progress)

	_, err := m.db.Pool.Exec(ctx, `
		INSERT INTO jobs (id, user_id, status, priority, input_file_id, output_format, output_file_name, operations, progress, input_duration_seconds, conversion_minutes, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
	`, jobID, nullString(params.UserID), job.Status, priority, params.InputFileID, params.OutputFormat, outputFileName, operationsJSON, progressJSON, params.InputDurationSeconds, convMin, now)
	if err != nil {
		return nil, fmt.Errorf("failed to insert job: %w", err)
	}

	// Keep in memory cache
	m.jobs[job.ID] = job

	// Generate output path
	outputPath := m.storage.GetPath(storage.ZoneOutput, fmt.Sprintf("%s.%s", jobID, params.OutputFormat))

	// Enqueue to Asynq
	useGPU := false
	if m.subSvc != nil && params.UserID != "" {
		limits := subscription.GetTierLimits(m.subSvc.GetTier(ctx, params.UserID))
		useGPU = limits.UseGPUEncoding
	}
	payload := MediaProcessPayload{
		JobID:      jobID,
		InputPath:  inputFilePath,
		OutputPath: outputPath,
		Operations: params.Operations,
		UseGPU:     useGPU,
	}
	// Add multiple input paths for merge
	if isMerge {
		payload.InputPaths = inputFilePaths
	}

	_, err = m.queue.EnqueueMediaProcess(payload, queuePriority)
	if err != nil {
		// Update job status to failed
		m.db.Pool.Exec(ctx, "UPDATE jobs SET status = $1 WHERE id = $2", StatusFailed, jobID)
		return nil, fmt.Errorf("failed to enqueue job: %w", err)
	}

	m.logger.Info("Job created and queued",
		zap.String("job_id", job.ID),
		zap.String("user_id", job.UserID),
		zap.String("input_file", params.InputFileID),
		zap.Int("input_files_count", len(inputFilePaths)),
		zap.Int("operations", len(job.Operations)),
		zap.Bool("is_merge", isMerge),
	)

	return job, nil
}

// GetJob retrieves a job by ID - always reads from database for fresh status
func (m *Module) GetJob(ctx context.Context, jobID string) (*Job, error) {
	// Always query from database to get fresh status
	job, err := m.getJobFromDB(ctx, jobID)
	if err != nil {
		return nil, err
	}
	return job, nil
}

// ListJobs returns jobs for a user
func (m *Module) ListJobs(ctx context.Context, userID, status, jobType string) ([]*Job, error) {
	query := `
		SELECT id, user_id, status, priority, input_file_id, output_file_id, output_format, output_file_name, 
		       operations, progress, error, created_at, started_at, completed_at
		FROM jobs
		WHERE ($1 = '' OR $1 = 'anonymous' OR user_id = $1 OR user_id IS NULL)
		  AND ($2 = '' OR status = $2::job_status)
		ORDER BY created_at DESC
		LIMIT 50
	`

	rows, err := m.db.Pool.Query(ctx, query, userID, status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var jobs []*Job
	for rows.Next() {
		job, err := m.scanJobRow(rows)
		if err != nil {
			m.logger.Error("Failed to scan job row", zap.Error(err))
			continue
		}
		jobs = append(jobs, job)
	}

	return jobs, nil
}

// CancelJob cancels a job
func (m *Module) CancelJob(ctx context.Context, jobID string) error {
	job, err := m.GetJob(ctx, jobID)
	if err != nil {
		return err
	}

	if job.Status == StatusCompleted || job.Status == StatusCancelled {
		return fmt.Errorf("job cannot be cancelled: status is %s", job.Status)
	}

	now := time.Now()
	_, err = m.db.Pool.Exec(ctx, `
		UPDATE jobs SET status = $1, completed_at = $2 WHERE id = $3
	`, StatusCancelled, now, jobID)
	if err != nil {
		return err
	}

	// Update cache
	job.Status = StatusCancelled
	job.CompletedAt = &now

	// Notify via WebSocket (if hub available)
	if m.wsHub != nil {
		m.wsHub.BroadcastJobFailed(jobID, "Job cancelled by user")
	}

	return nil
}

// DeleteJob removes a job from the database
func (m *Module) DeleteJob(ctx context.Context, jobID string) error {
	// Delete from database
	result, err := m.db.Pool.Exec(ctx, `DELETE FROM jobs WHERE id = $1`, jobID)
	if err != nil {
		return fmt.Errorf("failed to delete job: %w", err)
	}

	if result.RowsAffected() == 0 {
		return fmt.Errorf("job not found")
	}

	// Remove from cache
	delete(m.jobs, jobID)

	return nil
}

// RetryJob retries a failed job
func (m *Module) RetryJob(ctx context.Context, jobID string) (*Job, error) {
	oldJob, err := m.GetJob(ctx, jobID)
	if err != nil {
		return nil, err
	}

	if oldJob.Status != StatusFailed {
		return nil, fmt.Errorf("only failed jobs can be retried")
	}

	// Create a new job with the same parameters
	return m.CreateJob(ctx, CreateJobParams{
		UserID:         oldJob.UserID,
		InputFileID:    oldJob.InputFileID,
		Operations:     oldJob.Operations,
		OutputFormat:   oldJob.OutputFormat,
		OutputFileName: oldJob.OutputFileName,
	})
}

// GetJobLogs returns logs for a job
func (m *Module) GetJobLogs(ctx context.Context, jobID string) ([]string, error) {
	job, err := m.GetJob(ctx, jobID)
	if err != nil {
		return nil, err
	}

	// Build logs from job state
	logs := []string{
		fmt.Sprintf("[%s] Job created", job.CreatedAt.Format(time.RFC3339)),
	}

	if job.StartedAt != nil {
		logs = append(logs, fmt.Sprintf("[%s] Processing started", job.StartedAt.Format(time.RFC3339)))
	}

	if job.Progress.CurrentOperation != "" {
		logs = append(logs, fmt.Sprintf("[%s] %s (%d%%)", time.Now().Format(time.RFC3339), job.Progress.CurrentOperation, job.Progress.Percent))
	}

	if job.CompletedAt != nil {
		if job.Status == StatusCompleted {
			logs = append(logs, fmt.Sprintf("[%s] Job completed successfully", job.CompletedAt.Format(time.RFC3339)))
		} else if job.Status == StatusFailed && job.Error != nil {
			logs = append(logs, fmt.Sprintf("[%s] Job failed: %s", job.CompletedAt.Format(time.RFC3339), job.Error.Message))
		}
	}

	return logs, nil
}

// UpdateProgress updates job progress
func (m *Module) UpdateProgress(ctx context.Context, jobID string, percent int, operation string, eta int) error {
	progress := Progress{
		Percent:          percent,
		CurrentOperation: operation,
		ETA:              eta,
	}
	progressJSON, _ := json.Marshal(progress)

	_, err := m.db.Pool.Exec(ctx, `
		UPDATE jobs SET progress = $1, status = $2, started_at = COALESCE(started_at, NOW()) WHERE id = $3
	`, progressJSON, StatusProcessing, jobID)
	if err != nil {
		return err
	}

	// Update cache
	if job, ok := m.jobs[jobID]; ok {
		job.Progress = progress
		job.Status = StatusProcessing
	}

	// Notify via WebSocket (if hub available)
	if m.wsHub != nil {
		m.wsHub.BroadcastJobProgress(jobID, percent, operation, eta)
	}

	return nil
}

// CompleteJob marks a job as completed
func (m *Module) CompleteJob(ctx context.Context, jobID, outputFileID string) error {
	// Get user_id and conversion_minutes for usage recording
	var jobUserID *string
	var convMin int
	m.db.Pool.QueryRow(ctx, `SELECT user_id, COALESCE(conversion_minutes, 0) FROM jobs WHERE id = $1`, jobID).Scan(&jobUserID, &convMin)
	if m.subSvc != nil && jobUserID != nil && *jobUserID != "" && *jobUserID != "anonymous" && convMin > 0 {
		if err := m.subSvc.RecordConversionMinutes(ctx, *jobUserID, convMin); err != nil {
			m.logger.Warn("Failed to record conversion minutes", zap.Error(err), zap.String("user_id", *jobUserID))
		}
	}

	now := time.Now()
	progress := Progress{Percent: 100}
	progressJSON, _ := json.Marshal(progress)

	m.logger.Info("CompleteJob: Updating job in database",
		zap.String("job_id", jobID),
		zap.String("output_file_id", outputFileID),
		zap.String("status", StatusCompleted),
	)

	result, err := m.db.Pool.Exec(ctx, `
		UPDATE jobs SET status = $1, output_file_id = $2, progress = $3, completed_at = $4 WHERE id = $5
	`, StatusCompleted, outputFileID, progressJSON, now, jobID)
	if err != nil {
		m.logger.Error("CompleteJob: Failed to update job in database", zap.Error(err))
		return err
	}

	m.logger.Info("CompleteJob: Database update successful",
		zap.String("job_id", jobID),
		zap.Int64("rows_affected", result.RowsAffected()),
	)

	// Update cache
	if job, ok := m.jobs[jobID]; ok {
		job.Status = StatusCompleted
		job.OutputFileID = outputFileID
		job.Progress.Percent = 100
		job.CompletedAt = &now
	}

	// Notify via WebSocket (if hub available)
	if m.wsHub != nil {
		m.wsHub.BroadcastJobCompleted(jobID, outputFileID)
	}

	return nil
}

// FailJob marks a job as failed
func (m *Module) FailJob(ctx context.Context, jobID string, err error, retryable bool) error {
	now := time.Now()
	jobError := JobError{
		Code:      "PROCESSING_ERROR",
		Message:   err.Error(),
		Retryable: retryable,
	}
	errorJSON, _ := json.Marshal(jobError)

	_, dbErr := m.db.Pool.Exec(ctx, `
		UPDATE jobs SET status = $1, error = $2, completed_at = $3 WHERE id = $4
	`, StatusFailed, errorJSON, now, jobID)
	if dbErr != nil {
		return dbErr
	}

	// Update cache
	if job, ok := m.jobs[jobID]; ok {
		job.Status = StatusFailed
		job.Error = &jobError
		job.CompletedAt = &now
	}

	// Notify via WebSocket (if hub available)
	if m.wsHub != nil {
		m.wsHub.BroadcastJobFailed(jobID, err.Error())
	}

	return nil
}

// getJobFromDB retrieves a job from the database
func (m *Module) getJobFromDB(ctx context.Context, jobID string) (*Job, error) {
	row := m.db.Pool.QueryRow(ctx, `
		SELECT id, user_id, status, priority, input_file_id, output_file_id, output_format, output_file_name, 
		       operations, progress, error, created_at, started_at, completed_at
		FROM jobs WHERE id = $1
	`, jobID)

	return m.scanJobSingleRow(row)
}

type rowScanner interface {
	Scan(dest ...interface{}) error
}

func (m *Module) scanJobSingleRow(row rowScanner) (*Job, error) {
	var job Job
	var userID *string
	var outputFileID *string
	var operationsJSON, progressJSON, errorJSON []byte
	var startedAt, completedAt *time.Time

	err := row.Scan(
		&job.ID, &userID, &job.Status, &job.Priority, &job.InputFileID, &outputFileID,
		&job.OutputFormat, &job.OutputFileName, &operationsJSON, &progressJSON, &errorJSON,
		&job.CreatedAt, &startedAt, &completedAt,
	)
	if err != nil {
		return nil, err
	}

	if userID != nil {
		job.UserID = *userID
	}
	if outputFileID != nil {
		job.OutputFileID = *outputFileID
	}
	if startedAt != nil {
		job.StartedAt = startedAt
	}
	if completedAt != nil {
		job.CompletedAt = completedAt
	}

	json.Unmarshal(operationsJSON, &job.Operations)
	json.Unmarshal(progressJSON, &job.Progress)
	if errorJSON != nil {
		json.Unmarshal(errorJSON, &job.Error)
	}

	return &job, nil
}

type rowsScanner interface {
	Scan(dest ...interface{}) error
}

func (m *Module) scanJobRow(rows rowsScanner) (*Job, error) {
	return m.scanJobSingleRow(rows)
}

func nullString(s string) *string {
	if s == "" || s == "anonymous" {
		return nil
	}
	return &s
}

// SerializeJob serializes a job to JSON
func SerializeJob(job *Job) ([]byte, error) {
	return json.Marshal(job)
}

// DeserializeJob deserializes a job from JSON
func DeserializeJob(data []byte) (*Job, error) {
	var job Job
	if err := json.Unmarshal(data, &job); err != nil {
		return nil, err
	}
	return &job, nil
}
