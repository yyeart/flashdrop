package config

import (
	"log/slog"
	"strings"
	"testing"
	"time"
)

func TestLoad_ValidConfig(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want Config
	}{
		{
			name: "defaults",
			want: Config{
				LogLevel:        slog.LevelInfo,
				ShutdownTimeout: 10 * time.Second,
			},
		},
		{
			name: "debug",
			env: map[string]string{
				"LOG_LEVEL":        "DEBUG",
				"SHUTDOWN_TIMEOUT": "30s",
			},
			want: Config{
				LogLevel:        slog.LevelDebug,
				ShutdownTimeout: 30 * time.Second,
			},
		},
		{
			name: "lowercase warn",
			env: map[string]string{
				"LOG_LEVEL":        "warn",
				"SHUTDOWN_TIMEOUT": "5s",
			},
			want: Config{
				LogLevel:        slog.LevelWarn,
				ShutdownTimeout: 5 * time.Second,
			},
		},
		{
			name: "error",
			env: map[string]string{
				"LOG_LEVEL":        "ERROR",
				"SHUTDOWN_TIMEOUT": "1m",
			},
			want: Config{
				LogLevel:        slog.LevelError,
				ShutdownTimeout: time.Minute,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := load(mapLookup(tt.env))
			if err != nil {
				t.Fatalf("load() unexpected error: %v", err)
			}

			if got != tt.want {
				t.Errorf("load() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestLoad_InvalidConfig(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		wantErr string
	}{
		{
			name: "empty log level",
			env: map[string]string{
				"LOG_LEVEL": "",
			},
			wantErr: "LOG_LEVEL variable is empty",
		},
		{
			name: "unknown log level",
			env: map[string]string{
				"LOG_LEVEL": "TRACE",
			},
			wantErr: "unknown LOG_LEVEL log level: TRACE",
		},
		{
			name: "WARNING is not supported",
			env: map[string]string{
				"LOG_LEVEL": "WARNING",
			},
			wantErr: "unknown LOG_LEVEL log level: WARNING",
		},
		{
			name: "empty shutdown timeout",
			env: map[string]string{
				"SHUTDOWN_TIMEOUT": "",
			},
			wantErr: "SHUTDOWN_TIMEOUT variable is empty",
		},
		{
			name: "invalid shutdown timeout",
			env: map[string]string{
				"SHUTDOWN_TIMEOUT": "invalid",
			},
			wantErr: "error parsing SHUTDOWN_TIMEOUT duration",
		},
		{
			name: "zero shutdown timeout",
			env: map[string]string{
				"SHUTDOWN_TIMEOUT": "0s",
			},
			wantErr: "SHUTDOWN_TIMEOUT duration must be > 0",
		},
		{
			name: "negative shutdown timeout",
			env: map[string]string{
				"SHUTDOWN_TIMEOUT": "-1s",
			},
			wantErr: "SHUTDOWN_TIMEOUT duration must be > 0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := load(mapLookup(tt.env))
			assertErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestLoad_WrapsError(t *testing.T) {
	t.Setenv("LOG_LEVEL", "invalid")
	t.Setenv("SHUTDOWN_TIMEOUT", "10s")

	_, err := Load()

	assertErrorContains(t, err, "failed to load config")
}

func mapLookup(env map[string]string) func(string) (string, bool) {
	return func(key string) (string, bool) {
		value, ok := env[key]
		return value, ok
	}
}

func assertErrorContains(t *testing.T, err error, want string) {
	t.Helper()

	if err == nil {
		t.Fatalf("error = nil, want error containing %q", want)
	}

	if !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %q, want error containing %q", err, want)
	}
}
