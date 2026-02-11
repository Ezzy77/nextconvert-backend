package handlers

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/nextconvert/backend/internal/api/middleware"
	"github.com/nextconvert/backend/internal/shared/database"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// PresetsHandler handles user preset operations
type PresetsHandler struct {
	db     *database.Postgres
	logger *zap.Logger
}

// NewPresetsHandler creates a new presets handler
func NewPresetsHandler(db *database.Postgres, logger *zap.Logger) *PresetsHandler {
	return &PresetsHandler{db: db, logger: logger}
}

// UserPreset represents a user-created preset
type UserPreset struct {
	ID          string                   `json:"id"`
	Name        string                   `json:"name"`
	ToolID      string                   `json:"toolId"`
	MediaType   string                   `json:"mediaType"`
	Description string                   `json:"description,omitempty"`
	Options     map[string]interface{}   `json:"options"`
	Operations  []map[string]interface{} `json:"operations,omitempty"`
	CreatedAt   time.Time                `json:"createdAt"`
}

// CreatePresetRequest is the request body for creating a preset
type CreatePresetRequest struct {
	Name        string                   `json:"name"`
	ToolID      string                   `json:"toolId"`
	MediaType   string                   `json:"mediaType"`
	Description string                   `json:"description"`
	Options     map[string]interface{}   `json:"options"`
	Operations  []map[string]interface{} `json:"operations"`
}

// ListPresets returns the user's saved presets
func (h *PresetsHandler) ListPresets(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r.Context())
	if user == nil || user.ID == "anonymous" {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]UserPreset{})
		return
	}

	rows, err := h.db.Pool.Query(r.Context(), `
		SELECT id, name, description, operations, created_at
		FROM presets
		WHERE user_id = $1 AND (is_system = FALSE OR is_system IS NULL)
		ORDER BY created_at DESC
	`, user.ID)
	if err != nil {
		h.logger.Error("Failed to list presets", zap.Error(err))
		http.Error(w, "failed to list presets", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var presets []UserPreset
	for rows.Next() {
		var id, name, description string
		var operationsJSON []byte
		var createdAt time.Time

		if err := rows.Scan(&id, &name, &description, &operationsJSON, &createdAt); err != nil {
			continue
		}

		var ops []map[string]interface{}
		if len(operationsJSON) > 0 {
			json.Unmarshal(operationsJSON, &ops)
		}

		// Extract toolId and options from first operation if present
		toolID := ""
		options := make(map[string]interface{})
		if len(ops) > 0 {
			if t, ok := ops[0]["type"].(string); ok {
				toolID = t
			}
			if p, ok := ops[0]["params"].(map[string]interface{}); ok {
				options = p
			}
		}

		presets = append(presets, UserPreset{
			ID:          id,
			Name:        name,
			ToolID:      toolID,
			MediaType:   "video",
			Description: description,
			Options:     options,
			Operations:  ops,
			CreatedAt:   createdAt,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(presets)
}

// CreatePreset saves a new user preset
func (h *PresetsHandler) CreatePreset(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r.Context())
	if user == nil || user.ID == "anonymous" {
		http.Error(w, "authentication required", http.StatusUnauthorized)
		return
	}

	var req CreatePresetRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}

	operationsJSON, _ := json.Marshal(req.Operations)
	if len(req.Operations) == 0 && len(req.Options) > 0 {
		// Store options as single operation
		op := map[string]interface{}{"type": req.ToolID, "params": req.Options}
		operationsJSON, _ = json.Marshal([]map[string]interface{}{op})
	}

	id := uuid.New().String()
	_, err := h.db.Pool.Exec(r.Context(), `
		INSERT INTO presets (id, name, media_type, description, operations, is_system, user_id, created_at)
		VALUES ($1, $2, $3, $4, $5, FALSE, $6, NOW())
	`, id, req.Name, req.MediaType, req.Description, operationsJSON, user.ID)
	if err != nil {
		h.logger.Error("Failed to create preset", zap.Error(err))
		http.Error(w, "failed to create preset", http.StatusInternalServerError)
		return
	}

	preset := UserPreset{
		ID:          id,
		Name:        req.Name,
		ToolID:      req.ToolID,
		MediaType:   req.MediaType,
		Description: req.Description,
		Options:     req.Options,
		Operations:  req.Operations,
		CreatedAt:   time.Now(),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(preset)
}

// DeletePreset removes a user preset
func (h *PresetsHandler) DeletePreset(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r.Context())
	if user == nil || user.ID == "anonymous" {
		http.Error(w, "authentication required", http.StatusUnauthorized)
		return
	}

	presetID := chi.URLParam(r, "id")
	if presetID == "" {
		http.Error(w, "preset id required", http.StatusBadRequest)
		return
	}

	result, err := h.db.Pool.Exec(r.Context(), `
		DELETE FROM presets WHERE id = $1 AND user_id = $2 AND (is_system = FALSE OR is_system IS NULL)
	`, presetID, user.ID)
	if err != nil {
		h.logger.Error("Failed to delete preset", zap.Error(err))
		http.Error(w, "failed to delete preset", http.StatusInternalServerError)
		return
	}

	if result.RowsAffected() == 0 {
		http.Error(w, "preset not found", http.StatusNotFound)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
