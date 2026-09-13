package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yyeart/flashdrop/internal/app"
	"github.com/yyeart/flashdrop/internal/appLogger"
	"github.com/yyeart/flashdrop/internal/config"
	"github.com/yyeart/flashdrop/internal/identity"
	identity_postgres "github.com/yyeart/flashdrop/internal/identity/postgres"
)

func main() {
	ctx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()

	btLogger := appLogger.NewLogger(slog.LevelInfo, os.Stderr)

	config, err := config.Load()
	if err != nil {
		btLogger.Error("failed to load config", "err", err)

		os.Exit(1)
	}

	logger := appLogger.NewLogger(config.LogLevel, os.Stderr)
	slog.SetDefault(logger)

	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "seed-admin":
			if len(os.Args) > 2 {
				logger.Error("unexpected argument")
				os.Exit(1)
			}

			input, err := loadSeedAdminInput(os.LookupEnv)
			if err != nil {
				logger.Error("invalid seed-admin configuration", "err", err)
				os.Exit(1)
			}

			if err := runSeedAdmin(ctx, config, input); err != nil {
				logger.Error("seed admin failed", "err", err)
				os.Exit(1)
			}

			return

		default:
			logger.Error("unknown command")
			os.Exit(1)
		}
	}

	if err := run(
		ctx,
		config.ShutdownTimeout,
		func(ctx context.Context) error {
			<-ctx.Done()
			return nil
		},
	); err != nil {
		logger.Error("worker error", "err", err)

		os.Exit(1)
	}
}

func run(
	ctx context.Context,
	shutdownTimeout time.Duration,
	workers ...app.Worker,
) error {

	if err := app.Run(ctx, shutdownTimeout, workers...); err != nil {
		return err
	}

	return nil
}

func runSeedAdmin(
	ctx context.Context,
	cfg config.Config,
	input identity.SeedAdminInput,
) error {
	if cfg.DatabaseURL == "" {
		return errors.New("DATABASE_URL is required for seed-admin")
	}

	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		return fmt.Errorf("ping database: %w", err)
	}

	store := identity_postgres.NewStore(pool)
	seeder := identity.NewAdminSeeder(store)

	return seeder.SeedAdmin(ctx, input)
}

func loadSeedAdminInput(
	lookup func(string) (string, bool),
) (identity.SeedAdminInput, error) {
	email, exists := lookup("SEED_ADMIN_EMAIL")
	if !exists || strings.TrimSpace(email) == "" {
		return identity.SeedAdminInput{}, errors.New(
			"SEED_ADMIN_EMAIL is required",
		)
	}

	password, exists := lookup("SEED_ADMIN_PASSWORD")
	if !exists || password == "" {
		return identity.SeedAdminInput{}, errors.New(
			"SEED_ADMIN_PASSWORD is required",
		)
	}

	return identity.SeedAdminInput{
		Email:    email,
		Password: password,
	}, nil
}
