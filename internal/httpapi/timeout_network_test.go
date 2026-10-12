package httpapi

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestTimeout_ClientNotReadingResponseDoesNotBlockHandler(t *testing.T) {
	const requestTimeout = 300 * time.Millisecond
	logger := slog.New(slog.DiscardHandler)
	middleware, err := NewTimeout(requestTimeout, logger)
	middleware = mustMiddleware(t, middleware, err)
	// Exceed socket buffers without relying on the application's data size.
	payload := strings.Repeat("x", 16<<20)
	started := make(chan time.Time, 1)
	finished := make(chan struct{})
	var writeErr error
	bounded := Chain(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		deadline, _ := r.Context().Deadline()
		started <- deadline
		writeErr = writeJson(w, http.StatusOK, payload)
	}), requestIDMiddleware(t), middleware)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(finished)
		bounded.ServeHTTP(w, r)
	})
	listener := listenForServerTest(t)
	server := newServerForTest(t, listener, handler, time.Second)
	startServerForTest(t, server)
	dialer := net.Dialer{Timeout: serverTestWait}
	connection, err := dialer.DialContext(t.Context(), "tcp", listener.Addr().String())
	if err != nil {
		t.Fatalf("dial server: %v", err)
	}
	t.Cleanup(func() {
		if err := connection.Close(); err != nil {
			t.Errorf("close connection: %v", err)
		}
		select {
		case <-finished:
		case <-time.After(serverTestWait):
			t.Error("handler did not finish during cleanup")
		}
	})
	if tcp, ok := connection.(*net.TCPConn); ok {
		if err := tcp.SetReadBuffer(1024); err != nil {
			t.Fatalf("set client read buffer: %v", err)
		}
	}
	if _, err := fmt.Fprint(connection, "GET / HTTP/1.1\r\nHost: localhost\r\n\r\n"); err != nil {
		t.Fatalf("send request: %v", err)
	}
	var deadline time.Time
	select {
	case deadline = <-started:
	case <-time.After(serverTestWait):
		t.Fatal("handler did not start")
	}
	// Do not read or close the client until the bounded handler has returned.
	// Include scheduling slack beyond the configured write deadline.
	timer := time.NewTimer(time.Until(deadline.Add(responseWriteGrace)) + time.Second)
	defer timer.Stop()
	select {
	case <-finished:
	case <-timer.C:
		t.Fatal("handler remained blocked after the response write deadline")
	}
	var timeoutErr net.Error
	if !errors.As(writeErr, &timeoutErr) || !timeoutErr.Timeout() {
		t.Errorf("response write error = %v, want a network timeout", writeErr)
	}
}

func TestTimeout_StalledRequestBodyReturnsGatewayTimeout(t *testing.T) {
	fixture := newRouterFixture(t)
	fixture.deps.RequestTimeout = 50 * time.Millisecond
	listener := listenForServerTest(t)
	server := newServerForTest(t, listener, fixture.handler(t), time.Second)
	startServerForTest(t, server)

	dialer := net.Dialer{Timeout: serverTestWait}
	connection, err := dialer.DialContext(t.Context(), "tcp", listener.Addr().String())
	if err != nil {
		t.Fatalf("dial server: %v", err)
	}
	t.Cleanup(func() {
		if err := connection.Close(); err != nil {
			t.Errorf("close connection: %v", err)
		}
	})
	if err := connection.SetDeadline(time.Now().Add(serverTestWait)); err != nil {
		t.Fatalf("set test connection deadline: %v", err)
	}

	// Leave the body incomplete while keeping the connection open.
	if _, err := fmt.Fprintf(connection,
		"POST /v1/auth/register HTTP/1.1\r\nHost: %s\r\nContent-Type: application/json\r\nContent-Length: 100\r\n\r\n{\"email\":",
		listener.Addr().String(),
	); err != nil {
		t.Fatalf("send request headers and partial body: %v", err)
	}

	request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "http://"+listener.Addr().String()+"/v1/auth/register", nil)
	if err != nil {
		t.Fatalf("create response request: %v", err)
	}
	response, err := http.ReadResponse(bufio.NewReader(connection), request)
	if err != nil {
		t.Fatalf("read timeout response: %v", err)
	}
	defer func() {
		if err := response.Body.Close(); err != nil {
			t.Errorf("close response body: %v", err)
		}
	}()
	assertNetworkTimeoutResponse(t, response)
}

func assertNetworkTimeoutResponse(t *testing.T, response *http.Response) {
	t.Helper()
	if response.StatusCode != http.StatusGatewayTimeout {
		t.Fatalf("status = %d, want 504", response.StatusCode)
	}
	if got := response.Header.Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}
	var envelope errorEnvelope
	if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
		t.Fatalf("decode timeout error: %v", err)
	}
	if envelope.Error.Code != codeDeadlineExceeded || envelope.Error.Message == "" {
		t.Errorf("unexpected timeout error: %+v", envelope.Error)
	}
	if got := response.Header.Get(requestIDHeader); got != foundationRequestID || envelope.Error.RequestID != got {
		t.Errorf("request IDs: header=%q, body=%q, want %q", got, envelope.Error.RequestID, foundationRequestID)
	}
}
