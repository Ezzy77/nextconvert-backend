package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/convert-studio/backend/internal/modules/document"
	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

// DocumentHandler handles document-related endpoints
type DocumentHandler struct {
	module *document.Module
	logger *zap.Logger
}

// NewDocumentHandler creates a new document handler
func NewDocumentHandler(module *document.Module, logger *zap.Logger) *DocumentHandler {
	return &DocumentHandler{
		module: module,
		logger: logger,
	}
}

// GetFormats returns the format conversion matrix
func (h *DocumentHandler) GetFormats(w http.ResponseWriter, r *http.Request) {
	formats := h.module.GetFormatMatrix()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(formats)
}

// GetTemplates returns available templates
func (h *DocumentHandler) GetTemplates(w http.ResponseWriter, r *http.Request) {
	format := r.URL.Query().Get("format")
	templates := h.module.GetTemplates(format)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(templates)
}

// GetTemplate returns a specific template
func (h *DocumentHandler) GetTemplate(w http.ResponseWriter, r *http.Request) {
	templateID := chi.URLParam(r, "id")

	template, err := h.module.GetTemplate(templateID)
	if err != nil {
		http.Error(w, "template not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(template)
}

// CreateTemplateRequest represents a template creation request
type CreateTemplateRequest struct {
	Name        string `json:"name"`
	Format      string `json:"format"`
	Description string `json:"description"`
	Content     string `json:"content"`
}

// CreateTemplate creates a new custom template
func (h *DocumentHandler) CreateTemplate(w http.ResponseWriter, r *http.Request) {
	var req CreateTemplateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	template, err := h.module.CreateTemplate(document.Template{
		Name:        req.Name,
		Format:      req.Format,
		Description: req.Description,
		Content:     req.Content,
		IsSystem:    false,
	})
	if err != nil {
		h.logger.Error("Failed to create template", zap.Error(err))
		http.Error(w, "failed to create template", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(template)
}

// ValidateConversionRequest represents a validation request
type ValidateConversionRequest struct {
	FromFormat string                    `json:"fromFormat"`
	ToFormat   string                    `json:"toFormat"`
	Options    document.ConversionOptions `json:"options"`
}

// ValidateConversion validates conversion options
func (h *DocumentHandler) ValidateConversion(w http.ResponseWriter, r *http.Request) {
	var req ValidateConversionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	result := h.module.ValidateConversion(req.FromFormat, req.ToFormat, req.Options)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

// GetCitationStyles returns available citation styles
func (h *DocumentHandler) GetCitationStyles(w http.ResponseWriter, r *http.Request) {
	styles := h.module.GetCitationStyles()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(styles)
}
