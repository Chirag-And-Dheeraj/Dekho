# Dekho: An on-demand video streaming server

## Some Achievements ✨

We launched [Dekho](https://peerlist.io/dheerajlalwani/project/dekho) on [Peerlist](https://peerlist.io) in the 3rd Week of January 2025 and we had a pretty good response.

![Project of the week](/documentation/static/Winner-Medal-Weekly.svg) ![Project of the week](/documentation/static/Winner-Medal-Monthly.svg)

## Star History

[![Star History Chart](https://api.star-history.com/svg?repos=Chirag-and-Dheeraj/video-streaming-server&type=Timeline)](https://www.star-history.com/#Chirag-and-Dheeraj/video-streaming-server&Timeline)

## Overview

Dekho is a research and study project aimed at understanding and learning about audio and video streaming. Our primary focus was on implementing the [HLS (HTTP Live Streaming)](https://developer.apple.com/streaming) protocol to build an on-demand video streaming server.

## Setup Instructions

### Basic Steps

- Clone the repository
- Create an **_[Appwrite storage bucket](https://appwrite.io/docs/products/storage)_**
  - Make sure that you make a note of `APPWRITE_KEY`, `APPWRITE_PROJECT_ID` and the `BUCKET_ID`.
  - Set `APPWRITE_S3_ENDPOINT` to the project's regional S3 endpoint (for example, `https://fra.cloud.appwrite.io/v1/s3`) and `APPWRITE_S3_REGION` to `auto` or the Appwrite region code. The API key needs the `buckets.read`, `buckets.write`, `files.read`, and `files.write` scopes.
  - Browser uploads send cross-origin `PUT` requests to this S3 endpoint, which require an `OPTIONS` preflight. Appwrite's S3 API allows browser requests from any origin by default and does not implement per-bucket CORS settings, so no Appwrite bucket CORS setup is needed. If you put a proxy in front of the endpoint or use a self-hosted deployment with different CORS behavior, allow `OPTIONS` and `PUT`, permit the request's `Content-Type` header, and expose the `ETag` response header to the browser.
  - Uploads not completed within 24 hours are automatically removed, including their multipart data and any source object. Cleanup runs every 15 minutes while the server is running.
  - Video processing runs in a separate worker service. The API enqueues work through Asynq on Redis; processing status is saved in PostgreSQL and relayed through Redis Streams to the API's SSE endpoint.
- Docker Compose starts Redis with persistent storage and a read-only Asynqmon UI at `http://localhost:8081` (configurable with `ASYNQMON_PORT`; read-only mode uses `ASYNQMON_READ_ONLY`, default `true`). Successfully completed video tasks are retained in Redis for 24 hours so they remain visible in Asynqmon. Asynq's default successful-task retention is zero, which deletes tasks on completion. The UI binds to localhost by default; for hosted deployments, expose it only through an authenticated access path.
- Asynq task settings are configurable in `.env`: `VIDEO_TASK_MAX_RETRIES` (default `5`), `VIDEO_TASK_TIMEOUT` (`6h`), `VIDEO_TASK_RETENTION` (`24h`), `VIDEO_TASK_UNIQUE_TTL` (`24h`), `WORKER_CONCURRENCY` (`2`), and `WORKER_SHUTDOWN_TIMEOUT` (`2m`). Use Go duration syntax for duration values; set retention to `0s` to remove completed tasks immediately or unique TTL to `0s` to disable enqueue deduplication. Asynqmon is the monitoring UI; retries, processing timeouts, and task retention are Asynq settings.

### Render POC deployment

`render.yaml` describes the API, the Asynq worker hosted as a free Render Web Service, and an authenticated, read-only Asynqmon Web Service. Redis and PostgreSQL remain external managed services: use the Neon connection URL for `DATABASE_URL` and the Upstash TCP/TLS URL (`rediss://...`) for `REDIS_URL`. The shared Go packages are compiled into the API, worker, and Asynqmon binaries; each Render service uses its own Dockerfile and image.

Before syncing the Blueprint, change the `dekho-api` service name in `render.yaml` to exactly match the name of the API Web Service that already exists in your Render workspace. This lets Render update that service rather than create a duplicate. Render prompts for the database, Redis, Appwrite, JWT, and Asynqmon credentials during the initial Blueprint setup. If the API is not currently using the JWT secret you enter, existing login cookies will be invalidated.

The worker exposes `GET /healthz` on Render's `PORT` while running the Asynq server in the same process. `WORKER_CONCURRENCY` defaults to `1` in the Render Blueprint to limit load on the free instance. The deployment intentionally does not include a keep-alive process; run that outside this repository while using the POC. The free Render services can still be restarted at any time, so Redis queue persistence and the API's pending-video reconciliation remain important.

Asynqmon is read-only and protected by HTTP Basic Auth (`ASYNQMON_USER` and `ASYNQMON_PASSWORD`). Keep those credentials private. For the locally hosted worker health endpoint, use `WORKER_HEALTH_PORT` (default `8082` in `.env.local`).

### With Docker

- Install `docker-compose` on your machine.
- cd into the root of the repository.
  - Create a `.env` file using **_[.env.docker](.env.docker)_** as a reference.
- Run `docker compose up --build` to start the API, worker, PostgreSQL, Redis, and Asynqmon.

### With Docker in GitHub Codespaces

- Create a `.env` file using **_[.env.docker](.env.docker)_** as a reference.
- Run `docker compose -f docker-compose.yml -f docker-compose.codespaces.yml up --build -d`.
- Open port `8000` from the Codespaces **Ports** tab to use the app.
- Open port `8081` to view the Asynqmon queue dashboard.
- Stop the services with `docker compose -f docker-compose.yml -f docker-compose.codespaces.yml down`.

#### Separate service terminals in VS Code

- Open **Run and Debug**, select **Dekho: All services (separate terminals)**, and press **F5**. This starts the Codespaces Compose stack, then opens a terminal following logs for PostgreSQL, Redis, Asynqmon, the API, the worker, and the Codespaces proxy.
- Stop the log sessions with **Shift+F5**. To stop the containers too, run **Tasks: Run Task** and select **Dekho: Stop Compose stack (Codespaces)**.

### Without Docker

- Install the `make` utility because we have created a **_[Makefile](Makefile)_** to ease the setup
- Now we are moving to the dependency installation steps.
- This project needs Go 1.27.3 or newer, the `ffmpeg` utility, and a `psql` (PostgreSQL) 16.3 database.
- To install these, and the Go dependencies, run `make install-dev`.
  - The `make install-dev` takes care of installing PostgreSQL, Go, FFMpeg, golang-migrate and Go dependencies.
  - Run `make start-postgres` to start the postgres service.
  - Create a database user by running this command `sudo -u postgres createuser -s username_here -P`
  - `-P` will prompt for a password.
  - Enter the `psql` shell by running this command `sudo -u postgres psql` and create a database by running `CREATE DATABASE <database_name>;`.
  - Create a `.env` file using **_[.env.local](.env.local)_** as a reference.
- Once the dependencies are installed successfully, run `make cleanstart`.
- This command will create all the necessary folders and start the API server on `http://127.0.0.1:8000`. Redis must also be running; use `make start-redis` or start it through Docker Compose.
- If you just want to run the server, run: `make start`
- To start a worker locally, run: `make start-worker`.
- If you just want to clean up, run: `make clean`

## Technologies Used

- **API and worker:** Go, with Asynq for background processing
- **Queue and status events:** Redis (Asynq queue and Redis Streams)
- **Database:** PostgreSQL
- **Storage:** [Appwrite Storage](https://appwrite.io/docs/products/storage)
- **Video Processing:** [FFMPEG](https://ffmpeg.org) for breaking down videos into .ts chunks
- **Video Player:** [HLS.js](https://github.com/video-dev/hls.js)
- **Frontend:** HTML, CSS, JS

## Project Architecture

Appwrite S3 object keys use `raw_files/<videoID><extension>`, `hls_chunks/<videoID>/<chunk>.ts`, `manifests/<videoID>.m3u8`, and `thumbnails/<videoID>.png`.

![Architecture Diagram](documentation/static/dekho-architecture.excalidraw.png)

## Key Resources

Our journey has been greatly enriched by the insights and guidance from various resources. A pivotal article that set us on the right path is "[Learning the basics of video streaming with Golang](https://www.rohitmundra.com/video-streaming-server)" by [Rohit Mundra](https://twitter.com/brohit3).

For a comprehensive list of resources that have been instrumental in our learning and development process, please refer to our [documentation](https://github.com/Chirag-And-Dheeraj/video-streaming-server/blob/main/documentation/video-streaming-project-stuff/links.md).

## Getting Involved

We welcome any queries or contributions to the project. If you have any questions or suggestions, please feel free to reach out to us:

- **Dheeraj Lalwani:** [Twitter](https://twitter.com/DhiruCodes)
- **Chirag Lulla:** [Twitter](https://twitter.com/_chiraglulla_)

## Updates and Blog Posts

Stay tuned for upcoming blog posts and updates on our progress. If you haven't heard from us in a while, feel free to bug us about it!
