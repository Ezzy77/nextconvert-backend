-- ============================================================================
-- NextConvert Example Queries
-- ============================================================================
-- Sample queries to test and understand the database schema
-- ============================================================================

-- ============================================================================
-- 1. USER MANAGEMENT
-- ============================================================================

-- Create a test user profile (simulating first-time user)
INSERT INTO user_profiles (user_id, tier, conversion_minutes_used, usage_period_start)
VALUES ('user_test123', 'free', 0, NOW())
ON CONFLICT (user_id) DO NOTHING;

-- Get user profile with subscription info
SELECT 
    user_id,
    tier,
    conversion_minutes_used,
    usage_period_start,
    stripe_customer_id,
    created_at
FROM user_profiles
WHERE user_id = 'user_test123';

-- Upgrade user to pro tier
UPDATE user_profiles
SET tier = 'pro',
    stripe_customer_id = 'cus_test123',
    updated_at = NOW()
WHERE user_id = 'user_test123';

-- Get subscription limits by tier
SELECT 
    'free' as tier, 50 as conversion_minutes, '500 MB' as max_file_size
UNION ALL
SELECT 'basic', 1500, '1.5 GB'
UNION ALL
SELECT 'standard', 2000, '2 GB'
UNION ALL
SELECT 'pro', 4000, '5 GB';

-- ============================================================================
-- 2. FILE MANAGEMENT
-- ============================================================================

-- Upload a test file
INSERT INTO files (id, user_id, original_name, storage_path, mime_type, size_bytes, zone, media_type, metadata, expires_at)
VALUES (
    uuid_generate_v4(),
    'user_test123',
    'sample_video.mp4',
    'upload/user_test123/sample_video.mp4',
    'video/mp4',
    10485760, -- 10MB
    'upload',
    'video',
    '{"duration": 120, "resolution": "1920x1080", "codec": "h264"}'::jsonb,
    NOW() + INTERVAL '24 hours'
)
RETURNING id, original_name, size_bytes, zone;

-- Get all files for a user
SELECT 
    id,
    original_name,
    zone,
    media_type,
    pg_size_pretty(size_bytes) as file_size,
    expires_at,
    created_at
FROM files
WHERE user_id = 'user_test123'
ORDER BY created_at DESC;

-- Get files by zone
SELECT 
    zone,
    COUNT(*) as file_count,
    pg_size_pretty(SUM(size_bytes)) as total_size
FROM files
WHERE user_id = 'user_test123'
GROUP BY zone;

-- Find expired files (for cleanup)
SELECT 
    id,
    original_name,
    zone,
    expires_at,
    AGE(NOW(), expires_at) as overdue_by
FROM files
WHERE expires_at < NOW()
ORDER BY expires_at;

-- ============================================================================
-- 3. JOB MANAGEMENT
-- ============================================================================

-- Create a test job
WITH input_file AS (
    SELECT id FROM files WHERE user_id = 'user_test123' LIMIT 1
)
INSERT INTO jobs (user_id, status, priority, input_file_id, output_format, output_file_name, operations, input_duration_seconds, conversion_minutes)
SELECT 
    'user_test123',
    'queued',
    5,
    id,
    'mp4',
    'converted_video.mp4',
    '[{"type":"resize","params":{"width":1280,"height":720}},{"type":"compress","params":{"quality":70}}]'::jsonb,
    120.5,
    3
FROM input_file
RETURNING id, status, created_at;

-- Get all jobs for a user
SELECT 
    j.id,
    j.status,
    j.output_format,
    j.output_file_name,
    j.conversion_minutes,
    (j.progress->>'percent')::int as progress_percent,
    j.created_at,
    j.started_at,
    j.completed_at,
    f.original_name as input_file
FROM jobs j
LEFT JOIN files f ON j.input_file_id = f.id
WHERE j.user_id = 'user_test123'
ORDER BY j.created_at DESC;

-- Get jobs by status
SELECT 
    status,
    COUNT(*) as job_count,
    AVG((progress->>'percent')::int) as avg_progress
FROM jobs
WHERE user_id = 'user_test123'
GROUP BY status;

-- Update job progress
UPDATE jobs
SET 
    status = 'processing',
    progress = '{"percent": 50, "currentOperation": "Compressing video..."}'::jsonb,
    started_at = NOW()
WHERE id = (SELECT id FROM jobs WHERE user_id = 'user_test123' AND status = 'queued' LIMIT 1);

-- Complete a job
WITH output_file AS (
    INSERT INTO files (user_id, original_name, storage_path, mime_type, size_bytes, zone, media_type, expires_at)
    VALUES (
        'user_test123',
        'converted_video.mp4',
        'output/user_test123/converted_video.mp4',
        'video/mp4',
        8388608, -- 8MB
        'output',
        'video',
        NOW() + INTERVAL '24 hours'
    )
    RETURNING id
)
UPDATE jobs j
SET 
    status = 'completed',
    progress = '{"percent": 100}'::jsonb,
    output_file_id = (SELECT id FROM output_file),
    completed_at = NOW()
WHERE j.id = (SELECT id FROM jobs WHERE user_id = 'user_test123' AND status = 'processing' LIMIT 1);

-- Get job statistics
SELECT 
    COUNT(*) as total_jobs,
    COUNT(CASE WHEN status = 'completed' THEN 1 END) as completed_jobs,
    COUNT(CASE WHEN status = 'failed' THEN 1 END) as failed_jobs,
    COUNT(CASE WHEN status = 'processing' THEN 1 END) as in_progress_jobs,
    AVG(EXTRACT(EPOCH FROM (completed_at - created_at))) as avg_processing_time_seconds
FROM jobs
WHERE user_id = 'user_test123';

-- ============================================================================
-- 4. SUBSCRIPTION MANAGEMENT
-- ============================================================================

-- Create a subscription (from Stripe webhook)
INSERT INTO subscriptions (user_id, tier, stripe_subscription_id, stripe_customer_id, status, current_period_start, current_period_end)
VALUES (
    'user_test123',
    'pro',
    'sub_test123',
    'cus_test123',
    'active',
    NOW(),
    NOW() + INTERVAL '1 month'
)
ON CONFLICT (stripe_subscription_id) DO UPDATE
SET status = EXCLUDED.status,
    updated_at = NOW();

-- Get active subscriptions
SELECT 
    s.user_id,
    s.tier,
    s.status,
    s.current_period_start,
    s.current_period_end,
    EXTRACT(DAY FROM (s.current_period_end - NOW())) as days_remaining,
    up.conversion_minutes_used
FROM subscriptions s
JOIN user_profiles up ON s.user_id = up.user_id
WHERE s.status = 'active'
ORDER BY s.current_period_end;

-- Record usage (after job completion)
UPDATE user_profiles
SET conversion_minutes_used = conversion_minutes_used + 3,
    updated_at = NOW()
WHERE user_id = 'user_test123';

-- Check if user exceeded limits
SELECT 
    user_id,
    tier,
    conversion_minutes_used,
    CASE tier
        WHEN 'free' THEN 50
        WHEN 'basic' THEN 1500
        WHEN 'standard' THEN 2000
        WHEN 'pro' THEN 4000
    END as limit,
    conversion_minutes_used::float / 
    CASE tier
        WHEN 'free' THEN 50
        WHEN 'basic' THEN 1500
        WHEN 'standard' THEN 2000
        WHEN 'pro' THEN 4000
    END * 100 as usage_percentage
FROM user_profiles
WHERE user_id = 'user_test123';

-- Reset monthly usage (run at billing period start)
UPDATE user_profiles
SET conversion_minutes_used = 0,
    usage_period_start = NOW(),
    updated_at = NOW()
WHERE user_id = 'user_test123';

-- ============================================================================
-- 5. PRESETS
-- ============================================================================

-- Get all system presets
SELECT 
    name,
    media_type,
    description,
    is_system
FROM presets
WHERE is_system = true
ORDER BY media_type, name;

-- Get presets for a specific media type
SELECT 
    name,
    description,
    operations
FROM presets
WHERE media_type = 'video'
AND is_system = true;

-- Create a custom user preset
INSERT INTO presets (name, media_type, description, operations, is_system, user_id)
VALUES (
    'My Custom 4K Preset',
    'video',
    'Custom 4K video with high quality settings',
    '[{"type":"resize","params":{"width":3840,"height":2160}},{"type":"compress","params":{"quality":90}}]'::jsonb,
    false,
    'user_test123'
);

-- Get user's custom presets
SELECT 
    name,
    media_type,
    description,
    created_at
FROM presets
WHERE user_id = 'user_test123'
ORDER BY created_at DESC;

-- ============================================================================
-- 6. ANALYTICS & REPORTING
-- ============================================================================

-- User activity summary
SELECT 
    up.user_id,
    up.tier,
    up.conversion_minutes_used,
    COUNT(DISTINCT f.id) as total_files,
    COUNT(DISTINCT j.id) as total_jobs,
    pg_size_pretty(COALESCE(SUM(f.size_bytes), 0)) as total_storage_used
FROM user_profiles up
LEFT JOIN files f ON up.user_id = f.user_id
LEFT JOIN jobs j ON up.user_id = j.user_id
WHERE up.user_id = 'user_test123'
GROUP BY up.user_id, up.tier, up.conversion_minutes_used;

-- Daily job statistics
SELECT 
    DATE(created_at) as date,
    COUNT(*) as total_jobs,
    COUNT(CASE WHEN status = 'completed' THEN 1 END) as completed,
    COUNT(CASE WHEN status = 'failed' THEN 1 END) as failed,
    AVG(EXTRACT(EPOCH FROM (completed_at - started_at))) as avg_duration_seconds
FROM jobs
WHERE created_at >= NOW() - INTERVAL '7 days'
GROUP BY DATE(created_at)
ORDER BY date DESC;

-- Popular output formats
SELECT 
    output_format,
    COUNT(*) as usage_count,
    ROUND(COUNT(*) * 100.0 / (SELECT COUNT(*) FROM jobs WHERE output_format IS NOT NULL), 2) as percentage
FROM jobs
WHERE output_format IS NOT NULL
GROUP BY output_format
ORDER BY usage_count DESC;

-- Storage usage by zone
SELECT 
    zone,
    COUNT(*) as file_count,
    pg_size_pretty(SUM(size_bytes)) as total_size,
    AVG(size_bytes)::bigint as avg_file_size
FROM files
GROUP BY zone
ORDER BY SUM(size_bytes) DESC;

-- ============================================================================
-- 7. MAINTENANCE QUERIES
-- ============================================================================

-- Clean up expired files
DELETE FROM files
WHERE expires_at < NOW()
AND zone IN ('upload', 'output');

-- Cancel stale jobs (stuck in processing for more than 2 hours)
UPDATE jobs
SET status = 'failed',
    error = '{"code": "timeout", "message": "Job processing timeout"}'::jsonb,
    completed_at = NOW()
WHERE status = 'processing'
AND started_at < NOW() - INTERVAL '2 hours';

-- Find large files
SELECT 
    user_id,
    original_name,
    zone,
    pg_size_pretty(size_bytes) as file_size,
    created_at
FROM files
WHERE size_bytes > 1073741824 -- Files larger than 1GB
ORDER BY size_bytes DESC
LIMIT 10;

-- Vacuum and analyze tables (optimize database)
VACUUM ANALYZE files;
VACUUM ANALYZE jobs;
VACUUM ANALYZE user_profiles;
VACUUM ANALYZE subscriptions;

-- ============================================================================
-- 8. CLEANUP TEST DATA
-- ============================================================================

-- Remove test data (run this after testing)
-- DELETE FROM jobs WHERE user_id = 'user_test123';
-- DELETE FROM files WHERE user_id = 'user_test123';
-- DELETE FROM subscriptions WHERE user_id = 'user_test123';
-- DELETE FROM user_profiles WHERE user_id = 'user_test123';
-- DELETE FROM presets WHERE user_id = 'user_test123';

-- ============================================================================
-- END OF EXAMPLES
-- ============================================================================
