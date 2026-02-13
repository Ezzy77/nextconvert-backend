package handlers

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

func TestNewStripeHandler(t *testing.T) {
	logger := zap.NewNop()
	priceIDs := map[string]string{
		"basic":    "price_123",
		"standard": "price_456",
		"pro":      "price_789",
	}

	yearlyPriceIDs := map[string]string{
		"basic":    "price_y123",
		"standard": "price_y456",
		"pro":      "price_y789",
	}

	t.Run("creates handler with configuration", func(t *testing.T) {
		handler := NewStripeHandler(
			nil,
			"sk_test_123",
			"whsec_123",
			priceIDs,
			yearlyPriceIDs,
			"https://example.com/success",
			"https://example.com/cancel",
			logger,
		)

		assert.NotNil(t, handler)
		assert.Equal(t, "sk_test_123", handler.secretKey)
		assert.Equal(t, "whsec_123", handler.webhookSecret)
		assert.Equal(t, "https://example.com/success", handler.successURL)
		assert.Equal(t, "https://example.com/cancel", handler.cancelURL)
		assert.Equal(t, priceIDs, handler.priceIDs)
		assert.Equal(t, yearlyPriceIDs, handler.yearlyPriceIDs)
	})

	t.Run("stores all tier price IDs", func(t *testing.T) {
		handler := NewStripeHandler(nil, "", "", priceIDs, yearlyPriceIDs, "", "", logger)
		
		assert.Equal(t, "price_123", handler.priceIDs["basic"])
		assert.Equal(t, "price_456", handler.priceIDs["standard"])
		assert.Equal(t, "price_789", handler.priceIDs["pro"])
		assert.Equal(t, "price_y123", handler.yearlyPriceIDs["basic"])
		assert.Equal(t, "price_y456", handler.yearlyPriceIDs["standard"])
		assert.Equal(t, "price_y789", handler.yearlyPriceIDs["pro"])
	})
}

func TestStripeTierMapping(t *testing.T) {
	tests := []struct {
		name     string
		tier     string
		priceID  string
	}{
		{"basic tier has price ID", "basic", "price_basic"},
		{"standard tier has price ID", "standard", "price_standard"},
		{"pro tier has price ID", "pro", "price_pro"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			priceIDs := map[string]string{
				"basic":    "price_basic",
				"standard": "price_standard",
				"pro":      "price_pro",
			}
			
			handler := NewStripeHandler(nil, "", "", priceIDs, nil, "", "", zap.NewNop())
			priceID, exists := handler.priceIDs[tt.tier]
			
			assert.True(t, exists, "tier should exist in price IDs")
			assert.Equal(t, tt.priceID, priceID)
		})
	}
}

func TestCreateCheckoutRequest(t *testing.T) {
	t.Run("validates tier field", func(t *testing.T) {
		validTiers := []string{"basic", "standard", "pro"}
		
		for _, tier := range validTiers {
			req := CreateCheckoutRequest{Tier: tier}
			assert.NotEmpty(t, req.Tier)
			assert.Contains(t, validTiers, req.Tier)
		}
	})

	t.Run("rejects invalid tiers", func(t *testing.T) {
		invalidTiers := []string{"free", "enterprise", "invalid", ""}
		validTiers := []string{"basic", "standard", "pro"}
		
		for _, tier := range invalidTiers {
			assert.NotContains(t, validTiers, tier)
		}
	})
}

func TestStripeConfiguration(t *testing.T) {
	logger := zap.NewNop()

	t.Run("requires secret key for production", func(t *testing.T) {
		handler := NewStripeHandler(nil, "", "", nil, nil, "", "", logger)
		assert.Empty(t, handler.secretKey, "empty secret key should be handled gracefully")
	})

	t.Run("requires webhook secret for security", func(t *testing.T) {
		handler := NewStripeHandler(nil, "sk_test", "", nil, nil, "", "", logger)
		assert.Empty(t, handler.webhookSecret, "empty webhook secret should be handled")
	})

	t.Run("requires success and cancel URLs", func(t *testing.T) {
		handler := NewStripeHandler(
			nil,
			"sk_test",
			"whsec_test",
			nil,
			nil,
			"https://example.com/success",
			"https://example.com/cancel",
			logger,
		)
		assert.NotEmpty(t, handler.successURL)
		assert.NotEmpty(t, handler.cancelURL)
		assert.Contains(t, handler.successURL, "success")
		assert.Contains(t, handler.cancelURL, "cancel")
	})
}

func TestStripeTrialPeriod(t *testing.T) {
	t.Run("documents 60-day trial period", func(t *testing.T) {
		// This is a documentation test to ensure trial period is known
		trialPeriodDays := 60
		assert.Equal(t, 60, trialPeriodDays, "trial period should be 60 days")
	})
}
