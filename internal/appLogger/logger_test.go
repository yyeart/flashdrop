package appLogger

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func TestNewLogger_FilterByLevel(t *testing.T) {
	tests := []struct {
		name        string
		level       slog.Level
		log         func(*slog.Logger, string)
		message     string
		wantWritten bool
	}{
		{
			name:        "info allows info",
			level:       slog.LevelInfo,
			log:         func(l *slog.Logger, msg string) { l.Info(msg) },
			message:     "info message",
			wantWritten: true,
		},
		{
			name:        "info hides debug",
			level:       slog.LevelInfo,
			log:         func(l *slog.Logger, msg string) { l.Debug(msg) },
			message:     "debug message",
			wantWritten: false,
		},
		{
			name:        "debug allows debug",
			level:       slog.LevelDebug,
			log:         func(l *slog.Logger, msg string) { l.Debug(msg) },
			message:     "debug message",
			wantWritten: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			logger := NewLogger(tt.level, &buf)

			tt.log(logger, tt.message)

			written := strings.Contains(buf.String(), tt.message)
			if written != tt.wantWritten {
				t.Errorf(
					"output contains message = %v, want %v, output = %q",
					written,
					tt.wantWritten,
					buf.String(),
				)
			}
		})
	}
}

func TestNewLogger_WritesTextToProvidedWriter(t *testing.T) {
	var buf bytes.Buffer

	logger := NewLogger(slog.LevelInfo, &buf)
	logger.Info("hello logger")

	output := buf.String()

	if output == "" {
		t.Fatalf("writer received no output")
	}

	if !strings.Contains(output, "hello logger") {
		t.Errorf("output = %q, want %q", output, "hello logger")
	}

	if !strings.Contains(output, "level=INFO") {
		t.Errorf("output = %q, want text handler level", output)
	}
}
