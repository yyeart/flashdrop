package httpapi

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/yyeart/flashdrop/internal/identity"
)

const (
	requestIDHeader  = "X-Request-ID"
	retryAfterHeader = "Retry-After"
)

func NewRequestID(generator RequestIDGenerator) (Middleware, error) {
	if generator == nil {
		return nil, ErrNilRequestIDGenerator
	}

	middleware := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := generator().String()

			ctx := context.WithValue(r.Context(), requestKey, id)

			w.Header().Set(requestIDHeader, id)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}

	return middleware, nil
}

func NewLogging(logger *slog.Logger) (Middleware, error) {
	if logger == nil {
		return nil, ErrNilLogger
	}

	middleware := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			startedAt := time.Now()
			state := ensureResponseState(w)
			ctx := r.Context()
			// Share the completion log with the outer recovery middleware.
			state.completionLog = func() {
				duration := time.Since(startedAt)
				requestID, _ := requestIDFromContext(ctx)
				status := state.status
				if !state.Written() {
					status = http.StatusOK
				}

				logger.InfoContext(
					ctx,
					"HTTP request completed",
					slog.String("request_id", requestID),
					slog.String("method", r.Method),
					slog.String("path", r.URL.Path),
					slog.Int("status", status),
					slog.Int("bytes", state.bytes),
					slog.Duration("duration", duration),
				)
			}

			next.ServeHTTP(state, r)
			state.logRequestCompletion()
		})
	}

	return middleware, nil
}

func NewRecovery(logger *slog.Logger) (Middleware, error) {
	if logger == nil {
		return nil, ErrNilLogger
	}

	middleware := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			state := ensureResponseState(w)
			defer recoverRequest(
				logger, state, r,
			)

			next.ServeHTTP(state, r)
		})
	}

	return middleware, nil
}

func NewTimeout(
	timeout time.Duration,
	logger *slog.Logger,
) (Middleware, error) {
	if timeout <= 0 {
		return nil, ErrInvalidTimeout
	}

	if logger == nil {
		return nil, ErrNilLogger
	}

	middleware := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, cancel := context.WithTimeout(r.Context(), timeout)
			defer cancel()
			timedRequest := r.WithContext(ctx)

			state := ensureResponseState(w)

			next.ServeHTTP(state, timedRequest)

			if !errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return
			}

			if state.Written() {
				return
			}

			writeErrorAndLog(
				logger, state, timedRequest,
				http.StatusGatewayTimeout,
				codeDeadlineExceeded,
				"Request deadline exceeded",
			)
		})
	}

	return middleware, nil
}

func NewRateLimit(limiter RateLimiter, logger *slog.Logger) (Middleware, error) {
	if limiter == nil {
		return nil, ErrNilRateLimiter
	}

	if logger == nil {
		return nil, ErrNilLogger
	}

	middleware := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			serveRateLimited(limiter, logger, next, w, r)
		})
	}

	return middleware, nil
}

func NewAuthentication(
	authenticator Authenticator,
	logger *slog.Logger,
) (Middleware, error) {
	if authenticator == nil {
		return nil, ErrNilAuthenticator
	}

	if logger == nil {
		return nil, ErrNilLogger
	}

	middleware := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			serveAuthenticated(authenticator, logger, next, w, r)
		})
	}

	return middleware, nil
}

func NewRBAC(
	logger *slog.Logger,
	allowedRoles ...identity.Role,
) (Middleware, error) {
	if logger == nil {
		return nil, ErrNilLogger
	}

	if len(allowedRoles) == 0 {
		return nil, ErrNoAllowedRoles
	}

	allowed := make(map[identity.Role]struct{}, len(allowedRoles))

	for _, role := range allowedRoles {
		if !role.IsUserOrAdmin() {
			return nil, fmt.Errorf(
				"invalid allowed role: %q: %w",
				role, ErrInvalidAllowedRole,
			)
		}

		allowed[role] = struct{}{}
	}

	middleware := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			state := ensureResponseState(w)

			principal, err := principalFromContext(r.Context())
			if err != nil {
				writeErrorAndLog(
					logger, state, r,
					http.StatusUnauthorized, codeUnauthorized, "Unauthorized",
				)
				return
			}

			if _, ok := allowed[principal.Role]; ok {
				next.ServeHTTP(state, r)
				return
			}

			writeErrorAndLog(
				logger, state, r,
				http.StatusForbidden,
				codeForbidden,
				"Forbidden",
			)

		})
	}

	return middleware, nil
}
