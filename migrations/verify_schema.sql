-- ============================================================================
-- Schema Verification Script
-- ============================================================================
-- Run this after executing production_schema.sql to verify everything is correct
-- ============================================================================

\echo '========================================='
\echo 'NextConvert Database Schema Verification'
\echo '========================================='
\echo ''

-- Check PostgreSQL version
\echo '1. PostgreSQL Version:'
SELECT version();
\echo ''

-- Check if uuid-ossp extension is enabled
\echo '2. Required Extensions:'
SELECT extname, extversion 
FROM pg_extension 
WHERE extname = 'uuid-ossp';
\echo ''

-- Check all enum types exist
\echo '3. Enum Types:'
SELECT 
    t.typname as enum_name,
    array_agg(e.enumlabel ORDER BY e.enumsortorder) as values
FROM pg_type t
JOIN pg_enum e ON t.oid = e.enumtypid
WHERE t.typname IN ('user_tier', 'subscription_tier', 'subscription_status', 'file_zone', 'job_status', 'media_type')
GROUP BY t.typname
ORDER BY t.typname;
\echo ''

-- Check all tables exist
\echo '4. Tables Created:'
SELECT 
    tablename,
    pg_size_pretty(pg_total_relation_size(schemaname||'.'||tablename)) as size
FROM pg_tables 
WHERE schemaname = 'public'
ORDER BY tablename;
\echo ''

-- Check table row counts
\echo '5. Table Row Counts:'
SELECT 
    'users' as table_name,
    COUNT(*) as row_count
FROM users
UNION ALL
SELECT 'user_profiles', COUNT(*) FROM user_profiles
UNION ALL
SELECT 'subscriptions', COUNT(*) FROM subscriptions
UNION ALL
SELECT 'files', COUNT(*) FROM files
UNION ALL
SELECT 'jobs', COUNT(*) FROM jobs
UNION ALL
SELECT 'presets', COUNT(*) FROM presets
ORDER BY table_name;
\echo ''

-- Check indexes
\echo '6. Indexes Created:'
SELECT 
    tablename,
    indexname,
    indexdef
FROM pg_indexes 
WHERE schemaname = 'public'
AND indexname NOT LIKE '%_pkey'
ORDER BY tablename, indexname;
\echo ''

-- Check triggers
\echo '7. Triggers:'
SELECT 
    trigger_name,
    event_object_table as table_name,
    action_timing || ' ' || event_manipulation as trigger_type
FROM information_schema.triggers
WHERE trigger_schema = 'public'
ORDER BY event_object_table, trigger_name;
\echo ''

-- Check system presets
\echo '8. System Presets:'
SELECT 
    name,
    media_type,
    is_system,
    length(description) as desc_length
FROM presets
WHERE is_system = true
ORDER BY media_type, name;
\echo ''

-- Check foreign key constraints
\echo '9. Foreign Key Constraints:'
SELECT
    tc.table_name,
    kcu.column_name,
    ccu.table_name AS foreign_table_name,
    ccu.column_name AS foreign_column_name
FROM information_schema.table_constraints AS tc
JOIN information_schema.key_column_usage AS kcu
    ON tc.constraint_name = kcu.constraint_name
    AND tc.table_schema = kcu.table_schema
JOIN information_schema.constraint_column_usage AS ccu
    ON ccu.constraint_name = tc.constraint_name
    AND ccu.table_schema = tc.table_schema
WHERE tc.constraint_type = 'FOREIGN KEY' 
AND tc.table_schema = 'public'
ORDER BY tc.table_name, kcu.column_name;
\echo ''

-- Database size
\echo '10. Database Size:'
SELECT 
    pg_database.datname as database_name,
    pg_size_pretty(pg_database_size(pg_database.datname)) as size
FROM pg_database
WHERE datname = current_database();
\echo ''

-- Final summary
\echo '========================================='
\echo 'Verification Complete!'
\echo '========================================='
\echo ''
\echo 'Expected Results:'
\echo '  - 6 enum types (user_tier, subscription_tier, subscription_status, file_zone, job_status, media_type)'
\echo '  - 6 tables (users, user_profiles, subscriptions, files, jobs, presets)'
\echo '  - 8 system presets'
\echo '  - 20+ indexes'
\echo '  - 3 triggers (update_updated_at for users, user_profiles, subscriptions)'
\echo ''
\echo 'If any of these are missing, review the deployment logs.'
\echo ''
