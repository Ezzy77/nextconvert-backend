package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/convert-studio/backend/internal/modules/subscription"
	"github.com/stripe/stripe-go/v81"
	portalsession "github.com/stripe/stripe-go/v81/billingportal/session"
	"github.com/stripe/stripe-go/v81/checkout/session"
	"github.com/stripe/stripe-go/v81/customer"
	stripesub "github.com/stripe/stripe-go/v81/subscription"
	"github.com/stripe/stripe-go/v81/webhook"
	"go.uber.org/zap"
)

// StripeHandler handles Stripe checkout and webhooks
type StripeHandler struct {
	subService    *subscription.Service
	secretKey     string
	webhookSecret string
	priceIDs      map[string]string
	successURL    string
	cancelURL     string
	logger        *zap.Logger
}

// NewStripeHandler creates a new Stripe handler
func NewStripeHandler(subService *subscription.Service, secretKey, webhookSecret string, priceIDs map[string]string, successURL, cancelURL string, logger *zap.Logger) *StripeHandler {
	return &StripeHandler{
		subService:    subService,
		secretKey:     secretKey,
		webhookSecret: webhookSecret,
		priceIDs:      priceIDs,
		successURL:    successURL,
		cancelURL:     cancelURL,
		logger:        logger,
	}
}

// CreateCheckoutRequest is the request body for checkout
type CreateCheckoutRequest struct {
	Tier string `json:"tier"` // basic, standard, pro
}

// CreateCheckoutResponse returns the Stripe Checkout URL
type CreateCheckoutResponse struct {
	URL string `json:"url"`
}

// CreateCheckoutSession creates a Stripe Checkout session and returns the URL
func (h *StripeHandler) CreateCheckoutSession(w http.ResponseWriter, r *http.Request) {
	if h.secretKey == "" {
		http.Error(w, "Stripe not configured", http.StatusServiceUnavailable)
		return
	}

	var req CreateCheckoutRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	priceID, ok := h.priceIDs[req.Tier]
	if !ok {
		http.Error(w, "invalid tier", http.StatusBadRequest)
		return
	}

	userID := r.Header.Get("X-User-ID")
	if userID == "" {
		http.Error(w, "user not found", http.StatusUnauthorized)
		return
	}

	// Check if user already has an active subscription for this tier
	hasActiveSub, err := h.subService.HasActiveSubscription(r.Context(), userID, req.Tier)
	if err != nil {
		h.logger.Error("Failed to check active subscription", zap.Error(err))
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if hasActiveSub {
		h.logger.Info("User attempted to subscribe to tier they already have",
			zap.String("user_id", userID),
			zap.String("tier", req.Tier))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(map[string]string{
			"error": fmt.Sprintf("You already have an active %s subscription", req.Tier),
		})
		return
	}

	stripe.Key = h.secretKey

	// Get or create Stripe customer
	prof, _ := h.subService.GetUserSubscription(r.Context(), userID)
	var custID string
	if prof != nil && prof.StripeCustomerID != "" {
		custID = prof.StripeCustomerID
	} else {
		params := &stripe.CustomerParams{}
		params.AddMetadata("clerk_user_id", userID)
		c, err := customer.New(params)
		if err != nil {
			h.logger.Error("Failed to create Stripe customer", zap.Error(err))
			http.Error(w, "failed to create customer", http.StatusInternalServerError)
			return
		}
		custID = c.ID
		// Store stripe_customer_id in user_profiles (will be done on checkout complete)
	}

	params := &stripe.CheckoutSessionParams{
		Customer: stripe.String(custID),
		Mode:     stripe.String(string(stripe.CheckoutSessionModeSubscription)),
		LineItems: []*stripe.CheckoutSessionLineItemParams{
			{
				Price:    stripe.String(priceID),
				Quantity: stripe.Int64(1),
			},
		},
		SuccessURL: stripe.String(h.successURL),
		CancelURL:  stripe.String(h.cancelURL),
		Metadata: map[string]string{
			"clerk_user_id": userID,
			"tier":          req.Tier,
		},
		SubscriptionData: &stripe.CheckoutSessionSubscriptionDataParams{
			TrialPeriodDays: stripe.Int64(60), // Get 2 months free
			Metadata: map[string]string{
				"clerk_user_id": userID,
				"tier":          req.Tier,
			},
		},
	}

	sess, err := session.New(params)
	if err != nil {
		h.logger.Error("Failed to create checkout session", zap.Error(err))
		http.Error(w, "failed to create checkout session", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(CreateCheckoutResponse{URL: sess.URL})
}

// CreatePortalSessionRequest for customer portal
type CreatePortalSessionRequest struct {
	ReturnURL string `json:"returnUrl"`
}

// CreatePortalSession creates a Stripe Customer Portal session
func (h *StripeHandler) CreatePortalSession(w http.ResponseWriter, r *http.Request) {
	if h.secretKey == "" {
		http.Error(w, "Stripe not configured", http.StatusServiceUnavailable)
		return
	}

	userID := r.Header.Get("X-User-ID")
	if userID == "" {
		http.Error(w, "user not found", http.StatusUnauthorized)
		return
	}

	prof, err := h.subService.GetUserSubscription(r.Context(), userID)
	if err != nil || prof.StripeCustomerID == "" {
		http.Error(w, "no subscription found", http.StatusBadRequest)
		return
	}

	stripe.Key = h.secretKey

	returnURL := h.cancelURL
	var req CreatePortalSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err == nil && req.ReturnURL != "" {
		returnURL = req.ReturnURL
	}

	params := &stripe.BillingPortalSessionParams{
		Customer:  stripe.String(prof.StripeCustomerID),
		ReturnURL: stripe.String(returnURL),
	}

	sess, err := portalsession.New(params)
	if err != nil {
		h.logger.Error("Failed to create portal session", zap.Error(err))
		http.Error(w, "failed to create portal session", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"url": sess.URL})
}

// HandleWebhook processes Stripe webhook events
func (h *StripeHandler) HandleWebhook(w http.ResponseWriter, r *http.Request) {
	if h.webhookSecret == "" {
		h.logger.Warn("Stripe webhook secret not configured")
		http.Error(w, "webhook not configured", http.StatusServiceUnavailable)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		h.logger.Error("Failed to read webhook body", zap.Error(err))
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}

	event, err := webhook.ConstructEventWithOptions(
		body,
		r.Header.Get("Stripe-Signature"),
		h.webhookSecret,
		webhook.ConstructEventOptions{
			IgnoreAPIVersionMismatch: true, // Allow Stripe CLI with different API versions
		},
	)
	if err != nil {
		h.logger.Error("Webhook signature verification failed", zap.Error(err))
		http.Error(w, "invalid signature", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	switch event.Type {
	case "checkout.session.completed":
		h.handleCheckoutCompleted(ctx, event)
	case "customer.subscription.created":
		h.handleSubscriptionCreated(ctx, event)
	case "customer.subscription.updated":
		h.handleSubscriptionUpdated(ctx, event)
	case "customer.subscription.deleted":
		h.handleSubscriptionDeleted(ctx, event)
	case "invoice.paid":
		h.handleInvoicePaid(ctx, event)
	default:
		h.logger.Debug("Unhandled webhook event", zap.String("type", string(event.Type)))
	}

	w.WriteHeader(http.StatusOK)
}

func (h *StripeHandler) handleCheckoutCompleted(ctx context.Context, event stripe.Event) {
	var sess stripe.CheckoutSession
	if err := json.Unmarshal(event.Data.Raw, &sess); err != nil {
		h.logger.Error("Failed to parse checkout session", zap.Error(err))
		return
	}
	userID := sess.Metadata["clerk_user_id"]
	tier := sess.Metadata["tier"]
	if userID == "" || tier == "" {
		h.logger.Warn("Checkout session missing metadata", zap.String("session_id", sess.ID))
		return
	}
	// Extract customer and subscription IDs - Stripe sends as strings in webhook when not expanded
	custID := getCustomerID(&sess)
	if custID == "" {
		custID = extractStringFromRaw(event.Data.Raw, "customer")
	}
	var periodStart, periodEnd *time.Time
	subID := getSubscriptionID(&sess)
	if subID == "" {
		subID = extractStringFromRaw(event.Data.Raw, "subscription")
	}
	var subStatus string
	if subID != "" {
		sub, err := stripesub.Get(subID, nil)
		if err == nil {
			ps := time.Unix(sub.CurrentPeriodStart, 0)
			pe := time.Unix(sub.CurrentPeriodEnd, 0)
			periodStart = &ps
			periodEnd = &pe
			subStatus = mapStripeStatusToDB(sub.Status)
		} else {
			h.logger.Warn("Failed to get subscription for period", zap.String("sub_id", subID), zap.Error(err))
			subStatus = "active"
		}
	}
	if err := h.subService.UpdateUserTier(ctx, userID, tier, strPtr(custID), periodStart, periodEnd); err != nil {
		h.logger.Error("Failed to update user tier", zap.Error(err))
		return
	}
	if subID != "" && periodStart != nil && periodEnd != nil {
		if subStatus == "" {
			subStatus = "active"
		}
		_ = h.subService.UpsertSubscription(ctx, userID, tier, subID, custID, subStatus, periodStart, periodEnd)
	}
	h.logger.Info("Updated user tier from checkout", zap.String("user_id", userID), zap.String("tier", tier))
}

// extractStringFromRaw extracts a string field from raw JSON (for Stripe IDs when not expanded)
func extractStringFromRaw(raw []byte, key string) string {
	var m map[string]interface{}
	if json.Unmarshal(raw, &m) != nil {
		return ""
	}
	v, ok := m[key]
	if !ok || v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	if obj, ok := v.(map[string]interface{}); ok {
		if id, ok := obj["id"].(string); ok {
			return id
		}
	}
	return ""
}

func getCustomerID(sess *stripe.CheckoutSession) string {
	if sess.Customer != nil {
		return sess.Customer.ID
	}
	return ""
}

func getSubscriptionID(sess *stripe.CheckoutSession) string {
	if sess.Subscription != nil {
		return sess.Subscription.ID
	}
	return ""
}

func (h *StripeHandler) handleSubscriptionCreated(ctx context.Context, event stripe.Event) {
	h.handleSubscriptionUpdated(ctx, event)
}

func (h *StripeHandler) handleSubscriptionUpdated(ctx context.Context, event stripe.Event) {
	var sub stripe.Subscription
	if err := json.Unmarshal(event.Data.Raw, &sub); err != nil {
		h.logger.Error("Failed to parse subscription", zap.Error(err))
		return
	}
	userID := sub.Metadata["clerk_user_id"]
	tier := sub.Metadata["tier"]
	// Fallback: look up user by stripe_customer_id when metadata is missing
	if userID == "" {
		custID := ""
		if sub.Customer != nil {
			custID = sub.Customer.ID
		}
		if custID == "" {
			custID = extractStringFromRaw(event.Data.Raw, "customer")
		}
		if custID != "" {
			var err error
			userID, err = h.subService.GetUserIDByStripeCustomerID(ctx, custID)
			if err != nil || userID == "" {
				h.logger.Warn("Could not resolve user from subscription", zap.String("customer_id", custID))
				return
			}
		}
	}
	if userID == "" {
		return
	}
	if tier == "" {
		tier = "basic" // Default if missing from metadata
	}
	if sub.Status != stripe.SubscriptionStatusActive && sub.Status != stripe.SubscriptionStatusTrialing {
		h.subService.SetFreeTier(ctx, userID)
		return
	}
	ps := time.Unix(sub.CurrentPeriodStart, 0)
	pe := time.Unix(sub.CurrentPeriodEnd, 0)
	custID := ""
	if sub.Customer != nil {
		custID = sub.Customer.ID
	}
	if custID == "" {
		custID = extractStringFromRaw(event.Data.Raw, "customer")
	}
	if err := h.subService.UpdateUserTier(ctx, userID, tier, strPtr(custID), &ps, &pe); err != nil {
		h.logger.Error("Failed to update user tier", zap.Error(err))
		return
	}
	dbStatus := mapStripeStatusToDB(sub.Status)
	if err := h.subService.UpsertSubscription(ctx, userID, tier, sub.ID, custID, dbStatus, &ps, &pe); err != nil {
		h.logger.Warn("Failed to upsert subscription record", zap.Error(err))
	}
	h.logger.Info("Updated user tier from subscription", zap.String("user_id", userID), zap.String("tier", tier))
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func mapStripeStatusToDB(status stripe.SubscriptionStatus) string {
	switch status {
	case stripe.SubscriptionStatusActive:
		return "active"
	case stripe.SubscriptionStatusTrialing:
		return "trialing"
	case stripe.SubscriptionStatusPastDue:
		return "past_due"
	case stripe.SubscriptionStatusCanceled:
		return "cancelled"
	default:
		return "active"
	}
}

func (h *StripeHandler) handleSubscriptionDeleted(ctx context.Context, event stripe.Event) {
	var sub stripe.Subscription
	if err := json.Unmarshal(event.Data.Raw, &sub); err != nil {
		h.logger.Error("Failed to parse subscription", zap.Error(err))
		return
	}
	userID := sub.Metadata["clerk_user_id"]
	if userID == "" {
		custID := ""
		if sub.Customer != nil {
			custID = sub.Customer.ID
		}
		if custID == "" {
			custID = extractStringFromRaw(event.Data.Raw, "customer")
		}
		if custID != "" {
			var err error
			userID, err = h.subService.GetUserIDByStripeCustomerID(ctx, custID)
			if err != nil {
				h.logger.Warn("Could not resolve user for subscription deleted", zap.String("customer_id", custID))
			}
		}
	}
	if userID != "" {
		if err := h.subService.SetFreeTier(ctx, userID); err != nil {
			h.logger.Error("Failed to set free tier", zap.Error(err))
		}
		h.logger.Info("Set user to free tier after subscription deleted", zap.String("user_id", userID))
	}
}

func (h *StripeHandler) handleInvoicePaid(ctx context.Context, event stripe.Event) {
	// Could reset usage when new billing period starts - handled in subscription.updated
}
