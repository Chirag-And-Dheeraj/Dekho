# API and video worker separation plan

## Goal

Run Dekho's HTTP API and CPU intensive video processing as separate services. The API will submit video processing tasks to an Asynq queue backed by Redis. A worker service will consume those tasks and run the existing FFmpeg and storage pipeline. PostgreSQL remains the source of truth for video state, and Appwrite S3 remains the shared file store.

This plan covers the API-to-worker job flow and the worker-to-API status flow. Processing status is committed by the worker to Postgres. A transactional outbox and Redis Streams carry committed status changes to the API, which forwards them to clients over the existing SSE endpoint.

## Current flow

1. `main.go` starts one HTTP server, opens PostgreSQL, starts abandoned multipart upload cleanup, and registers page and API routes.
2. `POST /video/` in `controllers/direct_upload.go` validates upload metadata, creates an Appwrite S3 multipart upload, inserts a `status=UploadPending` video row, and returns presigned part URLs to the browser.
3. The browser uploads parts directly to Appwrite S3.
4. `POST /video/{id}/complete` verifies the caller and pending row, validates the part list against S3, completes the multipart upload, checks the resulting object size, and advances the row to `UploadedOnServer`.
5. The same handler launches `downloadAndProcessVideo` as an in-process goroutine. That function downloads the source object to local disk, then calls `utils.PostUploadProcessFile`.
6. `utils.PostUploadProcessFile` extracts and uploads a thumbnail, uses `ffprobe`/FFmpeg to create HLS segments and a manifest, uploads those assets, and updates the database to `ProcessingCompleted` or `ProcessingFailed`.
7. Processing uses a PostgreSQL advisory lock and process-local cancellation map to coordinate with video deletion. The worker must preserve the database lock and deletion checks; the process-local map can only cancel work inside the worker process.
8. The API's SSE handler stores connections in process memory. Processing currently calls `shared.SendEventToUser` directly, which will not reach SSE connections after it moves to another process.

## Target service layout

- **API service:** serves pages and HTTP endpoints, validates and finalizes uploads, writes video state to PostgreSQL, and enqueues processing tasks through an Asynq client.
- **Worker service:** runs an Asynq server, consumes video processing tasks, reads task data and current video ownership/metadata from PostgreSQL, downloads the raw object from shared Appwrite S3, processes it locally, uploads output assets, and records final state in PostgreSQL. When changing a processing status, it also inserts a corresponding outbox event in the same database transaction. The processing handler does not publish directly to Redis Streams.
- **Redis service:** added to Docker Compose and used by Asynq for its queue and task state and by the status event relay/consumers for Redis Streams. Both API and worker receive the same Redis address/configuration.
- **Asynqmon service:** add to Docker Compose to provide a web UI for monitoring Asynq queues and tasks. It connects to the Compose Redis service and runs read-only by default. Its UI should be exposed only through an authenticated/restricted access path in hosted deployments, and the Asynqmon version should be compatible with the selected Asynq version.
- **Status event relay:** an API-side background component reads committed outbox rows and publishes them to a Redis Stream. It uses PostgreSQL `LISTEN`/`NOTIFY` as a wake-up signal and checks for unpublished rows at startup and after reconnecting, so it does not rely on continuous database polling.
- **API stream consumer:** reads status events from Redis Streams and forwards them to the user's active SSE sessions. The browser still connects to the API; it does not connect to Redis or the worker.
- **PostgreSQL and Appwrite S3:** shared by both services. No local files are shared between API and worker; the worker downloads its own processing input and uses its own scratch space.

## Proposed task contract and processing flow

Use a versioned task type such as `video:process` with a compact JSON payload containing the `video_id`. Avoid sending a DB connection, S3 client, file path, or other process-local object in the task. The worker should fetch `source_file_key`, `title`, and `user_id` from PostgreSQL when handling the task. This keeps task payloads stable and lets the worker verify the current state before processing.

1. API completes and verifies the multipart upload as it does today.
2. API atomically transitions the video from `UploadPending` to `UploadedOnServer` and enqueues `video:process` with the video ID. Use an Asynq task ID derived from the video ID (or another deduplication mechanism) so client retries do not create duplicate processing jobs.
3. If enqueueing fails after the database transition, return a server error and leave a recoverable state. Add a reconciliation path that finds `UploadedOnServer` rows without a live processing task and re-enqueues them. Asynq enqueue and a PostgreSQL update cannot be made one atomic transaction without an outbox; the implementation should document and address this failure window.
4. Worker loads the video record and skips deleted, missing, or no-longer-eligible rows. It obtains the existing PostgreSQL advisory lock, downloads the raw object, and runs the existing processing steps.
5. When processing completes or fails, the worker updates `videos.status` and inserts a versioned event payload into an `outbox_events` table in the same PostgreSQL transaction. The outbox record should include a stable event ID, video ID, user ID, event type, status, and relevant client data such as title and thumbnail.
6. An API-side outbox relay publishes unpublished outbox records to a versioned Redis Stream, such as `dekho:events:video-status:v1`, then marks each record as published. Use `LISTEN`/`NOTIFY` to wake the relay when new rows commit; scan pending rows on startup/reconnect to recover missed notifications. Multiple relay instances must claim rows safely, such as with short transactions and `FOR UPDATE SKIP LOCKED`. Clean published outbox rows after a configured retention period.
7. An API-side stream consumer forwards each status event to local SSE sessions for the event's user, then acknowledges it. For multiple API replicas, each replica needs a distinct consumer group so all replicas see each event and can notify their own local SSE connections. Use stable event IDs so duplicate delivery is harmless.
8. Configure bounded Asynq retries and task timeouts appropriate for large video files. Make the handler safe to retry: check current status, make output writes deterministic/idempotent, clean partial local output, and avoid turning deletion/cancellation into an erroneous retry loop.
9. Preserve abandoned multipart upload cleanup on the API side; it handles pending browser uploads, not queued processing.

## Video metadata and enums

Extend the `videos` table to record source and media metadata:

- `duration_seconds NUMERIC(12,3)` for the source video's duration in seconds, allowing fractional seconds.
- `original_video_codec` and `original_audio_codec` for codecs detected in the uploaded source.
- `original_container` for the uploaded source container.
- `final_video_codec` and `final_audio_codec` for the codecs in the generated HLS output.
- `video_source` for how the video entered Dekho. The current form upload path sets `upload_form`; future ingestion paths can add values such as `url_import` or `api_import`.

Create PostgreSQL enum types for `video_source`, `video_codec`, `audio_codec`, and `video_container`, and matching typed Go constants/validation. An initial value set should include:

- `video_source`: `upload_form`, `url_import`, `api_import`, `other`.
- `video_codec`: `h264`, `hevc`, `vp9`, `av1`, `mpeg4`, `mpeg2video`, `other`, `unknown`, `none`.
- `audio_codec`: `aac`, `mp3`, `opus`, `vorbis`, `ac3`, `eac3`, `pcm`, `other`, `unknown`, `none`.
- `video_container`: `mp4`, `matroska`, `mov`, `webm`, `mpegts`, `hls`, `other`, `unknown`.

Normalize `ffprobe` output into these values instead of persisting arbitrary probe strings. Use `unknown` for detected values outside the supported set and `none` when the input has no stream of that media type. Keep metadata columns nullable until the relevant probe/process step has completed. The upload handler sets `video_source`; the worker probes the original file for duration, codecs, and container, then records the actual resulting codecs and output packaging after producing HLS. Add the metadata to the video API responses where clients need to display or inspect it.

Add a reversible migration for the enum types and columns. Backfill existing rows to `video_source='upload_form'` because the current creation path is the form upload flow; leave media metadata null when it cannot be reliably reconstructed. Future source paths must explicitly set a valid `video_source` value.

## Worker status updates and client notification

The worker owns processing outcomes and is the only component that updates the processing status in PostgreSQL. To avoid a database/Redis dual-write gap, each status update and its outbox event are written in one PostgreSQL transaction. The worker does not publish status events directly to Redis or call the API.

An API-side relay moves committed outbox events into Redis Streams. The relay can use `LISTEN`/`NOTIFY` for low-latency wake-ups and query pending outbox rows at startup and after reconnecting. Notifications are only wake-up hints; the outbox rows are the durable source for publication. The relay must publish before marking an outbox row as published, so a crash in between can cause a duplicate Redis event but cannot silently lose the event. Consumers must therefore be idempotent. Retain published outbox rows for a bounded period and clean them up in batches.

The API consumes Redis Stream entries and forwards them to its existing SSE connections. Redis Streams consumer groups provide acknowledgements and pending-entry recovery. A shared consumer group load-balances entries among API replicas, so use a distinct group per API replica to broadcast each event to every replica that may hold the user's SSE connection. On handling, each replica only forwards the event to its own local connections and then acknowledges it.

PostgreSQL remains the durable status record. The Redis Stream transports status-change notifications and can retain/replay events for recovery. Normal page loads can fetch the current status through the existing API; continuous database polling is not required. As with any SSE server, a successful write to the response does not prove the browser rendered the event; include the stable event ID so duplicate notifications can be ignored.

## Configuration and deployment changes

- Add Redis connection settings to `config.Config` and load them from environment variables. Prefer a Redis URL or host/port pair with optional password and database, matching Asynq's Redis connection options. The API needs access for both Asynq job enqueueing and status stream relay/consumption; the worker needs Redis access for Asynq but does not publish processing status directly.
- Add `redis` to `docker-compose.yml` with a health check and persistent storage if queue durability across container restarts is required. Set API and worker Redis configuration to the Compose service hostname.
- Add `asynqmon` to `docker-compose.yml`, wait for Redis health, point it at the Redis service, and expose its UI on a configurable port. Keep it read-only by default and restrict access when hosted.
- Define separate Compose services/build commands for the API and worker. They can use the same image but must run different entry points/commands. The worker image needs FFmpeg and the same DB/Appwrite configuration, but does not need an HTTP port.
- Update `.env.docker` and `.env.local` examples, development startup scripts, Make targets, and README instructions for starting API, worker, Redis, and PostgreSQL.
- Keep the worker's scratch directory isolated per worker/container. Do not rely on the current `./video` path being shared with the API.

## Code organization changes

1. Add a small task package for task type names, payload serialization, enqueue helpers, and queue options.
2. Move orchestration currently in `controllers/direct_upload.go` (`downloadAndProcessVideo` and its options) to a processing/worker package. Keep HTTP parsing and response handling in controllers.
3. Add an API-side Asynq client lifecycle and a worker command/entry point that loads config, initializes logging, DB, storage dependencies, and Asynq handlers.
4. Refactor `utils.PostUploadProcessFile` so status/event reporting is injectable or returned as an outcome instead of hard-coded to the process-local `shared` SSE map.
5. Add graceful shutdown for the API Asynq client, API outbox relay and stream consumers, worker server, and DB connections.
6. Add structured logs with task ID/video ID and Asynq retry/attempt details.
7. Add Go metadata enum types and conversion helpers for normalizing `ffprobe` results, and populate the new metadata fields as part of worker processing.

## Failure handling and operational behavior

- Redis unavailable: API should fail enqueue clearly; worker should fail startup/readiness rather than accept no work silently.
- Worker crash/restart: Asynq retains queued tasks; in-flight task retry behavior must be explicit and processing idempotent.
- Duplicate completion requests: preserve the current accepted response for already-advanced uploads and ensure only one effective task per video.
- Queue backlog: expose Asynq queue depth/health (at minimum logs and a health endpoint or documented Asynq monitoring option).
- Shutdown: stop accepting HTTP work, close Asynq client, and let the worker finish or safely retry in-flight work within a configured grace period.
- Deletion during processing: retain the PostgreSQL advisory lock and active-row checks. Cross-process cancellation by the in-memory cancel map is not available to the API; the existing DB deletion coordination must remain the correctness mechanism, or a Redis cancellation signal must be added if prompt cancellation is required.

## Implementation sequence

1. Add Redis configuration and Compose service, persistent storage and health check; add the Asynq dependency, task package, and Asynqmon monitoring UI service.
2. Add the metadata enum types, columns, and migration; update API models/responses and source assignment.
3. Extract processing orchestration from the upload controller into a worker handler while preserving storage, status, lock, and deletion behavior.
4. Add an outbox table and update the worker's processing status transitions so each status write and outbox event commit together.
5. Replace the API goroutine with an Asynq enqueue after successful upload finalization; handle enqueue failure and duplicate completion safely.
6. Add an API-side outbox relay and Redis Stream consumer. Use `LISTEN`/`NOTIFY` as the relay wake-up signal, with startup/reconnect scans of unpublished outbox entries; fan status events into the existing SSE channels.
7. Add a standalone worker entry point and separate API/worker Compose services using the same application image/configuration.
8. Add recovery/reconciliation for videos finalized in PostgreSQL but not enqueued, and outbox records not yet published, then document local and Docker operation.
9. Verify queue retry, duplicate enqueue, worker restart, enqueue failure, stream consumer restart/reclaim, duplicate event handling, metadata normalization, processing failure, and deletion-during-processing paths.

## Existing workspace note

At plan creation time, the working tree already contains uncommitted changes in configuration, controllers, deployment files, and processing code. Implementation should inspect and preserve those changes; this plan does not assume they are unrelated or safe to overwrite.
