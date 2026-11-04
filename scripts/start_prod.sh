#!/bin/bash

make clean init install-prod build
echo "Applying migrations..."
make migrate-up
echo "API binary: bin/dekho-api"
echo "Worker binary: bin/dekho-worker"
