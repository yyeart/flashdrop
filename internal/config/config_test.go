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
			name: "database URL",
			env: map[string]string{
				"DATABASE_URL": "postgres://test:test@localhost:5432/flashdrop?sslmode=disable",
			},
			want: Config{
				LogLevel:        slog.LevelInfo,
				ShutdownTimeout: 10 * time.Second,
				DatabaseURL:     "postgres://test:test@localhost:5432/flashdrop?sslmode=disable",
			},
		},
		{
			name: "empty database URL remains optional",
			env: map[string]string{
				"DATABASE_URL": "",
			},
			want: Config{
				LogLevel:        slog.LevelInfo,
				ShutdownTimeout: 10 * time.Second,
				DatabaseURL:     "",
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

func TestLoadServer_Defaults(t *testing.T) {
	got, err := loadServer(mapLookup(map[string]string{
		"DATABASE_URL":         "postgres://localhost/flashdrop",
		"JWT_PUBLIC_KEY_FILE":  "public.pem",
		"JWT_PRIVATE_KEY_FILE": "private.pem",
	}))
	if err != nil {
		t.Fatalf("loadServer() unexpected error: %v", err)
	}

	want := ServerConfig{
		Config: Config{
			LogLevel:        slog.LevelInfo,
			ShutdownTimeout: 10 * time.Second,
			DatabaseURL:     "postgres://localhost/flashdrop",
		},
		HTTPAddr:            "127.0.0.1:8080",
		HTTPRequestTimeout:  5 * time.Second,
		HTTPShutdownTimeout: 5 * time.Second,
		ReservationTTL:      15 * time.Minute,
		JWTTTL:              15 * time.Minute,
		JWTPublicKeyFile:    "public.pem",
		JWTPrivateKeyFile:   "private.pem",
	}
	if got != want {
		t.Errorf("loadServer() = %+v, want %+v", got, want)
	}
}

func TestLoadServer_Overrides(t *testing.T) {
	got, err := loadServer(mapLookup(map[string]string{
		"LOG_LEVEL":             "debug",
		"SHUTDOWN_TIMEOUT":      "30s",
		"DATABASE_URL":          "postgres://localhost/other",
		"HTTP_ADDR":             "0.0.0.0:9090",
		"HTTP_REQUEST_TIMEOUT":  "2s",
		"HTTP_SHUTDOWN_TIMEOUT": "7s",
		"RESERVATION_TTL":       "20m",
		"JWT_ISSUER":            "custom-issuer",
		"JWT_AUDIENCE":          "custom-audience",
		"JWT_TTL":               "10m",
		"JWT_PUBLIC_KEY_FILE":   "other-public.pem",
		"JWT_PRIVATE_KEY_FILE":  "other-private.pem",
	}))
	if err != nil {
		t.Fatalf("loadServer() unexpected error: %v", err)
	}

	want := ServerConfig{
		Config: Config{
			LogLevel:        slog.LevelDebug,
			ShutdownTimeout: 30 * time.Second,
			DatabaseURL:     "postgres://localhost/other",
		},
		HTTPAddr:            "0.0.0.0:9090",
		HTTPRequestTimeout:  2 * time.Second,
		HTTPShutdownTimeout: 7 * time.Second,
		ReservationTTL:      20 * time.Minute,
		JWTIssuer:           "custom-issuer",
		JWTAudience:         "custom-audience",
		JWTTTL:              10 * time.Minute,
		JWTPublicKeyFile:    "other-public.pem",
		JWTPrivateKeyFile:   "other-private.pem",
	}
	if got != want {
		t.Errorf("loadServer() = %+v, want %+v", got, want)
	}
}

func TestLoadServer_InvalidConfig(t *testing.T) {
	tests := []struct {
		name    string
		unset   string
		set     map[string]string
		wantErr string
	}{
		{name: "missing database URL", unset: "DATABASE_URL", wantErr: "DATABASE_URL is required"},
		{name: "empty database URL", set: map[string]string{"DATABASE_URL": ""}, wantErr: "DATABASE_URL is required"},
		{name: "blank database URL", set: map[string]string{"DATABASE_URL": " \t"}, wantErr: "DATABASE_URL is required"},
		{name: "missing public key", unset: "JWT_PUBLIC_KEY_FILE", wantErr: "JWT_PUBLIC_KEY_FILE is required"},
		{name: "blank public key", set: map[string]string{"JWT_PUBLIC_KEY_FILE": " \t"}, wantErr: "JWT_PUBLIC_KEY_FILE is required"},
		{name: "missing private key", unset: "JWT_PRIVATE_KEY_FILE", wantErr: "JWT_PRIVATE_KEY_FILE is required"},
		{name: "blank private key", set: map[string]string{"JWT_PRIVATE_KEY_FILE": " \t"}, wantErr: "JWT_PRIVATE_KEY_FILE is required"},
		{name: "empty HTTP address", set: map[string]string{"HTTP_ADDR": ""}, wantErr: "HTTP_ADDR variable is empty"},
		{name: "empty request timeout", set: map[string]string{"HTTP_REQUEST_TIMEOUT": ""}, wantErr: "HTTP_REQUEST_TIMEOUT variable is empty"},
		{name: "invalid request timeout", set: map[string]string{"HTTP_REQUEST_TIMEOUT": "invalid"}, wantErr: "parse HTTP_REQUEST_TIMEOUT"},
		{name: "zero request timeout", set: map[string]string{"HTTP_REQUEST_TIMEOUT": "0s"}, wantErr: "HTTP_REQUEST_TIMEOUT must be > 0"},
		{name: "negative request timeout", set: map[string]string{"HTTP_REQUEST_TIMEOUT": "-1s"}, wantErr: "HTTP_REQUEST_TIMEOUT must be > 0"},
		{name: "empty HTTP shutdown timeout", set: map[string]string{"HTTP_SHUTDOWN_TIMEOUT": ""}, wantErr: "HTTP_SHUTDOWN_TIMEOUT variable is empty"},
		{name: "invalid HTTP shutdown timeout", set: map[string]string{"HTTP_SHUTDOWN_TIMEOUT": "invalid"}, wantErr: "parse HTTP_SHUTDOWN_TIMEOUT"},
		{name: "zero HTTP shutdown timeout", set: map[string]string{"HTTP_SHUTDOWN_TIMEOUT": "0s"}, wantErr: "HTTP_SHUTDOWN_TIMEOUT must be > 0"},
		{name: "negative HTTP shutdown timeout", set: map[string]string{"HTTP_SHUTDOWN_TIMEOUT": "-1s"}, wantErr: "HTTP_SHUTDOWN_TIMEOUT must be > 0"},
		{name: "equal shutdown timeouts", set: map[string]string{"SHUTDOWN_TIMEOUT": "5s"}, wantErr: "SHUTDOWN_TIMEOUT must be greater than HTTP_SHUTDOWN_TIMEOUT"},
		{name: "shorter global shutdown timeout", set: map[string]string{"SHUTDOWN_TIMEOUT": "4s"}, wantErr: "SHUTDOWN_TIMEOUT must be greater than HTTP_SHUTDOWN_TIMEOUT"},
		{name: "empty reservation TTL", set: map[string]string{"RESERVATION_TTL": ""}, wantErr: "RESERVATION_TTL variable is empty"},
		{name: "invalid reservation TTL", set: map[string]string{"RESERVATION_TTL": "invalid"}, wantErr: "parse RESERVATION_TTL"},
		{name: "zero reservation TTL", set: map[string]string{"RESERVATION_TTL": "0s"}, wantErr: "RESERVATION_TTL must be > 0"},
		{name: "negative reservation TTL", set: map[string]string{"RESERVATION_TTL": "-1s"}, wantErr: "RESERVATION_TTL must be > 0"},
		{name: "empty JWT TTL", set: map[string]string{"JWT_TTL": ""}, wantErr: "JWT_TTL variable is empty"},
		{name: "invalid JWT TTL", set: map[string]string{"JWT_TTL": "invalid"}, wantErr: "parse JWT_TTL"},
		{name: "zero JWT TTL", set: map[string]string{"JWT_TTL": "0s"}, wantErr: "JWT_TTL must be > 0"},
		{name: "negative JWT TTL", set: map[string]string{"JWT_TTL": "-1s"}, wantErr: "JWT_TTL must be > 0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := map[string]string{
				"DATABASE_URL":         "postgres://localhost/flashdrop",
				"JWT_PUBLIC_KEY_FILE":  "public.pem",
				"JWT_PRIVATE_KEY_FILE": "private.pem",
			}
			delete(env, tt.unset)
			for key, value := range tt.set {
				env[key] = value
			}

			_, err := loadServer(mapLookup(env))
			assertErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestLoadServer_WrapsError(t *testing.T) {
	t.Setenv("LOG_LEVEL", "invalid")

	_, err := LoadServer()
	assertErrorContains(t, err, "failed to load server config")
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
