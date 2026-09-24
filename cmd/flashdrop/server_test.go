package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yyeart/flashdrop/internal/config"
	"github.com/yyeart/flashdrop/internal/identity"
)

func TestRunServer_InvalidDatabaseURL(t *testing.T) {
	cfg := serverTestConfig("postgres://localhost:invalid/flashdrop")

	err := runServer(t.Context(), cfg, serverTestLogger())
	if err == nil {
		t.Fatal("runServer() error = nil for an invalid database URL")
	}
}

func TestRunServer_CanceledDatabasePing(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	cfg := serverTestConfig("postgres://test:test@127.0.0.1:1/test?sslmode=disable")

	err := runServer(ctx, cfg, serverTestLogger())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("runServer() error = %v, want context.Canceled", err)
	}
}

func TestRunServer_Integration_InvalidJWTKeys(t *testing.T) {
	cfg := serverTestConfig(serverTestDSN(t))
	cfg.JWTPublicKeyFile = filepath.Join(t.TempDir(), "missing-public.pem")
	cfg.JWTPrivateKeyFile = filepath.Join(t.TempDir(), "missing-private.pem")

	err := runServer(t.Context(), cfg, serverTestLogger())
	if !errors.Is(err, identity.ErrInvalidPublicKey) {
		t.Fatalf("runServer() error = %v, want identity.ErrInvalidPublicKey", err)
	}
}

func TestRunServer_Integration_OccupiedAddress(t *testing.T) {
	cfg := serverTestConfig(serverTestDSN(t))
	cfg.JWTPublicKeyFile, cfg.JWTPrivateKeyFile = serverTestKeys(t)

	var listenConfig net.ListenConfig
	listener, err := listenConfig.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve TCP address: %v", err)
	}
	t.Cleanup(func() {
		if err := listener.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			t.Errorf("close reserved listener: %v", err)
		}
	})
	cfg.HTTPAddr = listener.Addr().String()

	err = runServer(t.Context(), cfg, serverTestLogger())
	if !errors.Is(err, syscall.EADDRINUSE) {
		t.Fatalf("runServer() error = %v, want EADDRINUSE", err)
	}
}

func TestRunServer_Integration_StartAndShutdown(t *testing.T) {
	cfg := serverTestConfig(serverTestDSN(t))
	cfg.JWTPublicKeyFile, cfg.JWTPrivateKeyFile = serverTestKeys(t)
	cfg.HTTPAddr = availableServerTestAddress(t)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	result := make(chan error, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		result <- runServer(ctx, cfg, serverTestLogger())
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("runServer() did not stop during test cleanup")
		}
	})

	response := awaitServerResponse(t, cfg.HTTPAddr, result)
	assertSaleListResponse(t, response)

	cancel()
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("runServer() shutdown error = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("runServer() did not stop after context cancellation")
	}
}

type serverHTTPResponse struct {
	status int
	header http.Header
	body   []byte
}

func awaitServerResponse(t *testing.T, address string, result <-chan error) serverHTTPResponse {
	t.Helper()
	client := &http.Client{Timeout: time.Second}
	deadline := time.Now().Add(5 * time.Second)
	for {
		request, err := http.NewRequestWithContext(
			t.Context(), http.MethodGet, "http://"+address+"/v1/sales", nil,
		)
		if err != nil {
			t.Fatalf("create HTTP request: %v", err)
		}
		response, requestErr := client.Do(request)
		if requestErr == nil {
			body, readErr := io.ReadAll(response.Body)
			closeErr := response.Body.Close()
			if readErr != nil || closeErr != nil {
				t.Fatalf("read HTTP response: %v; close: %v", readErr, closeErr)
			}
			return serverHTTPResponse{
				status: response.StatusCode,
				header: response.Header,
				body:   body,
			}
		}
		select {
		case err := <-result:
			t.Fatalf("runServer() stopped before accepting requests: %v", err)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatalf("HTTP server did not become ready: %v", requestErr)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func assertSaleListResponse(t *testing.T, response serverHTTPResponse) {
	t.Helper()
	if response.status != http.StatusOK {
		t.Fatalf("GET /v1/sales status = %d, body = %s", response.status, response.body)
	}
	if response.header.Get("X-Request-ID") == "" {
		t.Fatal("GET /v1/sales response has no request ID")
	}
	if !strings.Contains(response.header.Get("Content-Type"), "application/json") {
		t.Fatalf("GET /v1/sales content type = %q", response.header.Get("Content-Type"))
	}
}

func serverTestConfig(dsn string) config.ServerConfig {
	return config.ServerConfig{
		Config: config.Config{
			DatabaseURL:     dsn,
			ShutdownTimeout: 3 * time.Second,
		},
		HTTPAddr:            "127.0.0.1:0",
		HTTPRequestTimeout:  time.Second,
		HTTPShutdownTimeout: 2 * time.Second,
		ReservationTTL:      15 * time.Minute,
		JWTTTL:              15 * time.Minute,
	}
}

func serverTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func serverTestDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("FLASHDROP_TEST_DSN")
	if dsn == "" {
		t.Skip("FLASHDROP_TEST_DSN is not set")
	}

	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("open test PostgreSQL: %v", err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("ping test PostgreSQL: %v", err)
	}
	return dsn
}

func serverTestKeys(t *testing.T) (publicPath, privatePath string) {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate Ed25519 keys: %v", err)
	}
	publicDER, err := x509.MarshalPKIXPublicKey(publicKey)
	if err != nil {
		t.Fatalf("marshal public key: %v", err)
	}
	privateDER, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		t.Fatalf("marshal private key: %v", err)
	}

	dir := t.TempDir()
	publicPath = filepath.Join(dir, "public.pem")
	privatePath = filepath.Join(dir, "private.pem")
	if err := os.WriteFile(publicPath, pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: publicDER}), 0o600); err != nil {
		t.Fatalf("write public key: %v", err)
	}
	if err := os.WriteFile(privatePath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateDER}), 0o600); err != nil {
		t.Fatalf("write private key: %v", err)
	}
	return publicPath, privatePath
}

func availableServerTestAddress(t *testing.T) string {
	t.Helper()
	var listenConfig net.ListenConfig
	listener, err := listenConfig.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("allocate TCP address: %v", err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("release TCP address: %v", err)
	}
	return address
}
