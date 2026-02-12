#!/bin/sh
set -e

# Create directories with full permissions for Railway volume compatibility
mkdir -p /app/data/upload /app/data/working /app/data/output 2>/dev/null || true
chmod -R 777 /app/data 2>/dev/null || true

# Execute the command passed as arguments
exec "$@"
