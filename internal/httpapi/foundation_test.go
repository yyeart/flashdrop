package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/yyeart/flashdrop/internal/identity"
)

const foundationRequestID = "00000000-0000-0000-0000-000000000123"

func mustMiddleware(t *testing.T, middleware Middleware, err error) Middleware {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	return middleware
}

func requestIDMiddleware(t *testing.T) Middleware {
	t.Helper()
	m, err := NewRequestID(func() uuid.UUID { return uuid.MustParse(foundationRequestID) })
	return mustMiddleware(t, m, err)
}

func testLogger() (*slog.Logger, *bytes.Buffer) {
	var output bytes.Buffer
	return slog.New(slog.NewJSONHandler(&output, nil)), &output
}

func assertErrorResponse(t *testing.T, recorder *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if recorder.Code != status {
		t.Fatalf("status = %d, want %d; body = %s", recorder.Code, status, recorder.Body)
	}
	if got := recorder.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q", got)
	}
	if got := recorder.Header().Get(requestIDHeader); got != foundationRequestID {
		t.Errorf("request ID header = %q", got)
	}
	// Decode into maps so extra fields forbidden by ErrorEnvelope cannot hide
	// behind the implementation's own Go types.
	var envelope map[string]map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	body := envelope["error"]
	if len(envelope) != 1 || len(body) != 3 {
		t.Fatalf("unexpected envelope fields: %#v", envelope)
	}
	if body["code"] != code || body["message"] == "" || body["request_id"] != foundationRequestID {
		t.Errorf("error body = %#v", body)
	}
}

func TestDecodeJSON_RejectsInvalidRequestsWithoutWritingResponse(t *testing.T) {
	cases := []struct {
		name         string
		contentTypes []string
		body         string
		want         error
	}{
		{"missing content type", nil, `{}`, ErrUnsupportedMediaType},
		{"wrong content type", []string{"text/plain"}, `{}`, ErrUnsupportedMediaType},
		{"duplicate content type", []string{"application/json", "application/json"}, `{}`, ErrUnsupportedMediaType},
		{"invalid content type", []string{"application/json; charset="}, `{}`, ErrUnsupportedMediaType},
		{"empty", []string{"application/json"}, "", ErrEmptyBody},
		{"whitespace", []string{"application/json"}, " \n\t", ErrEmptyBody},
		{"syntax", []string{"application/json"}, `{"name":!}`, ErrMalformedJSON},
		{"truncated", []string{"application/json"}, `{"name":`, ErrMalformedJSON},
		{"unknown field", []string{"application/json"}, `{"extra":true}`, ErrUnknownJSONField},
		{"wrong field type", []string{"application/json"}, `{"name":5}`, ErrInvalidJSONType},
		{"second value", []string{"application/json"}, `{} {}`, ErrMultipleJSONValues},
		{"trailing junk", []string{"application/json"}, `{} !`, ErrMalformedJSON},
		{"oversized string", []string{"application/json"}, `{"name":"` + strings.Repeat("x", int(maxJSONBodyBytes)) + `"}`, ErrBodyTooLarge},
		{"oversized trailing whitespace", []string{"application/json"}, `{}` + strings.Repeat(" ", int(maxJSONBodyBytes)), ErrBodyTooLarge},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", strings.NewReader(tc.body))
			r.Header["Content-Type"] = tc.contentTypes
			w := httptest.NewRecorder()
			state := ensureResponseState(w)
			var value struct {
				Name string `json:"name"`
			}
			if err := decodeJSON(state, r, &value); !errors.Is(err, tc.want) {
				t.Errorf("error = %v, want %v", err, tc.want)
			}
			if state.Written() || w.Body.Len() != 0 || len(w.Header()) != 0 {
				t.Error("decoder wrote a response")
			}
		})
	}
}

func TestDecodeJSON_AcceptsParametersAndExactBodyLimit(t *testing.T) {
	body := `{"name":"` + strings.Repeat("x", int(maxJSONBodyBytes)-len(`{"name":""}`)) + `"}`
	r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json; charset=utf-8")
	var value struct {
		Name string `json:"name"`
	}
	if err := decodeJSON(httptest.NewRecorder(), r, &value); err != nil {
		t.Fatal(err)
	}
	if len(value.Name) != int(maxJSONBodyBytes)-len(`{"name":""}`) {
		t.Fatal("decoded value was truncated")
	}
}

func TestWriteJSON_MarshalFailureDoesNotCommitResponse(t *testing.T) {
	w := httptest.NewRecorder()
	state := ensureResponseState(w)
	if err := writeJson(state, http.StatusCreated, make(chan int)); err == nil {
		t.Fatal("expected serialization error")
	}
	if state.Written() || w.Body.Len() != 0 || len(w.Header()) != 0 {
		t.Fatal("serialization failure changed response")
	}
	if err := writeError(state, http.StatusInternalServerError, "internal_error", "Internal server error", foundationRequestID); err != nil {
		t.Fatal(err)
	}
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("fallback status = %d", w.Code)
	}
}

func TestWriteError_MatchesEnvelopeAndRequestContext(t *testing.T) {
	w := httptest.NewRecorder()
	h := requestIDMiddleware(t)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, ok := requestIDFromContext(r.Context())
		if !ok {
			t.Fatal("missing context request ID")
		}
		if err := writeError(w, http.StatusBadRequest, "bad_request", "Invalid input", id); err != nil {
			t.Fatal(err)
		}
	}))
	h.ServeHTTP(w, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil))
	assertErrorResponse(t, w, http.StatusBadRequest, "bad_request")
}

func TestRecovery_ReturnsSafeErrorAndLogsStack(t *testing.T) {
	logger, output := testLogger()
	m, err := NewRecovery(logger)
	h := Chain(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("private panic value") }), mustMiddleware(t, m, err), requestIDMiddleware(t))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil))
	assertErrorResponse(t, w, http.StatusInternalServerError, "internal_error")
	if strings.Contains(w.Body.String(), "private panic value") {
		t.Fatal("panic leaked to client")
	}
	var record map[string]any
	if err := json.Unmarshal(output.Bytes(), &record); err != nil {
		t.Fatal(err)
	}
	if record["panic"] != "private panic value" || record["request_id"] != foundationRequestID || record["stack"] == "" || record["stack"] == nil {
		t.Errorf("panic log = %#v", record)
	}
}

func TestLogging_RecordsResponse(t *testing.T) {
	for _, status := range []int{0, http.StatusCreated} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			logger, output := testLogger()
			m, err := NewLogging(logger)
			h := Chain(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if status != 0 {
					w.WriteHeader(status)
				}
				if _, err := w.Write([]byte("hello")); err != nil {
					t.Fatal(err)
				}
			}), requestIDMiddleware(t), mustMiddleware(t, m, err))
			r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/test?secret=hidden", strings.NewReader("private body"))
			r.Header.Set("Authorization", "Bearer private-token")
			h.ServeHTTP(httptest.NewRecorder(), r)
			want := status
			if want == 0 {
				want = http.StatusOK
			}
			assertRequestLog(t, output, want)
		})
	}
}

func assertRequestLog(t *testing.T, output *bytes.Buffer, status int) {
	t.Helper()
	var record map[string]any
	if err := json.Unmarshal(output.Bytes(), &record); err != nil {
		t.Fatal(err)
	}
	for key, value := range map[string]any{"status": float64(status), "bytes": float64(5), "request_id": foundationRequestID, "method": "POST", "path": "/test"} {
		if record[key] != value {
			t.Errorf("log %s = %v, want %v", key, record[key], value)
		}
	}
	if _, ok := record["duration"]; !ok {
		t.Error("missing duration")
	}
	for _, secret := range []string{"private-token", "private body", "hidden"} {
		if strings.Contains(output.String(), secret) {
			t.Errorf("log leaked %q", secret)
		}
	}
}

func TestTimeout_PropagatesDeadlineAndCancelsContext(t *testing.T) {
	logger, _ := testLogger()
	m, err := NewTimeout(time.Millisecond, logger)
	var childError error
	h := Chain(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		child := r.Context()
		if _, ok := child.Deadline(); !ok {
			t.Fatal("missing deadline")
		}
		<-child.Done()
		childError = child.Err()
	}), requestIDMiddleware(t), mustMiddleware(t, m, err))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil))
	assertErrorResponse(t, w, http.StatusGatewayTimeout, "deadline_exceeded")
	if !errors.Is(childError, context.DeadlineExceeded) {
		t.Errorf("context error = %v", childError)
	}
}

type limiterFunc func(context.Context, string, string, string) (bool, time.Duration, error)

func (f limiterFunc) Allow(ctx context.Context, method, path, ip string) (bool, time.Duration, error) {
	return f(ctx, method, path, ip)
}

func TestRateLimit_AllowAllAndRejection(t *testing.T) {
	cases := []struct {
		name        string
		limiter     RateLimiter
		status      int
		code, retry string
	}{
		{"allow all", AllowAllRateLimiter{}, 204, "", ""},
		{"denied", limiterFunc(func(context.Context, string, string, string) (bool, time.Duration, error) {
			return false, 1500 * time.Millisecond, nil
		}), 429, "rate_limit_exceeded", "2"},
		{"failure", limiterFunc(func(context.Context, string, string, string) (bool, time.Duration, error) {
			return false, 0, errors.New("private limiter failure")
		}), 500, "internal_error", ""},
		{"canceled dependency", limiterFunc(func(context.Context, string, string, string) (bool, time.Duration, error) {
			return false, 0, fmt.Errorf("private limiter cancellation: %w", context.Canceled)
		}), 500, "internal_error", ""},
		{"deadline dependency", limiterFunc(func(context.Context, string, string, string) (bool, time.Duration, error) {
			return false, 0, fmt.Errorf("private limiter deadline: %w", context.DeadlineExceeded)
		}), 500, "internal_error", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			logger, _ := testLogger()
			m, err := NewRateLimit(tc.limiter, logger)
			calls := 0
			h := Chain(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { calls++; w.WriteHeader(204) }), requestIDMiddleware(t), mustMiddleware(t, m, err))
			w := httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil))
			if w.Code != tc.status {
				t.Fatalf("status = %d", w.Code)
			}
			if tc.code != "" {
				assertErrorResponse(t, w, tc.status, tc.code)
			}
			if (calls == 1) != (tc.status == 204) {
				t.Errorf("handler calls = %d", calls)
			}
			if got := w.Header().Get("Retry-After"); got != tc.retry {
				t.Errorf("Retry-After = %q", got)
			}
		})
	}
}

type authenticatorFunc func(context.Context, string) (identity.AuthenticateResult, error)

func (f authenticatorFunc) Authenticate(ctx context.Context, token string) (identity.AuthenticateResult, error) {
	return f(ctx, token)
}

type authenticationCase struct {
	name          string
	headers       []string
	authError     error
	status        int
	code          string
	wantAuthCalls int
}

func TestAuthentication_HeaderAndTokenErrors(t *testing.T) {
	cases := []authenticationCase{
		{"valid", []string{"bEaReR token"}, nil, 204, "", 1},
		{"missing", nil, nil, 401, "unauthorized", 0},
		{"multiple headers", []string{"Bearer token", "Bearer token"}, nil, 401, "unauthorized", 0},
		{"wrong scheme", []string{"Basic token"}, nil, 401, "unauthorized", 0},
		{"empty token", []string{"Bearer "}, nil, 401, "unauthorized", 0},
		{"extra token", []string{"Bearer token extra"}, nil, 401, "unauthorized", 0},
		{"invalid token", []string{"Bearer token"}, fmt.Errorf("private parser details: %w", identity.ErrInvalidToken), 401, "unauthorized", 1},
		{"internal failure", []string{"Bearer token"}, errors.New("private failure"), 500, "internal_error", 1},
		{"canceled dependency", []string{"Bearer token"}, fmt.Errorf("private authentication cancellation: %w", context.Canceled), 500, "internal_error", 1},
		{"deadline dependency", []string{"Bearer token"}, fmt.Errorf("private authentication deadline: %w", context.DeadlineExceeded), 504, "deadline_exceeded", 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runAuthenticationCase(t, tc)
		})
	}
}

func runAuthenticationCase(t *testing.T, tc authenticationCase) {
	t.Helper()
	logger, _ := testLogger()
	authCalls, handlerCalls := 0, 0
	principal := identity.AuthenticateResult{UserID: uuid.New(), Role: identity.RoleUser}
	m, err := NewAuthentication(authenticatorFunc(func(ctx context.Context, token string) (identity.AuthenticateResult, error) {
		authCalls++
		if token != "token" {
			t.Errorf("token = %q", token)
		}
		if id, _ := requestIDFromContext(ctx); id != foundationRequestID {
			t.Error("context was not propagated")
		}
		return principal, tc.authError
	}), logger)
	h := Chain(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalls++
		got, err := principalFromContext(r.Context())
		if err != nil || got == nil || *got != principal {
			t.Fatalf("principal = %v, error = %v", got, err)
		}
		w.WriteHeader(204)
	}), requestIDMiddleware(t), mustMiddleware(t, m, err))
	r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
	r.Header["Authorization"] = tc.headers
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != tc.status || authCalls != tc.wantAuthCalls {
		t.Fatalf("status = %d, auth calls = %d", w.Code, authCalls)
	}
	if (handlerCalls == 1) != (tc.status == 204) {
		t.Errorf("handler calls = %d", handlerCalls)
	}
	if tc.code != "" {
		assertErrorResponse(t, w, tc.status, tc.code)
	}
	if strings.Contains(w.Body.String(), "private") {
		t.Error("internal error leaked")
	}
}

func TestAuthentication_DeadlineBecomesGatewayTimeout(t *testing.T) {
	logger, _ := testLogger()
	auth, err := NewAuthentication(authenticatorFunc(func(ctx context.Context, _ string) (identity.AuthenticateResult, error) {
		<-ctx.Done()
		return identity.AuthenticateResult{}, fmt.Errorf("authentication: %w", ctx.Err())
	}), logger)
	auth = mustMiddleware(t, auth, err)
	timeout, err := NewTimeout(time.Millisecond, logger)
	h := Chain(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("handler called after deadline") }), requestIDMiddleware(t), mustMiddleware(t, timeout, err), auth)
	r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
	r.Header.Set("Authorization", "Bearer token")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	assertErrorResponse(t, w, 504, "deadline_exceeded")
}

func TestRBAC_RequiredPrincipalAndRoles(t *testing.T) {
	cases := []struct {
		name    string
		role    identity.Role
		allowed []identity.Role
		status  int
		code    string
	}{
		{"no principal", "", []identity.Role{identity.RoleAdmin}, 401, "unauthorized"},
		{"user on admin route", identity.RoleUser, []identity.Role{identity.RoleAdmin}, 403, "forbidden"},
		{"admin on admin route", identity.RoleAdmin, []identity.Role{identity.RoleAdmin}, 204, ""},
		{"user in multiple roles", identity.RoleUser, []identity.Role{identity.RoleAdmin, identity.RoleUser}, 204, ""},
		{"admin in multiple roles", identity.RoleAdmin, []identity.Role{identity.RoleUser, identity.RoleAdmin}, 204, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			logger, _ := testLogger()
			m, err := NewRBAC(logger, tc.allowed...)
			calls := 0
			h := Chain(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { calls++; w.WriteHeader(204) }), requestIDMiddleware(t), mustMiddleware(t, m, err))
			ctx := t.Context()
			if tc.role != "" {
				ctx = context.WithValue(ctx, principalKey, &identity.AuthenticateResult{UserID: uuid.New(), Role: tc.role})
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequestWithContext(ctx, http.MethodGet, "/", nil))
			if w.Code != tc.status || (calls == 1) != (tc.status == 204) {
				t.Fatalf("status = %d, handler calls = %d", w.Code, calls)
			}
			if tc.code != "" {
				assertErrorResponse(t, w, tc.status, tc.code)
			}
		})
	}
}

func TestFullMiddlewareChain_EntryAndExitOrder(t *testing.T) {
	logger, _ := testLogger()
	recovery, err := NewRecovery(logger)
	recovery = mustMiddleware(t, recovery, err)
	logging, err := NewLogging(logger)
	logging = mustMiddleware(t, logging, err)
	timeout, err := NewTimeout(time.Second, logger)
	timeout = mustMiddleware(t, timeout, err)
	limiter, err := NewRateLimit(AllowAllRateLimiter{}, logger)
	limiter = mustMiddleware(t, limiter, err)
	auth, err := NewAuthentication(authenticatorFunc(func(context.Context, string) (identity.AuthenticateResult, error) {
		return identity.AuthenticateResult{UserID: uuid.New(), Role: identity.RoleAdmin}, nil
	}), logger)
	auth = mustMiddleware(t, auth, err)
	rbac, err := NewRBAC(logger, identity.RoleAdmin)
	rbac = mustMiddleware(t, rbac, err)
	var calls []string
	names := []string{"recovery", "request ID", "logging", "timeout", "rate limit", "authentication", "RBAC"}
	middlewares := []Middleware{recovery, requestIDMiddleware(t), logging, timeout, limiter, auth, rbac}
	spies := make([]Middleware, 0, len(middlewares))
	for i, middleware := range middlewares {
		spies = append(spies, func(next http.Handler) http.Handler {
			return recordingMiddleware(names[i], &calls)(middleware(next))
		})
	}
	h := Chain(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { calls = append(calls, "handler"); w.WriteHeader(204) }), spies...)
	r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
	r.Header.Set("Authorization", "Bearer token")
	h.ServeHTTP(httptest.NewRecorder(), r)
	want := make([]string, 0, 2*len(names)+1)
	for _, name := range names {
		want = append(want, name+" before")
	}
	want = append(want, "handler")
	for i := len(names) - 1; i >= 0; i-- {
		want = append(want, names[i]+" after")
	}
	if !reflect.DeepEqual(calls, want) {
		t.Errorf("call order = %v, want %v", calls, want)
	}
}
