package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRateLimit_PassesRequestArgumentsAndIgnoresProxyHeaders(t *testing.T) {
	logger, _ := testLogger()
	limiterCalls := 0
	limiter := limiterFunc(func(ctx context.Context, method, path, ip string) (bool, time.Duration, error) {
		limiterCalls++
		if method != http.MethodPost || path != "/limited" || ip != "198.51.100.9" {
			t.Errorf("limiter arguments = (%q, %q, %q), want (POST, /limited, 198.51.100.9)", method, path, ip)
		}
		if id, ok := requestIDFromContext(ctx); !ok || id != foundationRequestID {
			t.Errorf("limiter request ID = %q, present = %v", id, ok)
		}
		return true, 0, nil
	})
	m, err := NewRateLimit(limiter, logger)
	rateLimit := mustMiddleware(t, m, err)
	handlerCalls := 0
	h := Chain(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		handlerCalls++
		w.WriteHeader(http.StatusNoContent)
	}), requestIDMiddleware(t), rateLimit)
	r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/limited?query=value", nil)
	r.RemoteAddr = "198.51.100.9:4242"
	r.Header.Set("X-Forwarded-For", "203.0.113.10")
	r.Header.Set("X-Real-IP", "203.0.113.11")
	w := httptest.NewRecorder()

	h.ServeHTTP(w, r)

	if limiterCalls != 1 || handlerCalls != 1 {
		t.Errorf("limiter calls = %d, handler calls = %d, want one each", limiterCalls, handlerCalls)
	}
	if w.Code != http.StatusNoContent {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNoContent)
	}
}

func TestWriteError_UnknownCodeDoesNotCommitResponse(t *testing.T) {
	w := httptest.NewRecorder()
	// A zero initial code distinguishes no WriteHeader call from an explicit 200.
	w.Code = 0
	state := ensureResponseState(w)

	err := writeError(state, http.StatusInternalServerError, errorCode("unknown"), "Internal server error", foundationRequestID)

	if !errors.Is(err, ErrInvalidErrorCode) {
		t.Fatalf("error = %v, want ErrInvalidErrorCode", err)
	}
	if state.Written() || w.Code != 0 {
		t.Errorf("response committed: Written = %v, status = %d", state.Written(), w.Code)
	}
	if len(w.Header()) != 0 || w.Body.Len() != 0 {
		t.Errorf("response changed: headers = %v, body = %q", w.Header(), w.Body.String())
	}
}
