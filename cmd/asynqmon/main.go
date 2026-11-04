package main

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/hibiken/asynqmon"

	"video-streaming-server/config"
	"video-streaming-server/shared"
	"video-streaming-server/shared/redisconn"
)

func main() {
	if err := run(); err != nil {
		slog.Error("Asynqmon stopped unexpectedly", "error", err)
		os.Exit(1)
	}
}

func run() error {
	if err := config.LoadConfig(".env"); err != nil {
		return fmt.Errorf("load configuration: %w", err)
	}
	username := os.Getenv("ASYNQMON_USER")
	password := os.Getenv("ASYNQMON_PASSWORD")
	if username == "" || password == "" {
		return errors.New("ASYNQMON_USER and ASYNQMON_PASSWORD must be set")
	}
	redisOptions, err := redisconn.AsynqOptions()
	if err != nil {
		return fmt.Errorf("configure Asynqmon Redis connection: %w", err)
	}
	monitor := asynqmon.New(asynqmon.Options{
		RootPath:     "/",
		RedisConnOpt: redisOptions,
		ReadOnly:     true,
	})
	defer monitor.Close()

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", shared.HealthHandler)
	mux.Handle("/", requireBasicAuth(username, password, monitor))
	port := strings.TrimSpace(os.Getenv("PORT"))
	if port == "" {
		port = "8081"
	}
	server := &http.Server{
		Addr:              ":" + port,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	serverErrors := make(chan error, 1)
	go func() {
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErrors <- err
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	slog.Info("Asynqmon is ready", "address", server.Addr)
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return server.Shutdown(shutdownCtx)
	case err := <-serverErrors:
		return fmt.Errorf("Asynqmon HTTP server failed: %w", err)
	}
}

func requireBasicAuth(username, password string, next http.Handler) http.Handler {
	expectedUser := []byte(username)
	expectedPassword := []byte(password)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		providedUser, providedPassword, ok := r.BasicAuth()
		userMatches := 0
		passwordMatches := 0
		if ok {
			userMatches = subtle.ConstantTimeCompare([]byte(providedUser), expectedUser)
			passwordMatches = subtle.ConstantTimeCompare([]byte(providedPassword), expectedPassword)
		}
		if userMatches != 1 || passwordMatches != 1 {
			w.Header().Set("WWW-Authenticate", `Basic realm="Dekho Asynqmon", charset="UTF-8"`)
			http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}
