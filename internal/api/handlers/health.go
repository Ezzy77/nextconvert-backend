package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/nextconvert/backend/internal/shared/database"
)

// HealthHandler handles health check endpoints
type HealthHandler struct {
	db    *database.Postgres
	redis *database.Redis
}

// NewHealthHandler creates a new health handler
func NewHealthHandler(db *database.Postgres, redis *database.Redis) *HealthHandler {
	return &HealthHandler{
		db:    db,
		redis: redis,
	}
}

// HealthResponse represents the health check response
type HealthResponse struct {
	Status    string            `json:"status"`
	Timestamp string            `json:"timestamp"`
	Services  map[string]string `json:"services,omitempty"`
}

// Health returns a basic health check
func (h *HealthHandler) Health(w http.ResponseWriter, r *http.Request) {
	response := HealthResponse{
		Status:    "ok",
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// Ready returns a readiness check including dependencies
func (h *HealthHandler) Ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	services := make(map[string]string)
	allHealthy := true

	// Check PostgreSQL
	if err := h.db.HealthCheck(ctx); err != nil {
		services["postgres"] = "unhealthy: " + err.Error()
		allHealthy = false
	} else {
		services["postgres"] = "healthy"
	}

	// Check Redis
	if err := h.redis.HealthCheck(ctx); err != nil {
		services["redis"] = "unhealthy: " + err.Error()
		allHealthy = false
	} else {
		services["redis"] = "healthy"
	}

	status := "ok"
	statusCode := http.StatusOK
	if !allHealthy {
		status = "degraded"
		statusCode = http.StatusServiceUnavailable
	}

	response := HealthResponse{
		Status:    status,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Services:  services,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(response)
}
