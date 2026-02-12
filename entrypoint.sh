#!/bin/sh
set -e

# Create directories if they don't exist
# Use -p flag to not fail if directories already exist
mkdir -p /app/data/upload /app/data/working /app/data/output 2>/dev/null || true

# Change ownership to current user (works even if running as non-root)
chown -R $(id -u):$(id -g) /app/data 2>/dev/null || true

# Execute the command passed as arguments
exec "$@"
