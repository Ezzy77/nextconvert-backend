# Production Database Deployment Guide

This guide explains how to deploy the NextConvert database schema to your production cloud database (Supabase, AWS RDS, etc.).

## 📋 Prerequisites

- PostgreSQL 12 or higher
- Database admin credentials
- `psql` command-line tool (or database GUI like pgAdmin, Supabase Dashboard)

## 🚀 Quick Deployment

### Option 1: Using Supabase Dashboard (Recommended)

1. **Login to Supabase**
   - Go to https://supabase.com/dashboard
   - Select your project

2. **Open SQL Editor**
   - Click "SQL Editor" in the left sidebar
   - Click "New query"

3. **Execute the Script**
   - Copy the entire contents of `production_schema.sql`
   - Paste into the SQL editor
   - Click "Run" or press `Ctrl+Enter` (Windows/Linux) / `Cmd+Enter` (Mac)

4. **Verify Success**
   - You should see "Success. No rows returned"
   - Check the "Table Editor" to see all created tables

### Option 2: Using psql Command Line

```bash
# Navigate to migrations directory
cd backend/migrations

# Connect and execute script
psql -h <your-host> \
     -p 5432 \
     -U <username> \
     -d <database> \
     -f production_schema.sql

# Example for Supabase:
psql "postgresql://postgres:[PASSWORD]@db.[PROJECT-REF].supabase.co:5432/postgres" \
     -f production_schema.sql
```

### Option 3: Using Docker (Local Testing)

```bash
# Start PostgreSQL container
docker run --name nextconvert-db \
  -e POSTGRES_PASSWORD=postgres \
  -e POSTGRES_DB=nextconvert \
  -p 5432:5432 \
  -d postgres:15-alpine

# Wait for database to be ready
sleep 5

# Execute schema
docker exec -i nextconvert-db psql -U postgres -d nextconvert < production_schema.sql
```

## 📊 What Gets Created

### Tables
- ✅ **users** - Legacy user table (kept for compatibility)
- ✅ **user_profiles** - Main user profiles with subscription tiers
- ✅ **subscriptions** - Stripe subscription tracking
- ✅ **files** - Uploaded and processed media files
- ✅ **jobs** - Media processing jobs queue
- ✅ **presets** - System and user conversion presets

### Enum Types
- `user_tier` - free, pro, enterprise
- `subscription_tier` - free, basic, standard, pro
- `subscription_status` - active, cancelled, past_due, trialing
- `file_zone` - upload, working, output
- `job_status` - pending, queued, processing, completed, failed, cancelled
- `media_type` - video, audio, image

### Indexes
- 20+ optimized indexes for fast queries
- Covering user lookups, job status, file zones, subscriptions

### Triggers
- Auto-update `updated_at` timestamps on modifications

### Default Data
- 8 system presets (Mobile Optimized, Web Optimized, etc.)

## ✅ Verification

After running the script, verify everything was created successfully:

```sql
-- Check all tables exist
SELECT tablename 
FROM pg_tables 
WHERE schemaname = 'public' 
ORDER BY tablename;

-- Expected output:
-- files
-- jobs
-- presets
-- subscriptions
-- user_profiles
-- users

-- Check enum types
SELECT typname, array_agg(enumlabel ORDER BY enumsortorder) as values
FROM pg_type t
JOIN pg_enum e ON t.oid = e.enumtypid
WHERE typname LIKE '%tier%' OR typname LIKE '%status%' OR typname IN ('file_zone', 'media_type')
GROUP BY typname;

-- Check presets were inserted
SELECT COUNT(*) as total_presets, 
       COUNT(CASE WHEN is_system THEN 1 END) as system_presets
FROM presets;

-- Expected: 8 total, 8 system presets

-- Check indexes
SELECT tablename, indexname 
FROM pg_indexes 
WHERE schemaname = 'public' 
ORDER BY tablename, indexname;
```

## 🔄 Updating Existing Database

If you already have a database with some tables:

**The script is idempotent and safe to run multiple times:**
- Uses `CREATE TABLE IF NOT EXISTS`
- Uses `CREATE INDEX IF NOT EXISTS`
- Uses `DO $$ BEGIN ... EXCEPTION WHEN duplicate_object THEN null; END $$` for enums
- Only inserts presets that don't exist

### If You Need to Reset Everything

```sql
-- ⚠️ WARNING: This will delete all data!
-- Only use in development or if you're sure

DROP TABLE IF EXISTS jobs CASCADE;
DROP TABLE IF EXISTS files CASCADE;
DROP TABLE IF EXISTS presets CASCADE;
DROP TABLE IF EXISTS subscriptions CASCADE;
DROP TABLE IF EXISTS user_profiles CASCADE;
DROP TABLE IF EXISTS users CASCADE;

DROP TYPE IF EXISTS media_type CASCADE;
DROP TYPE IF EXISTS job_status CASCADE;
DROP TYPE IF EXISTS file_zone CASCADE;
DROP TYPE IF EXISTS subscription_status CASCADE;
DROP TYPE IF EXISTS subscription_tier CASCADE;
DROP TYPE IF EXISTS user_tier CASCADE;

-- Then run production_schema.sql again
```

## 🔐 Security Considerations

### Row Level Security (RLS) - Supabase

If using Supabase, consider enabling RLS policies:

```sql
-- Enable RLS on all tables
ALTER TABLE user_profiles ENABLE ROW LEVEL SECURITY;
ALTER TABLE subscriptions ENABLE ROW LEVEL SECURITY;
ALTER TABLE files ENABLE ROW LEVEL SECURITY;
ALTER TABLE jobs ENABLE ROW LEVEL SECURITY;
ALTER TABLE presets ENABLE ROW LEVEL SECURITY;

-- Example: Users can only see their own files
CREATE POLICY "Users can view their own files"
  ON files FOR SELECT
  USING (auth.uid()::text = user_id);

CREATE POLICY "Users can insert their own files"
  ON files FOR INSERT
  WITH CHECK (auth.uid()::text = user_id);

-- Example: Users can only see their own jobs
CREATE POLICY "Users can view their own jobs"
  ON jobs FOR SELECT
  USING (auth.uid()::text = user_id);

-- Example: Everyone can view system presets
CREATE POLICY "Everyone can view system presets"
  ON presets FOR SELECT
  USING (is_system = true);

-- Example: Users can view their own presets
CREATE POLICY "Users can view their own presets"
  ON presets FOR SELECT
  USING (auth.uid()::text = user_id);
```

### Backups

Enable automated backups on your cloud provider:

- **Supabase**: Automatic daily backups included
- **AWS RDS**: Enable automated backups (7-35 days retention)
- **Google Cloud SQL**: Enable automated backups

## 🐛 Troubleshooting

### Error: "extension uuid-ossp does not exist"

**Solution**: Your database user needs superuser privileges, or run:
```sql
CREATE EXTENSION IF NOT EXISTS "uuid-ossp" SCHEMA public;
```

### Error: "type X already exists"

**Solution**: The script handles this automatically. This warning is safe to ignore.

### Error: "permission denied to create extension"

**Solution for Supabase**: Extensions are pre-installed, this shouldn't happen.

**Solution for other providers**: Contact your database admin or use a superuser account.

### Connection Issues

Make sure your IP is whitelisted:
- **Supabase**: Check "Settings" → "Database" → "Connection pooling"
- **AWS RDS**: Check Security Groups
- **Google Cloud**: Check authorized networks

## 📝 Environment Variables

After deployment, update your `.env.prod` file:

```bash
# Your Supabase connection string
DATABASE_URL=postgresql://postgres:[PASSWORD]@db.[PROJECT-REF].supabase.co:5432/postgres?sslmode=require

# Or direct connection (for migrations)
DATABASE_URL=postgresql://postgres:[PASSWORD]@db.[PROJECT-REF].supabase.co:5432/postgres

# For connection pooling (recommended for API)
DATABASE_URL=postgresql://postgres.[PROJECT-REF]:[PASSWORD]@aws-0-[region].pooler.supabase.com:6543/postgres
```

## 🎯 Next Steps

1. ✅ Run `production_schema.sql` in your cloud database
2. ✅ Verify all tables and indexes are created
3. ✅ Update your `.env.prod` with the correct `DATABASE_URL`
4. ✅ Test connection from your backend:
   ```bash
   cd backend
   go run cmd/server/main.go
   ```
5. ✅ Check health endpoint: `curl http://localhost:8080/api/v1/health`

## 📚 Additional Resources

- [Supabase Database Documentation](https://supabase.com/docs/guides/database)
- [PostgreSQL Official Docs](https://www.postgresql.org/docs/)
- [NextConvert Backend README](../README.md)

---

**Need Help?** Check the logs or database connection settings in your `.env.prod` file.
