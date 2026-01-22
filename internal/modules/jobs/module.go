package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/convert-studio/backend/internal/api/websocket"
	"github.com/convert-studio/backend/internal/shared/database"
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

// Job types
const (
	TypeMedia    = "media"
	TypeDocument = "document"
)

// Job represents a processing job
type Job struct {
	ID             string           `json:"id"`
	UserID         string           `json:"userId"`
	Type           string           `json:"type"`
	Status         string           `json:"status"`
	Priority       int              `json:"priority"`
	InputFileID    string           `json:"inputFileId"`
	OutputFileID   string           `json:"outputFileId,omitempty"`
	Operations     []Operation      `json:"operations,omitempty"`
	Conversion     *ConversionConfig `json:"conversion,omitempty"`
	OutputFileName string           `json:"outputFileName"`
	Progress       Progress         `json:"progress"`
	Error          *JobError        `json:"error,omitempty"`
	CreatedAt      time.Time        `json:"createdAt"`
	StartedAt      *time.Time       `json:"startedAt,omitempty"`
	CompletedAt    *time.Time       `json:"completedAt,omitempty"`
}

// Operation represents a media operation
type Operation struct {
	Type   string                 `json:"type"`
	Params map[string]interface{} `json:"params"`
}

// ConversionConfig represents document conversion options
type ConversionConfig struct {
	TargetFormat string                 `json:"targetFormat"`
	Options      map[string]interface{} `json:"options,omitempty"`
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
	UserID         string
	Type           string
	InputFileID    string
	Operations     []Operation
	Conversion     *ConversionConfig
	OutputFileName string
}

// Module handles job management
type Module struct {
	db     *database.Postgres
	redis  *database.Redis
	wsHub  *websocket.Hub
	logger *zap.Logger
	jobs   map[string]*Job // In-memory storage for demo
}

// NewModule creates a new jobs module
func NewModule(db *database.Postgres, redis *database.Redis, wsHub *websocket.Hub, logger *zap.Logger) *Module {
	return &Module{
		db:     db,
		redis:  redis,
		wsHub:  wsHub,
		logger: logger,
		jobs:   make(map[string]*Job),
	}
}

// CreateJob creates a new job
func (m *Module) CreateJob(ctx context.Context, params CreateJobParams) (*Job, error) {
	job := &Job{
		ID:             uuid.New().String(),
		UserID:         params.UserID,
		Type:           params.Type,
		Status:         StatusPending,
		Priority:       5, // Default priority
		InputFileID:    params.InputFileID,
		Operations:     params.Operations,
		Conversion:     params.Conversion,
		OutputFileName: params.OutputFileName,
		Progress:       Progress{Percent: 0},
		CreatedAt:      time.Now(),
	}

	// Store in memory (replace with DB in production)
	m.jobs[job.ID] = job

	// Queue the job
	// TODO: Add to Asynq queue

	m.logger.Info("Job created",
		zap.String("job_id", job.ID),
		zap.String("type", job.Type),
		zap.String("user_id", job.UserID),
	)

	return job, nil
}

// GetJob retrieves a job by ID
func (m *Module) GetJob(ctx context.Context, jobID string) (*Job, error) {
	job, ok := m.jobs[jobID]
	if !ok {
		return nil, fmt.Errorf("job not found: %s", jobID)
	}
	return job, nil
}

// ListJobs returns jobs for a user
func (m *Module) ListJobs(ctx context.Context, userID, status, jobType string) ([]*Job, error) {
	var jobs []*Job

	for _, job := range m.jobs {
		if job.UserID != userID && userID != "anonymous" {
			continue
		}
		if status != "" && job.Status != status {
			continue
		}
		if jobType != "" && job.Type != jobType {
			continue
		}
		jobs = append(jobs, job)
	}

	return jobs, nil
}

// CancelJob cancels a job
func (m *Module) CancelJob(ctx context.Context, jobID string) error {
	job, ok := m.jobs[jobID]
	if !ok {
		return fmt.Errorf("job not found: %s", jobID)
	}

	if job.Status == StatusCompleted || job.Status == StatusCancelled {
		return fmt.Errorf("job cannot be cancelled: status is %s", job.Status)
	}

	job.Status = StatusCancelled
	now := time.Now()
	job.CompletedAt = &now

	// Notify via WebSocket
	m.wsHub.BroadcastJobFailed(jobID, "Job cancelled by user")

	return nil
}

// RetryJob retries a failed job
func (m *Module) RetryJob(ctx context.Context, jobID string) (*Job, error) {
	oldJob, ok := m.jobs[jobID]
	if !ok {
		return nil, fmt.Errorf("job not found: %s", jobID)
	}

	if oldJob.Status != StatusFailed {
		return nil, fmt.Errorf("only failed jobs can be retried")
	}

	// Create a new job with the same parameters
	newJob := &Job{
		ID:             uuid.New().String(),
		UserID:         oldJob.UserID,
		Type:           oldJob.Type,
		Status:         StatusPending,
		Priority:       oldJob.Priority,
		InputFileID:    oldJob.InputFileID,
		Operations:     oldJob.Operations,
		Conversion:     oldJob.Conversion,
		OutputFileName: oldJob.OutputFileName,
		Progress:       Progress{Percent: 0},
		CreatedAt:      time.Now(),
	}

	m.jobs[newJob.ID] = newJob

	return newJob, nil
}

// GetJobLogs returns logs for a job
func (m *Module) GetJobLogs(ctx context.Context, jobID string) ([]string, error) {
	_, ok := m.jobs[jobID]
	if !ok {
		return nil, fmt.Errorf("job not found: %s", jobID)
	}

	// Return placeholder logs
	return []string{
		fmt.Sprintf("[%s] Job created", time.Now().Add(-5*time.Minute).Format(time.RFC3339)),
		fmt.Sprintf("[%s] Processing started", time.Now().Add(-4*time.Minute).Format(time.RFC3339)),
	}, nil
}

// UpdateProgress updates job progress
func (m *Module) UpdateProgress(ctx context.Context, jobID string, percent int, operation string, eta int) error {
	job, ok := m.jobs[jobID]
	if !ok {
		return fmt.Errorf("job not found: %s", jobID)
	}

	job.Progress = Progress{
		Percent:          percent,
		CurrentOperation: operation,
		ETA:              eta,
	}

	// Notify via WebSocket
	m.wsHub.BroadcastJobProgress(jobID, percent, operation, eta)

	return nil
}

// CompleteJob marks a job as completed
func (m *Module) CompleteJob(ctx context.Context, jobID, outputFileID string) error {
	job, ok := m.jobs[jobID]
	if !ok {
		return fmt.Errorf("job not found: %s", jobID)
	}

	job.Status = StatusCompleted
	job.OutputFileID = outputFileID
	job.Progress.Percent = 100
	now := time.Now()
	job.CompletedAt = &now

	// Notify via WebSocket
	m.wsHub.BroadcastJobCompleted(jobID, outputFileID)

	return nil
}

// FailJob marks a job as failed
func (m *Module) FailJob(ctx context.Context, jobID string, err error, retryable bool) error {
	job, ok := m.jobs[jobID]
	if !ok {
		return fmt.Errorf("job not found: %s", jobID)
	}

	job.Status = StatusFailed
	job.Error = &JobError{
		Code:      "PROCESSING_ERROR",
		Message:   err.Error(),
		Retryable: retryable,
	}
	now := time.Now()
	job.CompletedAt = &now

	// Notify via WebSocket
	m.wsHub.BroadcastJobFailed(jobID, err.Error())

	return nil
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
