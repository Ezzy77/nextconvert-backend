package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/nextconvert/backend/internal/modules/media"
	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

// MediaHandler handles media-related endpoints
type MediaHandler struct {
	module *media.Module
	logger *zap.Logger
}

// NewMediaHandler creates a new media handler
func NewMediaHandler(module *media.Module, logger *zap.Logger) *MediaHandler {
	return &MediaHandler{
		module: module,
		logger: logger,
	}
}

// ProbeRequest represents a media probe request
type ProbeRequest struct {
	FileID string `json:"fileId"`
}

// Probe extracts metadata from a media file
func (h *MediaHandler) Probe(w http.ResponseWriter, r *http.Request) {
	var req ProbeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	info, err := h.module.Probe(r.Context(), req.FileID)
	if err != nil {
		h.logger.Error("Failed to probe file", zap.Error(err), zap.String("file_id", req.FileID))
		http.Error(w, "failed to probe file", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(info)
}

// GetPresets returns all available presets
func (h *MediaHandler) GetPresets(w http.ResponseWriter, r *http.Request) {
	presets := h.module.GetPresets()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(presets)
}

// GetPreset returns a specific preset
func (h *MediaHandler) GetPreset(w http.ResponseWriter, r *http.Request) {
	presetID := chi.URLParam(r, "id")
	
	preset, err := h.module.GetPreset(presetID)
	if err != nil {
		http.Error(w, "preset not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(preset)
}

// ValidateOperationsRequest represents a validation request
type ValidateOperationsRequest struct {
	Operations []media.Operation `json:"operations"`
	InputType  string            `json:"inputType"`
}

// ValidateOperations validates a chain of operations
func (h *MediaHandler) ValidateOperations(w http.ResponseWriter, r *http.Request) {
	var req ValidateOperationsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	result := h.module.ValidateOperations(req.Operations, req.InputType)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

// GetFormats returns supported media formats
func (h *MediaHandler) GetFormats(w http.ResponseWriter, r *http.Request) {
	formats := h.module.GetSupportedFormats()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(formats)
}

// GetCodecs returns available codecs
func (h *MediaHandler) GetCodecs(w http.ResponseWriter, r *http.Request) {
	codecs := h.module.GetAvailableCodecs()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(codecs)
}
