package httpapi

import (
	"context"
	"errors"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"
)

const serverTestWait = 3 * time.Second

type serverRunHandle struct {
	cancel context.CancelFunc
	done   chan struct{}
	err    error
}

func startServerForTest(t *testing.T, server *Server) *serverRunHandle {
	t.Helper()

	ctx, cancel := context.WithCancel(t.Context())
	handle := &serverRunHandle{
		cancel: cancel,
		done:   make(chan struct{}),
	}
	go func() {
		handle.err = server.Run(ctx)
		close(handle.done)
	}()

	t.Cleanup(func() {
		cancel()
		select {
		case <-handle.done:
		case <-time.After(serverTestWait):
			t.Error("server Run did not finish during test cleanup")
		}
	})

	return handle
}

func (h *serverRunHandle) wait(t *testing.T) error {
	t.Helper()

	select {
	case <-h.done:
		return h.err
	case <-time.After(serverTestWait):
		t.Fatal("server Run did not finish")
		return nil
	}
}

func listenForServerTest(t *testing.T) net.Listener {
	t.Helper()

	var listenConfig net.ListenConfig
	listener, err := listenConfig.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() {
		if err := listener.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			t.Errorf("close listener: %v", err)
		}
	})

	return listener
}

func newServerForTest(t *testing.T, listener net.Listener, handler http.Handler, timeout time.Duration) *Server {
	t.Helper()

	server, err := NewServer(listener, handler, timeout)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	return server
}

func serverTestGet(ctx context.Context, address string) (int, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+address, nil)
	if err != nil {
		return 0, err
	}

	client := http.Client{Timeout: serverTestWait}
	response, err := client.Do(request)
	if err != nil {
		return 0, err
	}

	return response.StatusCode, response.Body.Close()
}

func TestNewServer_RejectsInvalidArguments(t *testing.T) {
	listener := listenForServerTest(t)
	handler := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})

	tests := []struct {
		name     string
		listener net.Listener
		handler  http.Handler
		timeout  time.Duration
		wantErr  error
	}{
		{"nil listener", nil, handler, time.Second, ErrNilListener},
		{"nil handler", listener, nil, time.Second, ErrNilHandler},
		{"zero timeout", listener, handler, 0, ErrInvalidTimeout},
		{"negative timeout", listener, handler, -time.Second, ErrInvalidTimeout},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server, err := NewServer(test.listener, test.handler, test.timeout)
			if server != nil || !errors.Is(err, test.wantErr) {
				t.Errorf("NewServer() = (%v, %v), want (nil, %v)", server, err, test.wantErr)
			}
		})
	}
}

func TestServerRun_GracefulShutdownWaitsForActiveRequest(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseRequest := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(releaseRequest)

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(started)
		<-release
		w.WriteHeader(http.StatusNoContent)
	})
	listener := listenForServerTest(t)
	server := newServerForTest(t, listener, handler, time.Second)
	run := startServerForTest(t, server)

	type response struct {
		status int
		err    error
	}
	responseResult := make(chan response, 1)
	go func() {
		status, err := serverTestGet(t.Context(), listener.Addr().String())
		responseResult <- response{status: status, err: err}
	}()

	select {
	case <-started:
	case <-time.After(serverTestWait):
		t.Fatal("handler did not start")
	}

	run.cancel()
	select {
	case <-run.done:
		t.Fatalf("Run returned before the active request finished: %v", run.err)
	case <-time.After(30 * time.Millisecond):
	}

	releaseRequest()
	select {
	case result := <-responseResult:
		if result.err != nil || result.status != http.StatusNoContent {
			t.Errorf("request = (%d, %v), want (%d, nil)", result.status, result.err, http.StatusNoContent)
		}
	case <-time.After(serverTestWait):
		t.Fatal("request did not finish")
	}
	if err := run.wait(t); err != nil {
		t.Errorf("Run() error = %v, want nil", err)
	}
}

func TestServerRun_ShutdownTimeoutClosesActiveConnection(t *testing.T) {
	started := make(chan struct{})
	requestCanceled := make(chan struct{})
	handler := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
		close(requestCanceled)
	})
	listener := listenForServerTest(t)
	server := newServerForTest(t, listener, handler, 50*time.Millisecond)
	run := startServerForTest(t, server)

	requestResult := make(chan error, 1)
	go func() {
		_, err := serverTestGet(t.Context(), listener.Addr().String())
		requestResult <- err
	}()

	select {
	case <-started:
	case <-time.After(serverTestWait):
		t.Fatal("handler did not start")
	}
	run.cancel()

	if err := run.wait(t); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Run() error = %v, want context deadline exceeded", err)
	}
	select {
	case <-requestCanceled:
	case <-time.After(serverTestWait):
		t.Error("active request context was not canceled")
	}
	select {
	case <-requestResult:
	case <-time.After(serverTestWait):
		t.Error("client request did not finish")
	}
}

type failAfterFirstAcceptListener struct {
	net.Listener
	fail       <-chan struct{}
	acceptErr  error
	accepted   bool
	failedDone chan struct{}
}

func (l *failAfterFirstAcceptListener) Accept() (net.Conn, error) {
	if !l.accepted {
		l.accepted = true
		return l.Listener.Accept()
	}
	<-l.fail
	close(l.failedDone)
	return nil, l.acceptErr
}

func TestServerRun_ServeErrorWaitsForActiveRequest(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseRequest := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(releaseRequest)

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(started)
		<-release
		w.WriteHeader(http.StatusNoContent)
	})
	underlying := listenForServerTest(t)
	fail := make(chan struct{})
	var failOnce sync.Once
	failAccept := func() { failOnce.Do(func() { close(fail) }) }
	t.Cleanup(failAccept)
	wantErr := errors.New("accept failed")
	listener := &failAfterFirstAcceptListener{
		Listener:   underlying,
		fail:       fail,
		acceptErr:  wantErr,
		failedDone: make(chan struct{}),
	}
	server := newServerForTest(t, listener, handler, time.Second)
	run := startServerForTest(t, server)

	requestResult := make(chan error, 1)
	go func() {
		_, err := serverTestGet(t.Context(), underlying.Addr().String())
		requestResult <- err
	}()

	select {
	case <-started:
	case <-time.After(serverTestWait):
		t.Fatal("handler did not start")
	}
	failAccept()
	select {
	case <-listener.failedDone:
	case <-time.After(serverTestWait):
		t.Fatal("listener did not return the injected error")
	}
	select {
	case <-run.done:
		t.Fatalf("Run returned before the active request finished: %v", run.err)
	case <-time.After(30 * time.Millisecond):
	}

	releaseRequest()
	select {
	case err := <-requestResult:
		if err != nil {
			t.Errorf("active request failed: %v", err)
		}
	case <-time.After(serverTestWait):
		t.Fatal("active request did not finish")
	}
	if err := run.wait(t); !errors.Is(err, wantErr) {
		t.Errorf("Run() error = %v, want %v", err, wantErr)
	}
}

func TestServerRun_AlreadyCanceledContext(t *testing.T) {
	listener := listenForServerTest(t)
	server := newServerForTest(t, listener, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}), time.Second)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	result := make(chan error, 1)
	go func() { result <- server.Run(ctx) }()
	select {
	case err := <-result:
		if err != nil {
			t.Errorf("Run() error = %v, want nil", err)
		}
	case <-time.After(serverTestWait):
		t.Fatal("Run did not finish with an already canceled context")
	}
}

func TestServerRun_PreservesSpontaneousServerClosed(t *testing.T) {
	listener := listenForServerTest(t)
	server := newServerForTest(t, listener, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}), time.Second)
	if err := server.httpServer.Close(); err != nil {
		t.Fatalf("close server before Run: %v", err)
	}

	if err := server.Run(t.Context()); !errors.Is(err, http.ErrServerClosed) {
		t.Errorf("Run() error = %v, want http.ErrServerClosed", err)
	}
}
