package httpapi

import (
	"net/http/httptest"
	"time"
)

// readDeadlineRecorder records deadline requests without simulating socket I/O.
// Actual read/write timeouts must be tested against a real HTTP server.
type readDeadlineRecorder struct {
	*httptest.ResponseRecorder
	readDeadline     time.Time
	writeDeadline    time.Time
	readDeadlineErr  error
	writeDeadlineErr error
}

func withReadDeadline(recorder *httptest.ResponseRecorder) *readDeadlineRecorder {
	return &readDeadlineRecorder{ResponseRecorder: recorder}
}

func (r *readDeadlineRecorder) SetReadDeadline(deadline time.Time) error {
	r.readDeadline = deadline
	return r.readDeadlineErr
}

func (r *readDeadlineRecorder) SetWriteDeadline(deadline time.Time) error {
	r.writeDeadline = deadline
	return r.writeDeadlineErr
}
