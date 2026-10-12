package httpapi

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/google/uuid"
)

func TestResponseState_WriteSetsImplicitOKStatus(t *testing.T) {
	recorder := httptest.NewRecorder()
	state := ensureResponseState(recorder)
	data := []byte("response body")

	n, err := state.Write(data)
	if err != nil {
		t.Fatalf("Write() error = %v, want nil", err)
	}
	if n != len(data) {
		t.Errorf("Write() n = %d, want %d", n, len(data))
	}
	if !state.Written() {
		t.Error("Written() = false, want true")
	}
	if state.Status() != http.StatusOK {
		t.Errorf("Status() = %d, want %d", state.Status(), http.StatusOK)
	}
	if state.Bytes() != len(data) {
		t.Errorf("Bytes() = %d, want %d", state.Bytes(), len(data))
	}
	if recorder.Code != http.StatusOK {
		t.Errorf("recorder.Code = %d, want %d", recorder.Code, http.StatusOK)
	}
	if recorder.Body.String() != string(data) {
		t.Errorf("response body = %q, want %q", recorder.Body.String(), data)
	}
}

func TestResponseState_WriteHeaderKeepsFirstStatus(t *testing.T) {
	recorder := httptest.NewRecorder()
	state := ensureResponseState(recorder)

	state.WriteHeader(http.StatusCreated)
	state.WriteHeader(http.StatusInternalServerError)

	if !state.Written() {
		t.Error("Written() = false, want true")
	}
	if state.Status() != http.StatusCreated {
		t.Errorf("Status() = %d, want %d", state.Status(), http.StatusCreated)
	}
	if recorder.Code != http.StatusCreated {
		t.Errorf("recorder.Code = %d, want %d", recorder.Code, http.StatusCreated)
	}
}

func TestResponseState_WriteAccumulatesBytes(t *testing.T) {
	recorder := httptest.NewRecorder()
	state := ensureResponseState(recorder)

	for _, data := range [][]byte{[]byte("abc"), []byte("de")} {
		n, err := state.Write(data)
		if err != nil {
			t.Fatalf("Write(%q) error = %v, want nil", data, err)
		}
		if n != len(data) {
			t.Errorf("Write(%q) n = %d, want %d", data, n, len(data))
		}
	}

	if state.Bytes() != 5 {
		t.Errorf("Bytes() = %d, want 5", state.Bytes())
	}
	if recorder.Body.String() != "abcde" {
		t.Errorf("response body = %q, want %q", recorder.Body.String(), "abcde")
	}
}

func TestResponseState_WriteCountsPartialWriteWithError(t *testing.T) {
	writeErr := errors.New("write failed")
	underlying := &partialWriteResponseWriter{
		header:   make(http.Header),
		writeN:   2,
		writeErr: writeErr,
	}
	state := ensureResponseState(underlying)

	n, err := state.Write([]byte("data"))

	if n != 2 {
		t.Errorf("Write() n = %d, want 2", n)
	}
	if !errors.Is(err, writeErr) {
		t.Errorf("Write() error = %v, want %v", err, writeErr)
	}
	if !state.Written() {
		t.Error("Written() = false, want true")
	}
	if state.Status() != http.StatusOK {
		t.Errorf("Status() = %d, want %d", state.Status(), http.StatusOK)
	}
	if state.Bytes() != 2 {
		t.Errorf("Bytes() = %d, want 2", state.Bytes())
	}
	if underlying.status != http.StatusOK {
		t.Errorf("underlying status = %d, want %d", underlying.status, http.StatusOK)
	}
}

func TestResponseState_UnwrapReturnsUnderlyingWriter(t *testing.T) {
	recorder := httptest.NewRecorder()
	state := ensureResponseState(recorder)

	if state.Unwrap() != recorder {
		t.Error("Unwrap() did not return the underlying ResponseWriter")
	}
}

func TestEnsureResponseState_WrapsResponseWriter(t *testing.T) {
	recorder := httptest.NewRecorder()

	state := ensureResponseState(recorder)

	if state == nil {
		t.Fatal("ensureResponseState() returned nil")
	}
	if state.ResponseWriter != recorder {
		t.Error("ensureResponseState() wrapped an unexpected ResponseWriter")
	}
}

func TestEnsureResponseState_ReusesExistingState(t *testing.T) {
	existing := ensureResponseState(httptest.NewRecorder())

	got := ensureResponseState(existing)

	if got != existing {
		t.Error("ensureResponseState() created a second wrapper")
	}
}

func TestChain_AppliesMiddlewareInDeclaredOrder(t *testing.T) {
	var calls []string

	first := recordingMiddleware("first", &calls)
	second := recordingMiddleware("second", &calls)
	final := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		calls = append(calls, "handler")
	})

	handler := Chain(final, first, second)
	handler.ServeHTTP(
		httptest.NewRecorder(),
		httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil),
	)

	want := []string{
		"first before",
		"second before",
		"handler",
		"second after",
		"first after",
	}
	if !reflect.DeepEqual(calls, want) {
		t.Errorf("call order = %v, want %v", calls, want)
	}
}

func TestChain_WithoutMiddlewareCallsHandlerOnce(t *testing.T) {
	calls := 0
	final := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		calls++
	})

	handler := Chain(final)
	handler.ServeHTTP(
		httptest.NewRecorder(),
		httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil),
	)

	if calls != 1 {
		t.Errorf("handler calls = %d, want 1", calls)
	}
}

func TestNewRequestID_RejectsNilGenerator(t *testing.T) {
	middleware, err := NewRequestID(nil)

	if !errors.Is(err, ErrNilRequestIDGenerator) {
		t.Errorf("NewRequestID() error = %v, want ErrNilRequestIDGenerator", err)
	}
	if middleware != nil {
		t.Error("NewRequestID() middleware is non-nil, want nil")
	}
}

func TestRequestID_SetsMatchingContextAndResponseHeader(t *testing.T) {
	fixedID := uuid.MustParse("00000000-0000-0000-0000-000000000123")
	generatorCalls := 0
	generator := func() uuid.UUID {
		generatorCalls++
		return fixedID
	}

	middleware, err := NewRequestID(generator)
	if err != nil {
		t.Fatalf("NewRequestID() error = %v, want nil", err)
	}

	handlerCalls := 0
	handler := middleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		handlerCalls++

		got, ok := requestIDFromContext(r.Context())
		if !ok {
			t.Fatal("request ID is missing from context")
		}
		if got != fixedID.String() {
			t.Errorf("request ID in context = %q, want %q", got, fixedID.String())
		}
	}))
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(
		recorder,
		httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil),
	)

	if handlerCalls != 1 {
		t.Errorf("handler calls = %d, want 1", handlerCalls)
	}
	if generatorCalls != 1 {
		t.Errorf("generator calls = %d, want 1", generatorCalls)
	}
	if got := recorder.Header().Get(requestIDHeader); got != fixedID.String() {
		t.Errorf("%s response header = %q, want %q", requestIDHeader, got, fixedID.String())
	}
}

func TestRequestID_IgnoresIncomingHeader(t *testing.T) {
	clientID := "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	serverID := uuid.MustParse("00000000-0000-0000-0000-000000000123")
	middleware, err := NewRequestID(func() uuid.UUID { return serverID })
	if err != nil {
		t.Fatalf("NewRequestID() error = %v, want nil", err)
	}

	var contextID string
	handler := middleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		var ok bool
		contextID, ok = requestIDFromContext(r.Context())
		if !ok {
			t.Fatal("request ID is missing from context")
		}
	}))
	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
	request.Header.Set(requestIDHeader, clientID)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	if contextID != serverID.String() {
		t.Errorf("request ID in context = %q, want server ID %q", contextID, serverID.String())
	}
	if got := recorder.Header().Get(requestIDHeader); got != serverID.String() {
		t.Errorf("%s response header = %q, want server ID %q", requestIDHeader, got, serverID.String())
	}
}

func recordingMiddleware(name string, calls *[]string) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			*calls = append(*calls, name+" before")
			next.ServeHTTP(w, r)
			*calls = append(*calls, name+" after")
		})
	}
}

type partialWriteResponseWriter struct {
	header   http.Header
	status   int
	writeN   int
	writeErr error
}

func (w *partialWriteResponseWriter) Header() http.Header {
	return w.header
}

func (w *partialWriteResponseWriter) WriteHeader(status int) {
	w.status = status
}

func (w *partialWriteResponseWriter) Write([]byte) (int, error) {
	return w.writeN, w.writeErr
}
