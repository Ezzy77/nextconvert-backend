# Database Migrations

This directory contains all database schema files for NextConvert.

## 📁 Files Overview

### Production Deployment Files

| File | Purpose | When to Use |
|------|---------|-------------|
| **production_schema.sql** | Complete production schema | Deploy to new cloud database |
| **PRODUCTION_DEPLOYMENT.md** | Deployment guide | Step-by-step deployment instructions |
| **verify_schema.sql** | Verification script | Verify schema after deployment |
| **example_queries.sql** | Sample queries | Learn the schema, test functionality |

### Development Migration Files

| File | Purpose | Order |
|------|---------|-------|
| **init.sql** | Initial schema | 1st |
| **002_clerk_user_id.sql** | Clerk integration | 2nd |
| **003_subscriptions.sql** | Subscription system | 3rd |

## 🚀 Quick Start

### For Production Deployment

```bash
# 1. Deploy the schema
psql "your-database-url" -f production_schema.sql

# 2. Verify everything works
psql "your-database-url" -f verify_schema.sql

# 3. (Optional) Test with examples
psql "your-database-url" -f example_queries.sql
```

### For Development

```bash
# Use docker-compose (includes migrations)
cd ..
docker-compose up -d postgres

# Or apply migrations manually
psql "postgres://postgres:postgres@localhost:5432/nextconvert" -f init.sql
psql "postgres://postgres:postgres@localhost:5432/nextconvert" -f 002_clerk_user_id.sql
psql "postgres://postgres:postgres@localhost:5432/nextconvert" -f 003_subscriptions.sql
```

## 📊 Database Schema

### Tables

```
user_profiles (Primary user table)
├── user_id (TEXT, PK) - Clerk user ID
├── tier (subscription_tier) - Current subscription
├── stripe_customer_id (TEXT) - Stripe customer
├── conversion_minutes_used (INT) - Monthly usage
└── usage_period_start (TIMESTAMP) - Billing period

subscriptions (Stripe subscriptions)
├── id (UUID, PK)
├── user_id (TEXT, FK → user_profiles)
├── tier (subscription_tier)
├── stripe_subscription_id (TEXT)
├── status (subscription_status)
└── current_period_start/end (TIMESTAMP)

files (Uploaded/processed files)
├── id (UUID, PK)
├── user_id (TEXT) - Clerk user ID
├── zone (file_zone) - upload/working/output
├── media_type (media_type) - video/audio/image
└── expires_at (TIMESTAMP) - Auto-cleanup

jobs (Media processing queue)
├── id (UUID, PK)
├── user_id (TEXT) - Clerk user ID
├── status (job_status) - pending/queued/processing/...
├── input_file_id (UUID, FK → files)
├── output_file_id (UUID, FK → files)
└── operations (JSONB) - Processing operations

presets (Conversion presets)
├── id (UUID, PK)
├── name (TEXT)
├── media_type (media_type)
├── operations (JSONB)
└── is_system (BOOLEAN) - System vs user preset

users (Legacy - kept for compatibility)
└── Not actively used (auth handled by Clerk)
```

### Enums

```sql
subscription_tier: 'free' | 'basic' | 'standard' | 'pro'
subscription_status: 'active' | 'cancelled' | 'past_due' | 'trialing'
file_zone: 'upload' | 'working' | 'output'
job_status: 'pending' | 'queued' | 'processing' | 'completed' | 'failed' | 'cancelled'
media_type: 'video' | 'audio' | 'image'
```

## 🔧 Common Tasks

### Add a New Migration

1. Create a new file: `00X_description.sql`
2. Add your schema changes
3. Update `production_schema.sql` to include the changes
4. Test locally before deploying

### Reset Development Database

```bash
# Drop and recreate
docker-compose down -v
docker-compose up -d postgres
sleep 5
psql "postgres://postgres:postgres@localhost:5432/nextconvert" -f production_schema.sql
```

### Backup Production Database

```bash
# Dump schema and data
pg_dump "your-database-url" > backup_$(date +%Y%m%d).sql

# Restore from backup
psql "your-database-url" < backup_20240212.sql
```

## 📈 Subscription Tiers & Limits

| Tier | Minutes/Month | Max File Size | Priority | GPU Encoding |
|------|---------------|---------------|----------|--------------|
| Free | 50 | 500 MB | Default | ❌ |
| Basic | 1,500 | 1.5 GB | High | ❌ |
| Standard | 2,000 | 2 GB | Critical | ❌ |
| Pro | 4,000 | 5 GB | Critical | ✅ |

## 🔒 Security Notes

- All user authentication is handled by Clerk
- User IDs are TEXT format (e.g., `user_2abc123xyz`)
- Files can have NULL user_id for anonymous uploads
- Expired files are automatically cleaned up by workers
- Stripe webhooks handle subscription lifecycle

## 📚 Additional Resources

- [Production Deployment Guide](./PRODUCTION_DEPLOYMENT.md)
- [Example Queries](./example_queries.sql)
- [Backend README](../README.md)
- [Supabase Documentation](https://supabase.com/docs)

## 🐛 Troubleshooting

### "Relation already exists"
- The schema is idempotent - this is expected and safe

### "Permission denied"
- Ensure your database user has CREATE privileges
- For Supabase, use the `postgres` user

### "Type already exists"
- The script handles this automatically via `DO $$ ... EXCEPTION`

### Connection timeout
- Check your IP is whitelisted in database settings
- Verify DATABASE_URL in your `.env` file

---

**Need Help?** Check the [Production Deployment Guide](./PRODUCTION_DEPLOYMENT.md) or review the backend logs.
