package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/yyeart/flashdrop/internal/identity"
)

type completionCase struct {
	name   string
	status int
	body   string
	panics bool
}

func (tc completionCase) handler(t *testing.T) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if tc.status != 0 {
			w.WriteHeader(tc.status)
		}
		if tc.body != "" {
			if _, err := w.Write([]byte(tc.body)); err != nil {
				t.Fatal(err)
			}
		}
		if tc.panics {
			panic("private panic value")
		}
	})
}

func TestLogging_CompletionAfterRecovery(t *testing.T) {
	cases := []completionCase{
		{"empty response", 0, "", false},
		{"implicit status", 0, "hello", false},
		{"explicit status", http.StatusAccepted, "hello", false},
		{"panic before response", 0, "", true},
		{"panic after headers", http.StatusAccepted, "", true},
		{"panic after implicit status", 0, "partial", true},
		{"panic after body", http.StatusAccepted, "partial", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			logger, output := testLogger()
			recovery, err := NewRecovery(logger)
			recovery = mustMiddleware(t, recovery, err)
			logging, err := NewLogging(logger)
			logging = mustMiddleware(t, logging, err)
			timeout, err := NewTimeout(time.Second, logger)
			timeout = mustMiddleware(t, timeout, err)
			limiter, err := NewRateLimit(AllowAllRateLimiter{}, logger)
			limiter = mustMiddleware(t, limiter, err)

			h := Chain(tc.handler(t), recovery, requestIDMiddleware(t), logging, timeout, limiter)

			r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/test?secret=hidden", nil)
			w := httptest.NewRecorder()
			startedAt := time.Now()
			h.ServeHTTP(w, r)
			elapsed := time.Since(startedAt)

			wantStatus := tc.status
			if wantStatus == 0 {
				wantStatus = http.StatusOK
			}
			if tc.panics && tc.status == 0 && tc.body == "" {
				wantStatus = http.StatusInternalServerError
				assertErrorResponse(t, w, wantStatus, "internal_error")
			} else if w.Body.String() != tc.body {
				t.Fatalf("body = %q, want %q", w.Body.String(), tc.body)
			}
			if w.Code != wantStatus {
				t.Fatalf("status = %d, want %d", w.Code, wantStatus)
			}

			assertCompletionLog(t, output, w, elapsed, tc.panics)
		})
	}
}

func TestRecovery_InvalidStatusWritesInternalError(t *testing.T) {
	logger, output := testLogger()
	recovery, err := NewRecovery(logger)
	recovery = mustMiddleware(t, recovery, err)
	logging, err := NewLogging(logger)
	logging = mustMiddleware(t, logging, err)
	timeout, err := NewTimeout(time.Second, logger)
	timeout = mustMiddleware(t, timeout, err)
	limiter, err := NewRateLimit(AllowAllRateLimiter{}, logger)
	limiter = mustMiddleware(t, limiter, err)
	authentication, err := NewAuthentication(authenticatorFunc(func(context.Context, string) (identity.AuthenticateResult, error) {
		return identity.AuthenticateResult{Role: identity.RoleAdmin}, nil
	}), logger)
	authentication = mustMiddleware(t, authentication, err)
	rbac, err := NewRBAC(logger, identity.RoleAdmin)
	rbac = mustMiddleware(t, rbac, err)

	h := Chain(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(99)
	}), recovery, requestIDMiddleware(t), logging, timeout, limiter, authentication, rbac)
	r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/test?secret=hidden", nil)
	r.Header.Set("Authorization", "Bearer token")
	w := httptest.NewRecorder()
	startedAt := time.Now()

	h.ServeHTTP(w, r)

	assertErrorResponse(t, w, http.StatusInternalServerError, "internal_error")
	assertCompletionLog(t, output, w, time.Since(startedAt), true)
}

type completionRecord struct {
	Message   string         `json:"msg"`
	RequestID string         `json:"request_id"`
	Method    string         `json:"method"`
	Path      string         `json:"path"`
	Status    int            `json:"status"`
	Bytes     int            `json:"bytes"`
	Duration  *time.Duration `json:"duration"`
}

func assertCompletionRecord(t *testing.T, record completionRecord, w *httptest.ResponseRecorder, elapsed time.Duration) {
	t.Helper()
	if record.RequestID != foundationRequestID || record.Method != http.MethodPost || record.Path != "/test" {
		t.Errorf("wrong request metadata: %+v", record)
	}
	if record.Status != w.Code || record.Bytes != w.Body.Len() {
		t.Errorf("logged status/bytes = %d/%d, response = %d/%d", record.Status, record.Bytes, w.Code, w.Body.Len())
	}
	if record.Duration == nil || *record.Duration <= 0 || *record.Duration > elapsed {
		t.Errorf("invalid duration: %v (request elapsed %v)", record.Duration, elapsed)
	}
}

func assertCompletionLog(
	t *testing.T,
	output *bytes.Buffer,
	w *httptest.ResponseRecorder,
	elapsed time.Duration,
	panicked bool,
) {
	t.Helper()
	decoder := json.NewDecoder(output)
	completionCount, panicCount := 0, 0
	var lastMessage string
	for {
		var record completionRecord
		if err := decoder.Decode(&record); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			t.Fatal(err)
		}
		lastMessage = record.Message
		switch record.Message {
		case "Recovered from panic":
			panicCount++
		case "HTTP request completed":
			completionCount++
			assertCompletionRecord(t, record, w, elapsed)
		default:
			t.Errorf("unexpected log message: %q", record.Message)
		}
	}
	if completionCount != 1 || lastMessage != "HTTP request completed" {
		t.Errorf("completion logs = %d, last message = %q", completionCount, lastMessage)
	}
	wantPanicCount := 0
	if panicked {
		wantPanicCount = 1
	}
	if panicCount != wantPanicCount {
		t.Errorf("panic logs = %d, want %d", panicCount, wantPanicCount)
	}
}
