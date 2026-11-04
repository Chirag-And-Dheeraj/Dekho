#!/bin/bash
set -e

mkdir -p "$ROOT_PATH/video" "$ROOT_PATH/hls_chunks" "$ROOT_PATH/manifests" "$ROOT_PATH/thumbnails"
exec go run ./cmd/worker
