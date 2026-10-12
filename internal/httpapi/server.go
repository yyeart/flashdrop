package httpapi

import (
	"context"
	"errors"
	"net"
	"net/http"
	"time"
)

type Server struct {
	httpServer      *http.Server
	listener        net.Listener
	shutdownTimeout time.Duration
}

func NewServer(
	listener net.Listener,
	handler http.Handler,
	shutdownTimeout time.Duration,
) (*Server, error) {
	if listener == nil {
		return nil, ErrNilListener
	}

	if handler == nil {
		return nil, ErrNilHandler
	}

	if shutdownTimeout <= 0 {
		return nil, ErrInvalidTimeout
	}

	server := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}

	return &Server{
		httpServer:      server,
		listener:        listener,
		shutdownTimeout: shutdownTimeout,
	}, nil
}

func (s *Server) Run(ctx context.Context) error {
	serveResult := make(chan error, 1)

	go func() {
		serveResult <- s.httpServer.Serve(s.listener)
	}()

	select {
	case err := <-serveResult:
		shutdownCtx, cancel := context.WithTimeout(
			context.WithoutCancel(ctx),
			s.shutdownTimeout,
		)
		defer cancel()

		shutdownErr := s.httpServer.Shutdown(shutdownCtx)
		var closeErr error
		if shutdownErr != nil {
			closeErr = s.httpServer.Close()
		}

		return errors.Join(err, shutdownErr, closeErr)

	case <-ctx.Done():
		var (
			shutdownErr error
			closeErr    error
			serveErr    error
		)

		shutdownCtx, cancel := context.WithTimeout(
			context.WithoutCancel(ctx),
			s.shutdownTimeout,
		)
		defer cancel()

		shutdownErr = s.httpServer.Shutdown(shutdownCtx)
		if shutdownErr != nil {
			closeErr = s.httpServer.Close()
		}

		rawServeErr := <-serveResult

		if rawServeErr != nil && !errors.Is(rawServeErr, http.ErrServerClosed) {
			serveErr = rawServeErr
		}

		return errors.Join(shutdownErr, closeErr, serveErr)
	}
}
