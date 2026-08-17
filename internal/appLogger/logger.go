package appLogger

import (
	"io"
	"log/slog"
)

func NewLogger(level slog.Level, w io.Writer) *slog.Logger {
	options := &slog.HandlerOptions{
		Level: level,
	}

	handler := slog.NewTextHandler(w, options)

	return slog.New(handler)
}
