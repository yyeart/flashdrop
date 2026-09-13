package config

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"
)

type Config struct {
	LogLevel        slog.Level
	ShutdownTimeout time.Duration
	DatabaseURL     string
}

func Load() (Config, error) {
	cfg, err := load(os.LookupEnv)
	if err != nil {
		return Config{}, fmt.Errorf("failed to load config: %w", err)
	}

	return cfg, nil
}

func load(lookup func(string) (string, bool)) (Config, error) {
	rawLevel, exists := lookup("LOG_LEVEL")
	if !exists {
		rawLevel = "info"
	} else if rawLevel == "" {
		return Config{}, errors.New("LOG_LEVEL variable is empty")
	}

	var level slog.Level
	switch strings.ToUpper(rawLevel) {
	case "DEBUG":
		level = slog.LevelDebug
	case "INFO":
		level = slog.LevelInfo
	case "WARN":
		level = slog.LevelWarn
	case "ERROR":
		level = slog.LevelError
	default:
		return Config{}, fmt.Errorf("unknown LOG_LEVEL log level: %s", rawLevel)
	}

	rawTimeout, exists := lookup("SHUTDOWN_TIMEOUT")
	if !exists {
		rawTimeout = "10s"
	} else if rawTimeout == "" {
		return Config{}, errors.New("SHUTDOWN_TIMEOUT variable is empty")
	}

	timeout, err := time.ParseDuration(rawTimeout)
	if err != nil {
		return Config{}, fmt.Errorf("error parsing SHUTDOWN_TIMEOUT duration: %w", err)
	} else if timeout <= 0 {
		return Config{}, errors.New("SHUTDOWN_TIMEOUT duration must be > 0")
	}

	databaseURL, exists := lookup("DATABASE_URL")
	if !exists {
		databaseURL = ""
	}

	return Config{
		LogLevel:        level,
		ShutdownTimeout: timeout,
		DatabaseURL:     databaseURL,
	}, nil
}
