#!/bin/bash
# Run database migrations against Supabase (or any PostgreSQL)
# Usage: DATABASE_URL="postgresql://postgres:[PASSWORD]@db.[PROJECT-REF].supabase.co:5432/postgres?sslmode=require" ./scripts/migrate-supabase.sh
# Or: source .env && ./scripts/migrate-supabase.sh

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BACKEND_DIR="$(dirname "$SCRIPT_DIR")"
MIGRATIONS_DIR="$BACKEND_DIR/migrations"

# Load .env from backend directory if DATABASE_URL not set
if [ -z "$DATABASE_URL" ] && [ -f "$BACKEND_DIR/.env" ]; then
  set -a
  source "$BACKEND_DIR/.env"
  set +a
fi

if [ -z "$DATABASE_URL" ]; then
  echo "Error: DATABASE_URL is not set"
  echo "Usage: DATABASE_URL=\"postgresql://...\" $0"
  exit 1
fi

if ! command -v psql &> /dev/null; then
  echo "Error: psql is required but not installed"
  echo "Install PostgreSQL client: https://www.postgresql.org/download/"
  exit 1
fi

echo "Running migrations from $MIGRATIONS_DIR..."
for f in "$MIGRATIONS_DIR"/init.sql "$MIGRATIONS_DIR"/002_clerk_user_id.sql "$MIGRATIONS_DIR"/003_subscriptions.sql; do
  if [ -f "$f" ]; then
    echo "Applying $(basename "$f")..."
    psql "$DATABASE_URL" -f "$f" -v ON_ERROR_STOP=1
    echo "Done: $(basename "$f")"
  else
    echo "Warning: $f not found, skipping"
  fi
done

echo "Migrations completed successfully."
