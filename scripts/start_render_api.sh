#!/bin/sh
set -eu

if [ -n "${DATABASE_URL:-}" ]; then
	database_url=$DATABASE_URL
else
	: "${DB_HOST:?Set DATABASE_URL or DB_HOST}"
	: "${DB_PORT:?Set DATABASE_URL or DB_PORT}"
	: "${DB_USER:?Set DATABASE_URL or DB_USER}"
	: "${DB_PASSWORD:?Set DATABASE_URL or DB_PASSWORD}"
	: "${DB_NAME:?Set DATABASE_URL or DB_NAME}"
	database_url="postgres://${DB_USER}:${DB_PASSWORD}@${DB_HOST}:${DB_PORT}/${DB_NAME}?sslmode=${SSL_MODE:-require}"
fi

# The pgx5 migrate driver uses the pgx5 URL scheme and supports Neon URL options.
case "$database_url" in
	postgres://*) database_url="pgx5://${database_url#postgres://}" ;;
	postgresql://*) database_url="pgx5://${database_url#postgresql://}" ;;
esac

migrate -path /app/database/migrations -database "$database_url" up
exec /app/dekho-api
