package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/convert-studio/backend/internal/api/middleware"
	"github.com/convert-studio/backend/internal/modules/jobs"
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
	Type           string                 `json:"type"` // media or document
	InputFileID    string                 `json:"inputFileId"`
	Operations     []jobs.Operation       `json:"operations,omitempty"`
	Conversion     *jobs.ConversionConfig `json:"conversion,omitempty"`
	OutputFileName string                 `json:"outputFileName"`
}

// CreateJob creates a new processing job
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

	job, err := h.module.CreateJob(r.Context(), jobs.CreateJobParams{
		UserID:         userID,
		Type:           req.Type,
		InputFileID:    req.InputFileID,
		Operations:     req.Operations,
		Conversion:     req.Conversion,
		OutputFileName: req.OutputFileName,
	})
	if err != nil {
		h.logger.Error("Failed to create job", zap.Error(err))
		http.Error(w, "failed to create job", http.StatusInternalServerError)
		return
	}

	h.logger.Info("Job created",
		zap.String("job_id", job.ID),
		zap.String("type", req.Type),
		zap.String("user_id", userID),
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
	jobType := r.URL.Query().Get("type")

	jobsList, err := h.module.ListJobs(r.Context(), userID, status, jobType)
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

// CancelJob cancels a job
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
