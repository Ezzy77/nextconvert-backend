-- ============================================================================
-- NextConvert Production Database Schema
-- ============================================================================
-- This script creates all tables, indexes, and initial data for production
-- Safe to run multiple times (idempotent)
-- Compatible with: PostgreSQL 12+, Supabase, AWS RDS, etc.
-- ============================================================================

-- Enable required extensions
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- ============================================================================
-- ENUM TYPES
-- ============================================================================

-- Legacy user tier (kept for backward compatibility)
DO $$ BEGIN
    CREATE TYPE user_tier AS ENUM ('free', 'pro', 'enterprise');
EXCEPTION
    WHEN duplicate_object THEN null;
END $$;

-- Subscription tier (new subscription system)
DO $$ BEGIN
    CREATE TYPE subscription_tier AS ENUM ('free', 'basic', 'standard', 'pro');
EXCEPTION
    WHEN duplicate_object THEN null;
END $$;

-- Subscription status (Stripe integration)
DO $$ BEGIN
    CREATE TYPE subscription_status AS ENUM ('active', 'cancelled', 'past_due', 'trialing');
EXCEPTION
    WHEN duplicate_object THEN null;
END $$;

-- File storage zones
DO $$ BEGIN
    CREATE TYPE file_zone AS ENUM ('upload', 'working', 'output');
EXCEPTION
    WHEN duplicate_object THEN null;
END $$;

-- Job status
DO $$ BEGIN
    CREATE TYPE job_status AS ENUM ('pending', 'queued', 'processing', 'completed', 'failed', 'cancelled');
EXCEPTION
    WHEN duplicate_object THEN null;
END $$;

-- Media type
DO $$ BEGIN
    CREATE TYPE media_type AS ENUM ('video', 'audio', 'image');
EXCEPTION
    WHEN duplicate_object THEN null;
END $$;

-- ============================================================================
-- TABLES
-- ============================================================================

-- ----------------------------------------------------------------------------
-- Users Table (Legacy - kept for potential future use)
-- Note: Authentication is handled by Clerk, but this table can store additional user data
-- ----------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS users (
    id TEXT PRIMARY KEY,
    email VARCHAR(255) UNIQUE,
    password_hash TEXT,
    tier user_tier DEFAULT 'free',
    quota_bytes BIGINT DEFAULT 5368709120, -- 5GB
    quota_used BIGINT DEFAULT 0,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- ----------------------------------------------------------------------------
-- User Profiles Table (Primary user management for subscriptions)
-- Links Clerk user_id to subscription tier and usage tracking
-- ----------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS user_profiles (
    user_id TEXT PRIMARY KEY,
    tier subscription_tier DEFAULT 'free',
    stripe_customer_id TEXT,
    conversion_minutes_used INT DEFAULT 0,
    usage_period_start TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- ----------------------------------------------------------------------------
-- Subscriptions Table (Stripe subscription management)
-- Tracks active, cancelled, and past_due subscriptions
-- ----------------------------------------------------------------------------
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

-- ----------------------------------------------------------------------------
-- Files Table (Uploaded and processed media files)
-- Stores metadata for all files in the system
-- ----------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS files (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id TEXT, -- NULL allowed for anonymous uploads
    original_name TEXT NOT NULL,
    storage_path TEXT NOT NULL,
    mime_type VARCHAR(255),
    size_bytes BIGINT NOT NULL,
    zone file_zone NOT NULL,
    media_type media_type,
    metadata JSONB DEFAULT '{}',
    checksum VARCHAR(64),
    expires_at TIMESTAMP WITH TIME ZONE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- ----------------------------------------------------------------------------
-- Jobs Table (Media processing jobs)
-- Tracks conversion, compression, and other media operations
-- ----------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS jobs (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id TEXT, -- NULL allowed for anonymous jobs
    status job_status DEFAULT 'pending',
    priority INT DEFAULT 5,
    input_file_id UUID REFERENCES files(id),
    output_file_id UUID REFERENCES files(id),
    output_format VARCHAR(20),
    output_file_name TEXT,
    operations JSONB DEFAULT '[]',
    progress JSONB DEFAULT '{"percent": 0}',
    error JSONB,
    retry_count INT DEFAULT 0,
    input_duration_seconds NUMERIC DEFAULT 0,
    conversion_minutes INT DEFAULT 0,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    started_at TIMESTAMP WITH TIME ZONE,
    completed_at TIMESTAMP WITH TIME ZONE
);

-- ----------------------------------------------------------------------------
-- Presets Table (Media conversion presets)
-- System and user-defined presets for common operations
-- ----------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS presets (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    name VARCHAR(100) NOT NULL,
    media_type media_type NOT NULL,
    description TEXT,
    operations JSONB NOT NULL,
    is_system BOOLEAN DEFAULT FALSE,
    user_id TEXT,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- ============================================================================
-- INDEXES
-- ============================================================================

-- User Profiles indexes
CREATE INDEX IF NOT EXISTS idx_user_profiles_stripe_customer ON user_profiles(stripe_customer_id);

-- Subscriptions indexes
CREATE INDEX IF NOT EXISTS idx_subscriptions_user_id ON subscriptions(user_id);
CREATE INDEX IF NOT EXISTS idx_subscriptions_stripe_id ON subscriptions(stripe_subscription_id);
CREATE INDEX IF NOT EXISTS idx_subscriptions_status ON subscriptions(status);

-- Files indexes
CREATE INDEX IF NOT EXISTS idx_files_user_id ON files(user_id);
CREATE INDEX IF NOT EXISTS idx_files_zone ON files(zone);
CREATE INDEX IF NOT EXISTS idx_files_media_type ON files(media_type);
CREATE INDEX IF NOT EXISTS idx_files_expires_at ON files(expires_at) WHERE expires_at IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_files_created_at ON files(created_at DESC);

-- Jobs indexes
CREATE INDEX IF NOT EXISTS idx_jobs_user_id ON jobs(user_id);
CREATE INDEX IF NOT EXISTS idx_jobs_status ON jobs(status);
CREATE INDEX IF NOT EXISTS idx_jobs_created_at ON jobs(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_jobs_input_file ON jobs(input_file_id);
CREATE INDEX IF NOT EXISTS idx_jobs_output_file ON jobs(output_file_id);

-- Presets indexes
CREATE INDEX IF NOT EXISTS idx_presets_user_id ON presets(user_id);
CREATE INDEX IF NOT EXISTS idx_presets_media_type ON presets(media_type);
CREATE INDEX IF NOT EXISTS idx_presets_is_system ON presets(is_system);

-- ============================================================================
-- FUNCTIONS & TRIGGERS
-- ============================================================================

-- Function to automatically update updated_at timestamp
CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Trigger for users table
DROP TRIGGER IF EXISTS update_users_updated_at ON users;
CREATE TRIGGER update_users_updated_at
    BEFORE UPDATE ON users
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

-- Trigger for user_profiles table
DROP TRIGGER IF EXISTS update_user_profiles_updated_at ON user_profiles;
CREATE TRIGGER update_user_profiles_updated_at
    BEFORE UPDATE ON user_profiles
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

-- Trigger for subscriptions table
DROP TRIGGER IF EXISTS update_subscriptions_updated_at ON subscriptions;
CREATE TRIGGER update_subscriptions_updated_at
    BEFORE UPDATE ON subscriptions
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

-- ============================================================================
-- DEFAULT DATA
-- ============================================================================

-- Insert system presets (only if they don't exist)
INSERT INTO presets (id, name, media_type, description, operations, is_system, user_id)
SELECT * FROM (VALUES
    (
        'a1b2c3d4-e5f6-4a5b-8c9d-0e1f2a3b4c5d'::uuid,
        'Mobile Optimized',
        'video'::media_type,
        'Optimized for mobile devices (720p, H.264)',
        '[{"type":"resize","params":{"width":1280,"height":720,"maintainAspect":true}},{"type":"compress","params":{"quality":70}},{"type":"convertFormat","params":{"targetFormat":"mp4","codec":"h264"}}]'::jsonb,
        true,
        NULL
    ),
    (
        'b2c3d4e5-f6a7-4b5c-9d0e-1f2a3b4c5d6e'::uuid,
        'Web Optimized',
        'video'::media_type,
        'Optimized for web streaming (1080p, WebM)',
        '[{"type":"resize","params":{"width":1920,"height":1080,"maintainAspect":true}},{"type":"compress","params":{"quality":80}},{"type":"convertFormat","params":{"targetFormat":"webm","codec":"vp9"}}]'::jsonb,
        true,
        NULL
    ),
    (
        'c3d4e5f6-a7b8-4c5d-0e1f-2a3b4c5d6e7f'::uuid,
        'Email Attachment',
        'video'::media_type,
        'Small file size for email (<25MB target)',
        '[{"type":"resize","params":{"width":640,"height":480,"maintainAspect":true}},{"type":"compress","params":{"targetSize":25000000}},{"type":"convertFormat","params":{"targetFormat":"mp4","codec":"h264"}}]'::jsonb,
        true,
        NULL
    ),
    (
        'd4e5f6a7-b8c9-4d5e-1f2a-3b4c5d6e7f8a'::uuid,
        'Podcast Audio',
        'audio'::media_type,
        'Optimized for podcast distribution (MP3, 128kbps)',
        '[{"type":"convertFormat","params":{"targetFormat":"mp3"}},{"type":"changeBitrate","params":{"bitrate":128000}}]'::jsonb,
        true,
        NULL
    ),
    (
        'e5f6a7b8-c9d0-4e5f-2a3b-4c5d6e7f8a9b'::uuid,
        'Create GIF',
        'video'::media_type,
        'Convert video clip to animated GIF',
        '[{"type":"createGif","params":{"fps":10,"width":480}}]'::jsonb,
        true,
        NULL
    ),
    (
        'f6a7b8c9-d0e1-4f5a-3b4c-5d6e7f8a9b0c'::uuid,
        'Extract Audio',
        'video'::media_type,
        'Extract audio track from video',
        '[{"type":"extractAudio","params":{"format":"mp3","bitrate":192000}}]'::jsonb,
        true,
        NULL
    ),
    (
        'a7b8c9d0-e1f2-4a5b-4c5d-6e7f8a9b0c1d'::uuid,
        'Thumbnail Generator',
        'video'::media_type,
        'Generate video thumbnails',
        '[{"type":"thumbnail","params":{"count":1,"width":320}}]'::jsonb,
        true,
        NULL
    ),
    (
        'b8c9d0e1-f2a3-4b5c-5d6e-7f8a9b0c1d2e'::uuid,
        'High Quality MP3',
        'audio'::media_type,
        'Convert to high quality MP3 (320kbps)',
        '[{"type":"convertFormat","params":{"targetFormat":"mp3"}},{"type":"changeBitrate","params":{"bitrate":320000}}]'::jsonb,
        true,
        NULL
    )
) AS presets_data (id, name, media_type, description, operations, is_system, user_id)
WHERE NOT EXISTS (
    SELECT 1 FROM presets WHERE presets.id = presets_data.id
);

-- ============================================================================
-- VERIFICATION QUERIES (Optional - comment out for production)
-- ============================================================================

-- Uncomment these to verify the schema after running the script:

-- -- Count tables
-- SELECT 
--     schemaname,
--     tablename
-- FROM pg_tables
-- WHERE schemaname = 'public'
-- ORDER BY tablename;

-- -- Count enum types
-- SELECT 
--     typname as enum_name,
--     array_agg(enumlabel ORDER BY enumsortorder) as enum_values
-- FROM pg_type t
-- JOIN pg_enum e ON t.oid = e.enumtypid
-- WHERE t.typname IN ('user_tier', 'subscription_tier', 'subscription_status', 'file_zone', 'job_status', 'media_type')
-- GROUP BY typname
-- ORDER BY typname;

-- -- Count presets
-- SELECT COUNT(*) as total_presets, 
--        COUNT(CASE WHEN is_system THEN 1 END) as system_presets
-- FROM presets;

-- ============================================================================
-- NOTES
-- ============================================================================
--
-- Subscription Tiers and Limits:
--   - free:     50 conversion minutes/month, 500MB max file size
--   - basic:    1,500 conversion minutes/month, 1.5GB max file size
--   - standard: 2,000 conversion minutes/month, 2GB max file size
--   - pro:      4,000 conversion minutes/month, 5GB max file size
--
-- File Zones:
--   - upload:  User-uploaded files (temporary)
--   - working: In-progress conversion files (temporary)
--   - output:  Completed conversion files (expires after 24 hours)
--
-- Job Status Flow:
--   pending -> queued -> processing -> completed/failed/cancelled
--
-- Authentication:
--   - Uses Clerk for authentication
--   - user_id in all tables is the Clerk user ID (TEXT format)
--   - user_profiles table is auto-created on first user action
--
-- ============================================================================
-- END OF SCRIPT
-- ============================================================================
