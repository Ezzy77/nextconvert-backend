package subscription

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/nextconvert/backend/internal/shared/database"
)

// UserSubscription represents a user's subscription and usage
type UserSubscription struct {
	UserID                 string    `json:"userId"`
	Tier                   string    `json:"tier"`
	ConversionMinutesUsed  int       `json:"conversionMinutesUsed"`
	ConversionMinutesLimit int       `json:"conversionMinutesLimit"`
	MaxFileSizeBytes       int64     `json:"maxFileSizeBytes"`
	PeriodStart            time.Time `json:"periodStart"`
	PeriodEnd              time.Time `json:"periodEnd"`
	StripeCustomerID       string    `json:"stripeCustomerId,omitempty"`
}

// Service handles subscription and usage logic
type Service struct {
	db *database.Postgres
}

// NewService creates a new subscription service
func NewService(db *database.Postgres) *Service {
	return &Service{db: db}
}

// GetOrCreateUserProfile ensures a user profile exists and returns it
func (s *Service) GetOrCreateUserProfile(ctx context.Context, userID string) (*UserSubscription, error) {
	sub, err := s.GetUserSubscription(ctx, userID)
	if err == nil {
		return sub, nil
	}
	// Create profile if not exists
	_, err = s.db.Pool.Exec(ctx, `
		INSERT INTO user_profiles (user_id, tier, usage_period_start, conversion_minutes_used)
		VALUES ($1, 'free', date_trunc('month', NOW()), 0)
		ON CONFLICT (user_id) DO NOTHING
	`, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to create user profile: %w", err)
	}
	return s.GetUserSubscription(ctx, userID)
}

// GetUserSubscription returns the user's subscription and usage
func (s *Service) GetUserSubscription(ctx context.Context, userID string) (*UserSubscription, error) {
	var tier string
	var convUsed int
	var periodStart time.Time
	var stripeCustomerID *string

	err := s.db.Pool.QueryRow(ctx, `
		SELECT tier, conversion_minutes_used, COALESCE(usage_period_start, date_trunc('month', NOW())), stripe_customer_id
		FROM user_profiles
		WHERE user_id = $1
	`, userID).Scan(&tier, &convUsed, &periodStart, &stripeCustomerID)
	if err != nil {
		return nil, err
	}

	limits := GetTierLimits(tier)
	periodEnd := periodStart.AddDate(0, 1, 0) // 1 month default for free tier

	// For paid tiers, get period from active subscription (ignore ErrNoRows)
	var subPeriodEnd *time.Time
	err = s.db.Pool.QueryRow(ctx, `
		SELECT current_period_end FROM subscriptions
		WHERE user_id = $1 AND status IN ('active', 'trialing')
		ORDER BY created_at DESC LIMIT 1
	`, userID).Scan(&subPeriodEnd)
	if err == nil && subPeriodEnd != nil {
		periodEnd = *subPeriodEnd
	}
	// err from subscriptions query (e.g. ErrNoRows) is expected for free users - ignore

	sub := &UserSubscription{
		UserID:                 userID,
		Tier:                   tier,
		ConversionMinutesUsed:  convUsed,
		ConversionMinutesLimit: limits.ConversionMinutes,
		MaxFileSizeBytes:       limits.MaxFileSizeBytes,
		PeriodStart:            periodStart,
		PeriodEnd:              periodEnd,
	}
	if stripeCustomerID != nil {
		sub.StripeCustomerID = *stripeCustomerID
	}
	return sub, nil
}

// CheckLimit validates if the user can perform an action (file size or conversion minutes)
func (s *Service) CheckLimit(ctx context.Context, userID string, limitType string, value int64) error {
	sub, err := s.GetOrCreateUserProfile(ctx, userID)
	if err != nil {
		return err
	}
	limits := GetTierLimits(sub.Tier)

	switch limitType {
	case "file_size":
		if value > limits.MaxFileSizeBytes {
			return fmt.Errorf("file size %d exceeds limit %d for tier %s", value, limits.MaxFileSizeBytes, sub.Tier)
		}
		return nil
	case "conversion_minutes":
		if sub.ConversionMinutesUsed+int(value) > limits.ConversionMinutes {
			return fmt.Errorf("conversion minutes limit exceeded: %d/%d used", sub.ConversionMinutesUsed, limits.ConversionMinutes)
		}
		return nil
	default:
		return fmt.Errorf("unknown limit type: %s", limitType)
	}
}

// RecordConversionMinutes increments usage after job completion
func (s *Service) RecordConversionMinutes(ctx context.Context, userID string, minutes int) error {
	if minutes <= 0 {
		return nil
	}
	_, err := s.db.Pool.Exec(ctx, `
		UPDATE user_profiles
		SET conversion_minutes_used = conversion_minutes_used + $1,
		    updated_at = NOW()
		WHERE user_id = $2
	`, minutes, userID)
	return err
}

// ResetUsageForNewPeriod resets conversion_minutes_used when billing period starts
func (s *Service) ResetUsageForNewPeriod(ctx context.Context, userID string, periodStart time.Time) error {
	_, err := s.db.Pool.Exec(ctx, `
		UPDATE user_profiles
		SET conversion_minutes_used = 0,
		    usage_period_start = $1,
		    updated_at = NOW()
		WHERE user_id = $2
	`, periodStart, userID)
	return err
}

// UpdateUserTier sets the user's tier (from Stripe webhook)
func (s *Service) UpdateUserTier(ctx context.Context, userID, tier string, stripeCustomerID *string, periodStart, periodEnd *time.Time) error {
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	_, err = tx.Exec(ctx, `
		INSERT INTO user_profiles (user_id, tier, stripe_customer_id, conversion_minutes_used, usage_period_start, updated_at)
		VALUES ($1, $2, $3, 0, COALESCE($4, NOW()), NOW())
		ON CONFLICT (user_id) DO UPDATE SET
			tier = EXCLUDED.tier,
			stripe_customer_id = COALESCE(EXCLUDED.stripe_customer_id, user_profiles.stripe_customer_id),
			conversion_minutes_used = CASE
				WHEN $4 IS NOT NULL AND (user_profiles.usage_period_start IS NULL OR user_profiles.usage_period_start < $4) THEN 0
				ELSE user_profiles.conversion_minutes_used
			END,
			usage_period_start = COALESCE($4, user_profiles.usage_period_start),
			updated_at = NOW()
	`, userID, tier, stripeCustomerID, periodStart)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// UpsertSubscription stores or updates a subscription record (from Stripe webhook)
func (s *Service) UpsertSubscription(ctx context.Context, userID, tier, stripeSubID, stripeCustomerID string, status string, periodStart, periodEnd *time.Time) error {
	_, err := s.db.Pool.Exec(ctx, `
		INSERT INTO subscriptions (user_id, tier, stripe_subscription_id, stripe_customer_id, status, current_period_start, current_period_end, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, NOW())
		ON CONFLICT (stripe_subscription_id) DO UPDATE SET
			user_id = EXCLUDED.user_id,
			tier = EXCLUDED.tier,
			status = EXCLUDED.status,
			current_period_start = EXCLUDED.current_period_start,
			current_period_end = EXCLUDED.current_period_end,
			updated_at = NOW()
	`, userID, tier, stripeSubID, stripeCustomerID, status, periodStart, periodEnd)
	return err
}

// GetUserIDByStripeCustomerID looks up user_id from stripe_customer_id (for webhook fallback)
func (s *Service) GetUserIDByStripeCustomerID(ctx context.Context, stripeCustomerID string) (string, error) {
	var userID string
	err := s.db.Pool.QueryRow(ctx, `SELECT user_id FROM user_profiles WHERE stripe_customer_id = $1`, stripeCustomerID).Scan(&userID)
	return userID, err
}

// SetFreeTier clears subscription and sets user to free tier (on cancel/past_due)
func (s *Service) SetFreeTier(ctx context.Context, userID string) error {
	_, err := s.db.Pool.Exec(ctx, `
		INSERT INTO user_profiles (user_id, tier, conversion_minutes_used, usage_period_start, updated_at)
		VALUES ($1, 'free', 0, date_trunc('month', NOW()), NOW())
		ON CONFLICT (user_id) DO UPDATE SET
			tier = 'free',
			updated_at = NOW()
	`, userID)
	return err
}

// HasActiveSubscription checks if a user already has an active subscription for a specific tier
func (s *Service) HasActiveSubscription(ctx context.Context, userID, tier string) (bool, error) {
	var count int
	err := s.db.Pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM subscriptions
		WHERE user_id = $1 
		  AND tier = $2 
		  AND status IN ('active', 'trialing')
	`, userID, tier).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// GetTier returns the user's tier (for auth middleware). Returns "free" if not found.
func (s *Service) GetTier(ctx context.Context, userID string) string {
	sub, err := s.GetUserSubscription(ctx, userID)
	if err != nil {
		return "free"
	}
	if sub != nil && sub.Tier != "" {
		return sub.Tier
	}
	return "free"
}

// ConversionMinutesFromDuration computes conversion minutes from duration in seconds
func ConversionMinutesFromDuration(seconds float64) int {
	if seconds <= 0 {
		return 1 // minimum 1 min for audio/image
	}
	return int(math.Ceil(seconds / 60))
}
