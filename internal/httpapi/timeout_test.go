package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestTimeout_UsesEarlierParentDeadline(t *testing.T) {
	parentDeadline := time.Now().Add(time.Second)
	ctx, cancel := context.WithDeadline(t.Context(), parentDeadline)
	defer cancel()
	logger, _ := testLogger()
	middleware, err := NewTimeout(time.Minute, logger)
	middleware = mustMiddleware(t, middleware, err)
	called := false
	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		if deadline, ok := r.Context().Deadline(); !ok || !deadline.Equal(parentDeadline) {
			t.Errorf("context deadline = %v, present = %v, want %v", deadline, ok, parentDeadline)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	writer := withReadDeadline(httptest.NewRecorder())
	handler.ServeHTTP(writer, httptest.NewRequestWithContext(ctx, http.MethodGet, "/", nil))
	if !called {
		t.Fatal("handler was not called")
	}
	if !writer.readDeadline.Equal(parentDeadline) {
		t.Errorf("read deadline = %v, want %v", writer.readDeadline, parentDeadline)
	}
	if want := parentDeadline.Add(responseWriteGrace); !writer.writeDeadline.Equal(want) {
		t.Errorf("write deadline = %v, want %v", writer.writeDeadline, want)
	}
}

func TestTimeout_DeadlineSetupFailureDoesNotCallHandler(t *testing.T) {
	setupErr := errors.New("forced deadline setup failure")
	for _, operation := range []string{"read", "write"} {
		t.Run(operation, func(t *testing.T) {
			logger, _ := testLogger()
			middleware, err := NewTimeout(time.Second, logger)
			middleware = mustMiddleware(t, middleware, err)
			called := false
			handler := Chain(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
				called = true
			}), requestIDMiddleware(t), middleware)
			recorder := httptest.NewRecorder()
			writer := withReadDeadline(recorder)
			if operation == "read" {
				writer.readDeadlineErr = setupErr
			} else {
				writer.writeDeadlineErr = setupErr
			}
			handler.ServeHTTP(writer, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil))
			if called {
				t.Error("handler called after deadline setup failure")
			}
			assertErrorResponse(t, recorder, http.StatusInternalServerError, string(codeInternalError))
		})
	}
}

func TestTimeout_DoesNotAppendErrorToStartedResponse(t *testing.T) {
	for _, body := range []string{"", `{"partial":`} {
		t.Run(body, func(t *testing.T) {
			logger, _ := testLogger()
			middleware, err := NewTimeout(time.Millisecond, logger)
			middleware = mustMiddleware(t, middleware, err)
			handler := Chain(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusAccepted)
				if _, err := w.Write([]byte(body)); err != nil {
					t.Fatalf("write partial response: %v", err)
				}
				<-r.Context().Done()
			}), requestIDMiddleware(t), middleware)
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(withReadDeadline(recorder), httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil))
			if recorder.Code != http.StatusAccepted || recorder.Body.String() != body {
				t.Errorf("response = (%d, %q), want (%d, %q)", recorder.Code, recorder.Body.String(), http.StatusAccepted, body)
			}
		})
	}
}
