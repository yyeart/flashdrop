package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"net"
	"net/http"
	"runtime/debug"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/yyeart/flashdrop/internal/identity"
)

type Middleware func(http.Handler) http.Handler

type responseState struct {
	http.ResponseWriter

	status      int
	bytes       int
	wroteHeader bool

	completionLog func()
}

type RequestIDGenerator func() uuid.UUID

func (w *responseState) WriteHeader(status int) {
	if w.wroteHeader {
		return
	}

	w.ResponseWriter.WriteHeader(status)
	w.status = status
	w.wroteHeader = true
}

func (w *responseState) Write(data []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}

	n, err := w.ResponseWriter.Write(data)
	w.bytes += n

	return n, err
}

func (w *responseState) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

func (w *responseState) Written() bool {
	return w.wroteHeader
}

func (w *responseState) Status() int {
	return w.status
}

func (w *responseState) Bytes() int {
	return w.bytes
}

func (w *responseState) logRequestCompletion() {
	if w.completionLog == nil {
		return
	}

	logCompletion := w.completionLog
	w.completionLog = nil
	logCompletion()
}

func Chain(handler http.Handler, middlewares ...Middleware) http.Handler {
	for i := len(middlewares) - 1; i >= 0; i-- {
		handler = middlewares[i](handler)
	}

	return handler
}

func ensureResponseState(w http.ResponseWriter) *responseState {
	if state, ok := w.(*responseState); ok {
		return state
	}

	return &responseState{ResponseWriter: w}
}

func requestIDForResponse(
	ctx context.Context, w http.ResponseWriter,
) string {
	requestID, ok := requestIDFromContext(ctx)
	if !ok {
		requestID = w.Header().Get(requestIDHeader)
	}

	return requestID
}

func clientIPFromRemoteAddr(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return remoteAddr
	}

	return host
}

func recoverRequest(
	logger *slog.Logger,
	state *responseState,
	r *http.Request,
) {
	panicValue := recover()
	if panicValue == nil {
		return
	}
	// Final response status and bytes are known only after recovery finishes.
	defer state.logRequestCompletion()

	ctx := r.Context()
	requestID := requestIDForResponse(ctx, state)
	logger.ErrorContext(
		ctx,
		"Recovered from panic",
		slog.String("request_id", requestID),
		slog.Any("panic", panicValue),
		slog.String("stack", string(debug.Stack())),
		slog.String("method", r.Method),
		slog.String("path", r.URL.Path),
	)

	if state.Written() {
		return
	}

	writeErrorAndLog(
		logger, state, r,
		http.StatusInternalServerError,
		codeInternalError,
		"Internal server error",
	)
}

func serveRateLimited(
	limiter RateLimiter,
	logger *slog.Logger,
	next http.Handler,
	w http.ResponseWriter,
	r *http.Request,
) {
	state := ensureResponseState(w)
	clientIP := clientIPFromRemoteAddr(r.RemoteAddr)

	allowed, retryAfter, err := limiter.Allow(
		r.Context(), r.Method, r.URL.Path, clientIP,
	)
	if err != nil {
		handleRateLimitError(
			logger, state, err, r, clientIP,
		)

		return
	}

	if allowed {
		next.ServeHTTP(state, r)

		return
	}

	seconds := int64(math.Ceil(retryAfter.Seconds()))
	if seconds < 0 {
		seconds = 0
	}
	state.Header().Set(retryAfterHeader, strconv.FormatInt(seconds, 10))

	writeErrorAndLog(
		logger, state, r,
		http.StatusTooManyRequests,
		codeRateLimitExceeded,
		"Rate limit exceeded",
	)
}

func handleRateLimitError(
	logger *slog.Logger,
	state *responseState,
	err error,
	r *http.Request,
	clientIP string,
) {
	ctx := r.Context()
	requestID := requestIDForResponse(ctx, state)
	logger.ErrorContext(
		ctx,
		"Allow error",
		slog.String("request_id", requestID),
		slog.String("method", r.Method),
		slog.String("path", r.URL.Path),
		slog.String("client_ip", clientIP),
		slog.String("error", err.Error()),
	)

	if errors.Is(ctx.Err(), context.Canceled) ||
		errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return
	}

	writeErrorAndLog(
		logger, state, r,
		http.StatusInternalServerError,
		codeInternalError,
		"Internal server error",
	)
}

func serveAuthenticated(
	authenticator Authenticator,
	logger *slog.Logger,
	next http.Handler,
	w http.ResponseWriter,
	r *http.Request,
) {
	state := ensureResponseState(w)

	token, ok := bearerToken(r.Header)
	if !ok {
		writeErrorAndLog(
			logger, state, r,
			http.StatusUnauthorized, codeUnauthorized, "Unauthorized",
		)

		return
	}

	result, err := authenticator.Authenticate(r.Context(), token)
	if err != nil {
		handleAuthenticationError(
			logger, state, err, r,
		)

		return
	}

	ctx := context.WithValue(r.Context(), principalKey, &result)
	next.ServeHTTP(state, r.WithContext(ctx))
}

func bearerToken(header http.Header) (string, bool) {
	values := header.Values("Authorization")
	if len(values) != 1 {
		return "", false
	}

	parts := strings.Fields(values[0])
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return "", false
	}

	return parts[1], parts[1] != ""
}

func handleAuthenticationError(
	logger *slog.Logger,
	state *responseState,
	err error,
	r *http.Request,
) {
	ctx := r.Context()
	requestID := requestIDForResponse(ctx, state)
	switch {
	case ctx.Err() != nil:
		return

	case errors.Is(err, context.DeadlineExceeded):
		writeErrorAndLog(
			logger, state, r,
			http.StatusGatewayTimeout,
			codeDeadlineExceeded,
			"Request deadline exceeded",
		)

	case errors.Is(err, identity.ErrInvalidToken):
		writeErrorAndLog(
			logger, state, r,
			http.StatusUnauthorized, codeUnauthorized, "Unauthorized",
		)

	default:
		logger.ErrorContext(
			ctx,
			"Authentication error",
			slog.String("error", err.Error()),
			slog.String("request_id", requestID),
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
		)

		writeErrorAndLog(
			logger, state, r,
			http.StatusInternalServerError,
			codeInternalError,
			"Internal server error",
		)
	}
}

func writeErrorAndLog(
	logger *slog.Logger,
	w http.ResponseWriter,
	r *http.Request,
	status int,
	code errorCode,
	message string,
) {
	ctx := r.Context()
	requestID := requestIDForResponse(ctx, w)
	if err := writeError(w, status, code, message, requestID); err != nil {
		logger.ErrorContext(
			ctx,
			"Failed to write error",
			slog.String("reason", err.Error()),
			slog.String("request_id", requestID),
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
		)
	}
}
