package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/nextconvert/backend/internal/api/middleware"
	"github.com/nextconvert/backend/internal/modules/subscription"
	"go.uber.org/zap"
)

// SubscriptionHandler handles subscription-related endpoints
type SubscriptionHandler struct {
	subService *subscription.Service
	stripe     *StripeHandler
	logger     *zap.Logger
}

// NewSubscriptionHandler creates a new subscription handler
func NewSubscriptionHandler(subService *subscription.Service, stripe *StripeHandler, logger *zap.Logger) *SubscriptionHandler {
	return &SubscriptionHandler{
		subService: subService,
		stripe:     stripe,
		logger:     logger,
	}
}

// GetMe returns the current user's subscription and usage
func (h *SubscriptionHandler) GetMe(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r.Context())
	userID := "anonymous"
	if user != nil && user.ID != "anonymous" {
		userID = user.ID
	}

	sub, err := h.subService.GetOrCreateUserProfile(r.Context(), userID)
	if err != nil {
		h.logger.Error("Failed to get subscription", zap.Error(err))
		http.Error(w, "failed to get subscription", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(sub)
}

// CreateCheckout delegates to Stripe handler
func (h *SubscriptionHandler) CreateCheckout(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r.Context())
	if user == nil || user.ID == "anonymous" {
		http.Error(w, "authentication required", http.StatusUnauthorized)
		return
	}
	// Set X-User-ID for Stripe handler
	r.Header.Set("X-User-ID", user.ID)
	h.stripe.CreateCheckoutSession(w, r)
}

// CreatePortal delegates to Stripe handler
func (h *SubscriptionHandler) CreatePortal(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r.Context())
	if user == nil || user.ID == "anonymous" {
		http.Error(w, "authentication required", http.StatusUnauthorized)
		return
	}
	r.Header.Set("X-User-ID", user.ID)
	h.stripe.CreatePortalSession(w, r)
}
