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

type ServerConfig struct {
	Config

	HTTPAddr            string
	HTTPRequestTimeout  time.Duration
	HTTPShutdownTimeout time.Duration
	ReservationTTL      time.Duration

	JWTIssuer         string
	JWTAudience       string
	JWTTTL            time.Duration
	JWTPublicKeyFile  string
	JWTPrivateKeyFile string
}

func Load() (Config, error) {
	cfg, err := load(os.LookupEnv)
	if err != nil {
		return Config{}, fmt.Errorf("failed to load config: %w", err)
	}

	return cfg, nil
}

func LoadServer() (ServerConfig, error) {
	cfg, err := loadServer(os.LookupEnv)
	if err != nil {
		return ServerConfig{}, fmt.Errorf(
			"failed to load server config: %w",
			err,
		)
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

func loadServer(lookup func(string) (string, bool)) (ServerConfig, error) {
	base, err := load(lookup)
	if err != nil {
		return ServerConfig{}, err
	}

	if strings.TrimSpace(base.DatabaseURL) == "" {
		return ServerConfig{}, errors.New("DATABASE_URL is required")
	}

	rawHTTPAddr, exists := lookup("HTTP_ADDR")
	if !exists {
		rawHTTPAddr = "127.0.0.1:8080"
	} else if rawHTTPAddr == "" {
		return ServerConfig{}, errors.New("HTTP_ADDR variable is empty")
	}

	rawJWTIssuer, exists := lookup("JWT_ISSUER")
	if !exists {
		rawJWTIssuer = ""
	}

	rawJWTAudience, exists := lookup("JWT_AUDIENCE")
	if !exists {
		rawJWTAudience = ""
	}

	jwtPublicKeyFile, err := requiredString(lookup, "JWT_PUBLIC_KEY_FILE")
	if err != nil {
		return ServerConfig{}, err
	}

	jwtPrivateKeyFile, err := requiredString(lookup, "JWT_PRIVATE_KEY_FILE")
	if err != nil {
		return ServerConfig{}, err
	}

	httpRequestTimeout, err := duration(
		lookup,
		"HTTP_REQUEST_TIMEOUT",
		5*time.Second,
	)
	if err != nil {
		return ServerConfig{}, err
	}

	httpShutdownTimeout, err := duration(
		lookup,
		"HTTP_SHUTDOWN_TIMEOUT",
		5*time.Second,
	)
	if err != nil {
		return ServerConfig{}, err
	}

	if base.ShutdownTimeout <= httpShutdownTimeout {
		return ServerConfig{}, errors.New(
			"SHUTDOWN_TIMEOUT must be greater than HTTP_SHUTDOWN_TIMEOUT",
		)
	}

	reservationTTL, err := duration(
		lookup,
		"RESERVATION_TTL",
		15*time.Minute,
	)
	if err != nil {
		return ServerConfig{}, err
	}

	jwtTTL, err := duration(
		lookup,
		"JWT_TTL",
		15*time.Minute,
	)
	if err != nil {
		return ServerConfig{}, err
	}

	return ServerConfig{
		Config: base,

		HTTPAddr:            rawHTTPAddr,
		HTTPRequestTimeout:  httpRequestTimeout,
		HTTPShutdownTimeout: httpShutdownTimeout,
		ReservationTTL:      reservationTTL,

		JWTIssuer:         rawJWTIssuer,
		JWTAudience:       rawJWTAudience,
		JWTTTL:            jwtTTL,
		JWTPublicKeyFile:  jwtPublicKeyFile,
		JWTPrivateKeyFile: jwtPrivateKeyFile,
	}, nil
}

func duration(
	lookup func(string) (string, bool),
	name string,
	defaultValue time.Duration,
) (time.Duration, error) {
	raw, exists := lookup(name)
	if !exists {
		return defaultValue, nil
	}
	if raw == "" {
		return 0, fmt.Errorf("%s variable is empty", name)
	}

	value, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", name, err)
	}
	if value <= 0 {
		return 0, fmt.Errorf("%s must be > 0", name)
	}

	return value, nil
}

func requiredString(lookup func(string) (string, bool), name string) (string, error) {
	value, exists := lookup(name)
	if !exists || strings.TrimSpace(value) == "" {
		return "", fmt.Errorf(
			"%s is required",
			name,
		)
	}

	return value, nil
}
