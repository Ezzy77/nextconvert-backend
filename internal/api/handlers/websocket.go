package handlers

import (
	"net/http"

	"github.com/nextconvert/backend/internal/api/websocket"
	"go.uber.org/zap"
)

// WebSocketHandler handles WebSocket connections
type WebSocketHandler struct {
	hub    *websocket.Hub
	logger *zap.Logger
}

// NewWebSocketHandler creates a new WebSocket handler
func NewWebSocketHandler(hub *websocket.Hub, logger *zap.Logger) *WebSocketHandler {
	return &WebSocketHandler{
		hub:    hub,
		logger: logger,
	}
}

// HandleConnection upgrades HTTP to WebSocket
func (h *WebSocketHandler) HandleConnection(w http.ResponseWriter, r *http.Request) {
	h.hub.HandleConnection(w, r)
}
