#!/bin/bash
set -e

make init
echo "Applying migrations..."
make migrate-up DB_HOST="$DB_HOST" SSL_MODE="$SSL_MODE"
echo "Starting server..."
air
