#!make
include .env

pwd := $(shell pwd)
down ?= 1

ifeq (, $(shell which uname))
$(error This Makefile requires a Unix-like environment)
endif

install-dev:
	@echo "Checking for PostgreSQL Installation..."
	@if command -v psql > /dev/null 2>&1; then \
		echo "PostgreSQL is already installed."; \
	else \
		echo "Installing PostgreSQL..."; \
		(sudo apt-get update && sudo apt-get -y install postgresql postgresql-contrib) || exit 1; \
		echo "PostgreSQL successfully installed."; \
	fi
	@echo "Checking for Go installation..."
	@if command -v go > /dev/null 2>&1; then \
		echo "Go is already installed."; \
	else \
		echo "Installing Go version 1.23.7..."; \
		wget https://golang.org/dl/go1.23.7.linux-amd64.tar.gz -O go1.23.7.tar.gz || exit 1; \
		sudo tar -C /usr/local -xzf go1.23.7.tar.gz || exit 1; \
		echo "Go version 1.23.7 installed."; \
		rm go1.23.7.tar.gz; \
		echo "Updating PATH to include Go binary directory..."; \
		echo "export PATH=\$$PATH:/usr/local/go/bin" >> ~/.bashrc; \
		export PATH=$PATH:$(go env GOPATH)/bin \
		. ~/.bashrc; \
	fi
	@echo "Checking for FFMpeg installation..."
	@if command -v ffmpeg > /dev/null 2>&1; then \
		echo "FFMpeg is already installed."; \
	else \
		echo "Installing FFMpeg..."; \
		sudo apt-get update && sudo apt-get install -y ffmpeg || exit 1; \
		echo "FFMpeg installed."; \
	fi
	@echo "Checking for golang-migrate installation..."
	@if command -v migrate > /dev/null 2>&1; then \
		echo "golang-migrate is already installed."; \
	else \
		echo "Installing golang-migrate..."; \
		go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest || exit 1; \
		echo "golang-migrate installed."; \
	fi
	@echo "Installing Go dependencies..."
	go mod download || exit 1
	@echo "Dependencies installed."

install-prod:
	@echo "Checking for Go installation..."
	@if command -v go > /dev/null 2>&1; then \
		echo "Go is already installed."; \
	else \
		echo "Installing Go version 1.22.2..."; \
		wget https://golang.org/dl/go1.22.2.linux-amd64.tar.gz -O go1.22.2.tar.gz || exit 1; \
		sudo tar -C /usr/local -xzf go1.22.2.tar.gz || exit 1; \
		echo "Go version 1.22.2 installed."; \
		rm go1.22.2.tar.gz; \
		echo "Updating PATH to include Go binary directory..."; \
		echo "export PATH=\$$PATH:/usr/local/go/bin" >> ~/.bashrc; \
		. ~/.bashrc; \
	fi
	@echo "Checking for FFMpeg installation..."
	@if command -v ffmpeg > /dev/null 2>&1; then \
		echo "FFMpeg is already installed."; \
	else \
		echo "Installing FFMpeg..."; \
		sudo apt-get update && sudo apt-get install -y ffmpeg || exit 1; \
		echo "FFMpeg installed."; \
	fi
	@echo "Checking for golang-migrate installation..."
	@if command -v migrate > /dev/null 2>&1; then \
		echo "golang-migrate is already installed."; \
	else \
		echo "Installing golang-migrate..."; \
		go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest || exit 1; \
		echo "golang-migrate installed."; \
	fi
	@echo "Installing Go dependencies..."
	go mod download || exit 1
	@echo "Dependencies installed."

clean:
	@echo "Cleaning up directories and files..."
	@if [ -d "video" ]; then rm -r video; fi
	@if [ -d "segments" ]; then rm -r segments; fi
	@if [ -d "hls_chunks" ]; then rm -r hls_chunks; fi
	@if [ -d "manifests" ]; then rm -r manifests; fi
	@if [ -d "thumbnails" ]; then rm -r thumbnails; fi
	@echo "Clean up complete."

init:
	@echo "Initializing directories..."
	@mkdir -p video
	@mkdir -p hls_chunks
	@mkdir -p manifests
	@mkdir -p thumbnails
	@echo "Initialization complete."

start-postgres:
	@echo "Starting PostgreSQL service...";
	@if command -v service > /dev/null 2>&1; then \
		sudo service postgresql start || exit 1; \
	elif command -v systemctl > /dev/null 2>&1; then \
		sudo systemctl start postgresql || exit 1; \
	elif command -v rc-service > /dev/null 2>&1; then \
		sudo rc-service postgresql start || exit 1; \
	fi
	
	@if ps aux | grep -v grep | grep postgres > /dev/null 2>&1; then \
		echo "PostgreSQL service started.";\
	else \
		echo "PostgreSQL service failed to start.";\
	fi

start:
	make start-postgres || exit 1
	make migrate-up || exit 1
	@echo "Starting the Go application..."
	go run main.go

cleanstart:
	@echo "Performing clean start..."
	make clean || exit 1
	make init || exit 1
	make start || exit 1

build:
	go build main.go

migration:
	migrate create -ext sql -dir $(pwd)/database/migrations -seq $(name)
	sudo chmod 666 $(pwd)/database/migrations/*_$(name).up.sql $(pwd)/database/migrations/*_$(name).down.sql

migrate-up:
	migrate -path $(pwd)/database/migrations -database "postgres://${DB_USER}:${DB_PASSWORD}@${DB_HOST}:${DB_PORT}/${DB_NAME}?sslmode=${SSL_MODE}" up

migrate-down:
	migrate -path $(pwd)/database/migrations -database "postgres://${DB_USER}:${DB_PASSWORD}@${DB_HOST}:${DB_PORT}/${DB_NAME}?sslmode=${SSL_MODE}" down $(down)
