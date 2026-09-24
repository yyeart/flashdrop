package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/yyeart/flashdrop/internal/config"
	"github.com/yyeart/flashdrop/internal/identity"
)

func TestLoadSeedAdminInput(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		want    identity.SeedAdminInput
		wantErr string
	}{
		{name: "missing email", env: map[string]string{"SEED_ADMIN_PASSWORD": "secret-value"}, wantErr: "SEED_ADMIN_EMAIL is required"},
		{name: "empty email", env: map[string]string{"SEED_ADMIN_EMAIL": "", "SEED_ADMIN_PASSWORD": "secret-value"}, wantErr: "SEED_ADMIN_EMAIL is required"},
		{name: "blank email", env: map[string]string{"SEED_ADMIN_EMAIL": " \t\n", "SEED_ADMIN_PASSWORD": "secret-value"}, wantErr: "SEED_ADMIN_EMAIL is required"},
		{name: "missing password", env: map[string]string{"SEED_ADMIN_EMAIL": "admin@example.com"}, wantErr: "SEED_ADMIN_PASSWORD is required"},
		{name: "empty password", env: map[string]string{"SEED_ADMIN_EMAIL": "admin@example.com", "SEED_ADMIN_PASSWORD": ""}, wantErr: "SEED_ADMIN_PASSWORD is required"},
		{name: "preserve input for identity validation", env: map[string]string{"SEED_ADMIN_EMAIL": " Admin@Example.COM ", "SEED_ADMIN_PASSWORD": " secret-value "}, want: identity.SeedAdminInput{Email: " Admin@Example.COM ", Password: " secret-value "}},
		{name: "domain validation belongs to identity", env: map[string]string{"SEED_ADMIN_EMAIL": "invalid-email", "SEED_ADMIN_PASSWORD": "x"}, want: identity.SeedAdminInput{Email: "invalid-email", Password: "x"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := loadSeedAdminInput(func(key string) (string, bool) {
				value, exists := tt.env[key]
				return value, exists
			})
			if tt.wantErr != "" {
				assertSeedInputError(t, got, err, tt.wantErr)
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatal("input was changed or lost")
			}
		})
	}
}

func assertSeedInputError(t *testing.T, got identity.SeedAdminInput, err error, want string) {
	t.Helper()
	if err == nil || err.Error() != want {
		t.Fatalf("unexpected error: %v; want %q", err, want)
	}
	if got != (identity.SeedAdminInput{}) {
		t.Fatal("input must be empty on error")
	}
}

func TestRunSeedAdmin_ConfigurationErrors(t *testing.T) {
	for _, tt := range []struct{ name, url, prefix string }{
		{"missing database URL", "", "DATABASE_URL is required"},
		{"invalid database URL", "postgres://localhost:invalid/flashdrop", "open database:"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			err := runSeedAdmin(context.Background(), config.Config{DatabaseURL: tt.url}, identity.SeedAdminInput{})
			if err == nil || !strings.HasPrefix(err.Error(), tt.prefix) {
				t.Fatalf("error = %v, want prefix %q", err, tt.prefix)
			}
		})
	}
}

func TestRunSeedAdmin_CancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := runSeedAdmin(ctx, config.Config{DatabaseURL: "postgres://test:test@127.0.0.1:1/test?sslmode=disable"}, identity.SeedAdminInput{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}

// Run main in a child process because it calls os.Exit on CLI errors.
func TestMainCLIHelper(t *testing.T) {
	if os.Getenv("FLASHDROP_CLI_TEST_HELPER") != "1" {
		return
	}
	for i, arg := range os.Args {
		if arg == "--" {
			os.Args = append([]string{"flashdrop"}, os.Args[i+1:]...)
			main()
			return
		}
	}
	t.Fatal("missing helper argument separator")
}

func TestMainCLI(t *testing.T) {
	for _, tt := range []struct {
		name                                      string
		args                                      []string
		email, password, databaseURL, wantMessage string
	}{
		{name: "missing email", args: []string{"seed-admin"}, password: "secret-value", wantMessage: "SEED_ADMIN_EMAIL is required"},
		{name: "missing password", args: []string{"seed-admin"}, email: "admin@example.com", wantMessage: "SEED_ADMIN_PASSWORD is required"},
		{name: "missing database URL", args: []string{"seed-admin"}, email: "admin@example.com", password: "secret-value", wantMessage: "DATABASE_URL is required"},
		{name: "seed-admin does not require JWT keys", args: []string{"seed-admin"}, email: "admin@example.com", password: "secret-value", databaseURL: "postgres://localhost:invalid/flashdrop", wantMessage: "open database:"},
		{name: "unknown command", args: []string{"seed-admn"}, wantMessage: "unknown command"},
		{name: "unexpected argument", args: []string{"seed-admin", "unexpected"}, wantMessage: "unexpected argument"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			args := append([]string{"-test.run=^TestMainCLIHelper$", "--"}, tt.args...)
			cmd := exec.CommandContext(ctx, os.Args[0], args...) // #nosec G204 G702 -- runs this test binary with fixed test-case arguments, without a shell.
			// Do not inherit developer credentials or PostgreSQL settings.
			cmd.Env = []string{"FLASHDROP_CLI_TEST_HELPER=1", "LOG_LEVEL=info", "SHUTDOWN_TIMEOUT=1s", "DATABASE_URL=" + tt.databaseURL, "SEED_ADMIN_EMAIL=" + tt.email, "SEED_ADMIN_PASSWORD=" + tt.password}
			output, err := cmd.CombinedOutput()
			if ctx.Err() != nil {
				t.Fatal("CLI did not exit; it may have entered the server lifecycle")
			}
			var exitErr *exec.ExitError
			if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 {
				t.Fatalf("error = %v, want exit status 1", err)
			}
			if strings.Contains(string(output), "secret-value") {
				t.Fatal("CLI leaked the password")
			}
			if !strings.Contains(string(output), tt.wantMessage) {
				t.Fatalf("output = %s, want message %q", output, tt.wantMessage)
			}
		})
	}
}
