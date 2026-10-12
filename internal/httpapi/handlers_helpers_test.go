package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestHandleJSONDecodeError_TimeoutWithCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	r := httptest.NewRequestWithContext(ctx, http.MethodPost, "/", nil)
	w := httptest.NewRecorder()
	logger, _ := testLogger()

	// A read deadline can fail after net/http has canceled the request context.
	// Wrap the timeout to exercise errors.As through the decoder's error chain.
	err := fmt.Errorf("decode body: %w", os.ErrDeadlineExceeded)
	h := requestIDMiddleware(t)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handleJSONDecodeError(w, r, logger, err)
	}))
	h.ServeHTTP(w, r)

	assertErrorResponse(t, w, http.StatusGatewayTimeout, string(codeDeadlineExceeded))
}

func TestHandleJSONDecodeError_CanceledContextWithoutTimeoutDoesNotWriteResponse(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	r := httptest.NewRequestWithContext(ctx, http.MethodPost, "/", nil)
	w := httptest.NewRecorder()
	state := ensureResponseState(w)
	logger, _ := testLogger()

	handleJSONDecodeError(state, r, logger, errors.New("client disconnected"))

	if state.Written() || w.Body.Len() != 0 || len(w.Header()) != 0 {
		t.Fatalf("unexpected response: written = %v, headers = %v, body = %q",
			state.Written(), w.Header(), w.Body.String())
	}
}
