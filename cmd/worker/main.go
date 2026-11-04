package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/hibiken/asynq"
	_ "github.com/lib/pq"

	"video-streaming-server/config"
	"video-streaming-server/database"
	"video-streaming-server/processing"
	"video-streaming-server/queueing"
	"video-streaming-server/shared/logger"
)

func main() {
	if err := config.LoadConfig(".env"); err != nil {
		slog.Error("failed to load configuration", "error", err)
		os.Exit(1)
	}
	logger.Init(config.AppConfig.Debug)
	db, err := database.GetDBConn()
	if err != nil {
		slog.Error("failed to connect to database", "error", err)
		os.Exit(1)
	}
	defer db.Close()
	redisOpt := asynq.RedisClientOpt{Addr: config.AppConfig.RedisAddr, Password: config.AppConfig.RedisPassword, DB: config.AppConfig.RedisDB}
	server := asynq.NewServer(redisOpt, asynq.Config{
		Concurrency:     config.AppConfig.WorkerConcurrency,
		Queues:          map[string]int{"default": 1},
		ShutdownTimeout: config.AppConfig.WorkerShutdownTimeout,
	})
	mux := asynq.NewServeMux()
	mux.Handle(queueing.TypeProcessVideo, &processing.Handler{DB: db})

	if err := server.Start(mux); err != nil {
		slog.Error("failed to start Asynq worker", "error", err)
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()
	server.Shutdown()
	server.Stop()
}
