package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/hibiken/asynq"
	_ "github.com/lib/pq"

	"video-streaming-server/config"
	"video-streaming-server/database"
	"video-streaming-server/processing"
	"video-streaming-server/queueing"
	"video-streaming-server/shared"
	"video-streaming-server/shared/logger"
	"video-streaming-server/shared/redisconn"
)

func main() {
	if err := run(); err != nil {
		slog.Error("worker stopped unexpectedly", "error", err)
		os.Exit(1)
	}
}

func run() error {
	// Render injects PORT into the process environment. Capture it before
	// loading .env so a local .env file cannot override Render's assigned port.
	renderPort, renderPortSet := os.LookupEnv("PORT")
	if err := config.LoadConfig(".env"); err != nil {
		return fmt.Errorf("load configuration: %w", err)
	}
	logger.Init(config.AppConfig.Debug)

	db, err := database.GetDBConn()
	if err != nil {
		return fmt.Errorf("connect to database: %w", err)
	}
	defer db.Close()

	redisOptions, err := redisconn.AsynqOptions()
	if err != nil {
		return fmt.Errorf("configure Asynq Redis connection: %w", err)
	}
	worker := asynq.NewServer(redisOptions, asynq.Config{
		Concurrency:     config.AppConfig.WorkerConcurrency,
		Queues:          map[string]int{"default": 1},
		ShutdownTimeout: config.AppConfig.WorkerShutdownTimeout,
	})
	mux := asynq.NewServeMux()
	mux.Handle(queueing.TypeProcessVideo, &processing.Handler{DB: db})
	if err := worker.Start(mux); err != nil {
		return fmt.Errorf("start Asynq worker: %w", err)
	}

	port := strings.TrimSpace(os.Getenv("WORKER_HEALTH_PORT"))
	if port == "" && renderPortSet {
		port = renderPort
	}
	if port == "" {
		port = "8082"
	}
	healthMux := http.NewServeMux()
	healthMux.HandleFunc("/healthz", shared.HealthHandler)
	healthServer := &http.Server{
		Addr:              ":" + port,
		Handler:           healthMux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	serverErrors := make(chan error, 1)
	go func() {
		if err := healthServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErrors <- err
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	logger.Log.Info("video worker is ready", "health_address", healthServer.Addr, "concurrency", config.AppConfig.WorkerConcurrency)
	var runErr error
	select {
	case <-ctx.Done():
		logger.Log.Info("worker shutdown requested")
	case err := <-serverErrors:
		runErr = fmt.Errorf("worker health server failed: %w", err)
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), config.AppConfig.WorkerShutdownTimeout)
	defer cancel()
	if err := healthServer.Shutdown(shutdownCtx); err != nil {
		logger.Log.Error("failed to stop worker health server cleanly", "error", err)
	}
	worker.Stop()
	worker.Shutdown()
	return runErr
}
