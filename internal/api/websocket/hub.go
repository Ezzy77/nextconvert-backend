package websocket

import (
	"encoding/json"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		// TODO: Implement proper origin checking
		return true
	},
}

// Message represents a WebSocket message
type Message struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

// JobProgressPayload represents a job progress update
type JobProgressPayload struct {
	JobID            string `json:"jobId"`
	Percent          int    `json:"percent"`
	CurrentOperation string `json:"currentOperation,omitempty"`
	ETA              int    `json:"eta,omitempty"`
}

// JobCompletedPayload represents a job completion
type JobCompletedPayload struct {
	JobID        string `json:"jobId"`
	OutputFileID string `json:"outputFileId"`
}

// JobFailedPayload represents a job failure
type JobFailedPayload struct {
	JobID string `json:"jobId"`
	Error string `json:"error"`
}

// Client represents a WebSocket client
type Client struct {
	hub          *Hub
	conn         *websocket.Conn
	send         chan []byte
	subscriptions map[string]bool
	mu           sync.RWMutex
}

// Hub manages WebSocket connections
type Hub struct {
	clients    map[*Client]bool
	broadcast  chan []byte
	register   chan *Client
	unregister chan *Client
	logger     *zap.Logger
	mu         sync.RWMutex
}

// NewHub creates a new WebSocket hub
func NewHub(logger *zap.Logger) *Hub {
	return &Hub{
		clients:    make(map[*Client]bool),
		broadcast:  make(chan []byte),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		logger:     logger,
	}
}

// Run starts the hub's main loop
func (h *Hub) Run() {
	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			h.clients[client] = true
			h.mu.Unlock()
			h.logger.Debug("Client connected", zap.Int("total_clients", len(h.clients)))

		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
			}
			h.mu.Unlock()
			h.logger.Debug("Client disconnected", zap.Int("total_clients", len(h.clients)))

		case message := <-h.broadcast:
			h.mu.RLock()
			for client := range h.clients {
				select {
				case client.send <- message:
				default:
					close(client.send)
					delete(h.clients, client)
				}
			}
			h.mu.RUnlock()
		}
	}
}

// HandleConnection handles a new WebSocket connection
func (h *Hub) HandleConnection(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		h.logger.Error("WebSocket upgrade failed", zap.Error(err))
		return
	}

	client := &Client{
		hub:           h,
		conn:          conn,
		send:          make(chan []byte, 256),
		subscriptions: make(map[string]bool),
	}

	h.register <- client

	go client.writePump()
	go client.readPump()
}

// SendToJob sends a message to all clients subscribed to a job
func (h *Hub) SendToJob(jobID string, msgType string, payload interface{}) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	msg := Message{
		Type:    msgType,
		Payload: data,
	}

	msgBytes, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	h.mu.RLock()
	defer h.mu.RUnlock()

	for client := range h.clients {
		client.mu.RLock()
		subscribed := client.subscriptions[jobID]
		client.mu.RUnlock()

		if subscribed {
			select {
			case client.send <- msgBytes:
			default:
				// Client buffer full, skip
			}
		}
	}

	return nil
}

// BroadcastJobProgress sends a progress update
func (h *Hub) BroadcastJobProgress(jobID string, percent int, operation string, eta int) {
	h.SendToJob(jobID, "job:progress", JobProgressPayload{
		JobID:            jobID,
		Percent:          percent,
		CurrentOperation: operation,
		ETA:              eta,
	})
}

// BroadcastJobCompleted sends a completion notification
func (h *Hub) BroadcastJobCompleted(jobID, outputFileID string) {
	h.SendToJob(jobID, "job:completed", JobCompletedPayload{
		JobID:        jobID,
		OutputFileID: outputFileID,
	})
}

// BroadcastJobFailed sends a failure notification
func (h *Hub) BroadcastJobFailed(jobID, errorMsg string) {
	h.SendToJob(jobID, "job:failed", JobFailedPayload{
		JobID: jobID,
		Error: errorMsg,
	})
}

func (c *Client) readPump() {
	defer func() {
		c.hub.unregister <- c
		c.conn.Close()
	}()

	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				c.hub.logger.Error("WebSocket read error", zap.Error(err))
			}
			break
		}

		var msg Message
		if err := json.Unmarshal(message, &msg); err != nil {
			c.hub.logger.Warn("Invalid WebSocket message", zap.Error(err))
			continue
		}

		c.handleMessage(msg)
	}
}

func (c *Client) writePump() {
	defer c.conn.Close()

	for message := range c.send {
		if err := c.conn.WriteMessage(websocket.TextMessage, message); err != nil {
			c.hub.logger.Error("WebSocket write error", zap.Error(err))
			return
		}
	}
}

func (c *Client) handleMessage(msg Message) {
	switch msg.Type {
	case "subscribe":
		var payload struct {
			JobID string `json:"jobId"`
		}
		if err := json.Unmarshal(msg.Payload, &payload); err == nil {
			c.mu.Lock()
			c.subscriptions[payload.JobID] = true
			c.mu.Unlock()
			c.hub.logger.Debug("Client subscribed to job", zap.String("job_id", payload.JobID))
		}

	case "unsubscribe":
		var payload struct {
			JobID string `json:"jobId"`
		}
		if err := json.Unmarshal(msg.Payload, &payload); err == nil {
			c.mu.Lock()
			delete(c.subscriptions, payload.JobID)
			c.mu.Unlock()
			c.hub.logger.Debug("Client unsubscribed from job", zap.String("job_id", payload.JobID))
		}

	case "ping":
		response, _ := json.Marshal(Message{Type: "pong"})
		c.send <- response
	}
}
