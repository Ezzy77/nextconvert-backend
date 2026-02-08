-- Migration: Add subscription system
-- Adds user_profiles, subscriptions, and usage tracking

-- Subscription tier enum (separate from legacy user_tier)
CREATE TYPE subscription_tier AS ENUM ('free', 'basic', 'standard', 'pro');

-- Subscription status for Stripe
CREATE TYPE subscription_status AS ENUM ('active', 'cancelled', 'past_due', 'trialing');

-- User profiles: Clerk user_id -> tier, usage, Stripe customer
CREATE TABLE IF NOT EXISTS user_profiles (
    user_id TEXT PRIMARY KEY,
    tier subscription_tier DEFAULT 'free',
    stripe_customer_id TEXT,
    conversion_minutes_used INT DEFAULT 0,
    usage_period_start TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- Subscriptions: links Stripe subscription to user
CREATE TABLE IF NOT EXISTS subscriptions (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id TEXT NOT NULL REFERENCES user_profiles(user_id) ON DELETE CASCADE,
    tier subscription_tier NOT NULL,
    stripe_subscription_id TEXT UNIQUE,
    stripe_customer_id TEXT,
    status subscription_status NOT NULL DEFAULT 'active',
    current_period_start TIMESTAMP WITH TIME ZONE,
    current_period_end TIMESTAMP WITH TIME ZONE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- Add conversion_minutes and input_duration_seconds to jobs
ALTER TABLE jobs ADD COLUMN IF NOT EXISTS input_duration_seconds NUMERIC DEFAULT 0;
ALTER TABLE jobs ADD COLUMN IF NOT EXISTS conversion_minutes INT DEFAULT 0;

-- Indexes
CREATE INDEX IF NOT EXISTS idx_subscriptions_user_id ON subscriptions(user_id);
CREATE INDEX IF NOT EXISTS idx_subscriptions_stripe_id ON subscriptions(stripe_subscription_id);
