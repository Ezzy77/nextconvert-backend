package handlers

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/convert-studio/backend/internal/api/middleware"
	"github.com/convert-studio/backend/internal/modules/jobs"
	"github.com/convert-studio/backend/internal/modules/subscription"
	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

// JobHandler handles job-related endpoints
type JobHandler struct {
	module *jobs.Module
	logger *zap.Logger
}

// NewJobHandler creates a new job handler
func NewJobHandler(module *jobs.Module, logger *zap.Logger) *JobHandler {
	return &JobHandler{
		module: module,
		logger: logger,
	}
}

// CreateJobRequest represents a job creation request
type CreateJobRequest struct {
	InputFileID           string           `json:"inputFileId"`
	InputFileIDs          []string         `json:"inputFileIds,omitempty"` // For merge operations
	Operations            []jobs.Operation `json:"operations"`
	OutputFormat          string           `json:"outputFormat"`
	OutputFileName        string           `json:"outputFileName"`
	InputDurationSeconds  float64          `json:"inputDurationSeconds"`  // From probe, for conversion minutes
}

// CreateJob creates a new media processing job
func (h *JobHandler) CreateJob(w http.ResponseWriter, r *http.Request) {
	var req CreateJobRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	user := middleware.GetUser(r.Context())
	userID := "anonymous"
	if user != nil {
		userID = user.ID
	}

	// Use InputFileIDs if provided (merge), otherwise use single InputFileID
	inputFileID := req.InputFileID
	if len(req.InputFileIDs) > 0 && inputFileID == "" {
		inputFileID = req.InputFileIDs[0]
	}

	convMin := subscription.ConversionMinutesFromDuration(req.InputDurationSeconds)

	job, err := h.module.CreateJob(r.Context(), jobs.CreateJobParams{
		UserID:               userID,
		InputFileID:          inputFileID,
		InputFileIDs:         req.InputFileIDs,
		Operations:           req.Operations,
		OutputFormat:         req.OutputFormat,
		OutputFileName:       req.OutputFileName,
		InputDurationSeconds: req.InputDurationSeconds,
		ConversionMinutes:    convMin,
	})
	if err != nil {
		errStr := err.Error()
		if strings.Contains(errStr, "conversion minutes limit") || strings.Contains(errStr, "file size") || strings.Contains(errStr, "exceeds limit") {
			h.logger.Warn("Job creation limit exceeded", zap.Error(err))
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			json.NewEncoder(w).Encode(map[string]string{
				"error":   errStr,
				"code":    "LIMIT_EXCEEDED",
				"message": errStr,
			})
			return
		}
		h.logger.Error("Failed to create job", zap.Error(err))
		http.Error(w, "failed to create job", http.StatusInternalServerError)
		return
	}

	h.logger.Info("Job created",
		zap.String("job_id", job.ID),
		zap.String("user_id", userID),
		zap.Int("operations", len(req.Operations)),
		zap.Int("input_files", len(req.InputFileIDs)),
	)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(job)
}

// ListJobs returns the user's jobs
func (h *JobHandler) ListJobs(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r.Context())
	userID := "anonymous"
	if user != nil {
		userID = user.ID
	}

	status := r.URL.Query().Get("status")

	jobsList, err := h.module.ListJobs(r.Context(), userID, status, "")
	if err != nil {
		h.logger.Error("Failed to list jobs", zap.Error(err))
		http.Error(w, "failed to list jobs", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(jobsList)
}

// GetJob returns a specific job
func (h *JobHandler) GetJob(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "id")

	job, err := h.module.GetJob(r.Context(), jobID)
	if err != nil {
		http.Error(w, "job not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(job)
}

// CancelJob cancels a job (keeps it in database)
func (h *JobHandler) CancelJob(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "id")

	if err := h.module.CancelJob(r.Context(), jobID); err != nil {
		h.logger.Error("Failed to cancel job", zap.Error(err), zap.String("job_id", jobID))
		http.Error(w, "failed to cancel job", http.StatusInternalServerError)
		return
	}

	h.logger.Info("Job cancelled", zap.String("job_id", jobID))
	w.WriteHeader(http.StatusNoContent)
}

// DeleteJob deletes a job from the database
func (h *JobHandler) DeleteJob(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "id")

	if err := h.module.DeleteJob(r.Context(), jobID); err != nil {
		h.logger.Error("Failed to delete job", zap.Error(err), zap.String("job_id", jobID))
		http.Error(w, "failed to delete job", http.StatusInternalServerError)
		return
	}

	h.logger.Info("Job deleted", zap.String("job_id", jobID))
	w.WriteHeader(http.StatusNoContent)
}

// RetryJob retries a failed job
func (h *JobHandler) RetryJob(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "id")

	newJob, err := h.module.RetryJob(r.Context(), jobID)
	if err != nil {
		h.logger.Error("Failed to retry job", zap.Error(err), zap.String("job_id", jobID))
		http.Error(w, "failed to retry job", http.StatusInternalServerError)
		return
	}

	h.logger.Info("Job retried", zap.String("old_job_id", jobID), zap.String("new_job_id", newJob.ID))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(newJob)
}

// GetJobLogs returns job execution logs
func (h *JobHandler) GetJobLogs(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "id")

	logs, err := h.module.GetJobLogs(r.Context(), jobID)
	if err != nil {
		http.Error(w, "logs not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"jobId": jobID,
		"logs":  logs,
	})
}
