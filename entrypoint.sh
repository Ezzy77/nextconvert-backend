#!/bin/sh
set -e

echo "Running as: $(id)"
echo "Owner of /app/data: $(ls -la /app/ | grep data)"

mkdir -p /app/data/upload /app/data/working /app/data/output
chmod -R 777 /app/data

exec "$@"
