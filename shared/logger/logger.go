package logger

import (
	"log/slog"
	"os"
)

var (
	Log *slog.Logger
)

func Init(debug bool) {
	handler := slog.Handler(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level:     slog.LevelInfo,
		AddSource: true,
	}))
	if debug {
		handler = slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
			Level:     slog.LevelDebug,
			AddSource: true,
		})
	}
	Log = slog.New(handler)
	Log.Info("Logger initialized", "debug", debug)
	slog.SetDefault(Log)
}
