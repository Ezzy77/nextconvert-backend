#!/bin/sh
set -e

# Start the worker in the background
/app/worker &

# Start the server in the foreground
exec /app/server
