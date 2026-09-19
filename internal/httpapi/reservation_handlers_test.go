package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"math"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/yyeart/flashdrop/internal/flashsale"
	"github.com/yyeart/flashdrop/internal/identity"
)

const reserveTestKey = "reserve-test-key"

var (
	reserveTestUserID = uuid.MustParse("11111111-1111-1111-1111-111111111111")
	reserveTestItemID = uuid.MustParse("22222222-2222-2222-2222-222222222222")
	reserveTestID     = uuid.MustParse("33333333-3333-3333-3333-333333333333")
	payTestOrderID    = uuid.MustParse("44444444-4444-4444-4444-444444444444")
	reserveTestNow    = time.Date(2026, time.September, 19, 12, 0, 0, 0, time.FixedZone("UTC+3", 3*60*60))
)

func newReservationHandlerForTest(
	t *testing.T,
	service reservationService,
	newID func() uuid.UUID,
	now func() time.Time,
	ttl time.Duration,
) *ReservationHandler {
	t.Helper()

	handler, err := NewReservationHandler(
		service,
		slog.New(slog.DiscardHandler),
		newID,
		now,
		ttl,
	)
	if err != nil {
		t.Fatalf("NewReservationHandler() error = %v", err)
	}

	return handler
}

func reserveRequestForTest(
	t *testing.T,
	method string,
	body string,
	principal *identity.AuthenticateResult,
) *http.Request {
	t.Helper()

	request := httptest.NewRequestWithContext(
		t.Context(),
		method,
		"/v1/reservations",
		strings.NewReader(body),
	)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", reserveTestKey)
	if principal != nil {
		request = request.WithContext(context.WithValue(request.Context(), principalKey, principal))
	}

	return request
}

func serveReserve(
	t *testing.T,
	handler *ReservationHandler,
	request *http.Request,
) *httptest.ResponseRecorder {
	t.Helper()

	recorder := httptest.NewRecorder()
	requestIDMiddleware(t)(http.HandlerFunc(handler.Reserve)).ServeHTTP(recorder, request)

	return recorder
}

func cancelRequestForTest(
	t *testing.T,
	method string,
	reservationID string,
	principal *identity.AuthenticateResult,
) *http.Request {
	t.Helper()

	request := httptest.NewRequestWithContext(
		t.Context(),
		method,
		"/v1/reservations/"+reservationID+"/cancel",
		nil,
	)
	if reservationID != "" {
		request.SetPathValue(reservationIDPathName, reservationID)
	}
	if principal != nil {
		request = request.WithContext(context.WithValue(request.Context(), principalKey, principal))
	}

	return request
}

func serveCancel(
	t *testing.T,
	handler *ReservationHandler,
	request *http.Request,
) *httptest.ResponseRecorder {
	t.Helper()

	recorder := httptest.NewRecorder()
	requestIDMiddleware(t)(http.HandlerFunc(handler.Cancel)).ServeHTTP(recorder, request)

	return recorder
}

func payRequestForTest(
	t *testing.T,
	method string,
	reservationID string,
	principal *identity.AuthenticateResult,
) *http.Request {
	t.Helper()

	request := httptest.NewRequestWithContext(
		t.Context(),
		method,
		"/v1/reservations/"+reservationID+"/pay",
		nil,
	)
	if reservationID != "" {
		request.SetPathValue(reservationIDPathName, reservationID)
	}
	if principal != nil {
		request = request.WithContext(context.WithValue(request.Context(), principalKey, principal))
	}

	return request
}

func servePay(
	t *testing.T,
	handler *ReservationHandler,
	request *http.Request,
) *httptest.ResponseRecorder {
	t.Helper()

	recorder := httptest.NewRecorder()
	requestIDMiddleware(t)(http.HandlerFunc(handler.Pay)).ServeHTTP(recorder, request)

	return recorder
}

func reservationResultForCommand(
	t *testing.T,
	cmd flashsale.ReserveCommand,
	now time.Time,
	replayed bool,
) flashsale.ReserveResult {
	t.Helper()

	reservation, err := flashsale.RehydrateReservation(flashsale.ReservationSnapshot{
		ID:         cmd.ReservationID,
		UserID:     cmd.UserID,
		SaleItemID: cmd.SaleItemID,
		Quantity:   cmd.Quantity,
		State:      flashsale.PendingState,
		CreatedAt:  now,
		ExpiresAt:  cmd.ExpiresAt,
	})
	if err != nil {
		t.Fatalf("RehydrateReservation() error = %v", err)
	}

	return flashsale.ReserveResult{Reservation: reservation, Replayed: replayed}
}

func TestReservationHandler_ReserveCreatesAndReplaysReservation(t *testing.T) {
	tests := []struct {
		name       string
		replayed   bool
		wantStatus int
	}{
		{"created", false, http.StatusCreated},
		{"replayed", true, http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runReserveSuccessCase(t, tt.replayed, tt.wantStatus)
		})
	}
}

func runReserveSuccessCase(t *testing.T, replayed bool, wantStatus int) {
	t.Helper()

	var newIDCalls, nowCalls int
	service := &reservationServiceStub{}
	service.reserve = func(
		_ context.Context,
		cmd flashsale.ReserveCommand,
		now time.Time,
	) (flashsale.ReserveResult, error) {
		return reservationResultForCommand(t, cmd, now, replayed), nil
	}
	handler := newReservationHandlerForTest(
		t,
		service,
		func() uuid.UUID {
			newIDCalls++
			return reserveTestID
		},
		func() time.Time {
			nowCalls++
			return reserveTestNow
		},
		15*time.Minute,
	)

	principal := &identity.AuthenticateResult{
		UserID: reserveTestUserID,
		Role:   identity.RoleUser,
	}
	request := reserveRequestForTest(
		t,
		http.MethodPost,
		`{"sale_item_id":"22222222-2222-2222-2222-222222222222","quantity":2}`,
		principal,
	)
	request.Header.Set("Idempotency-Key", "  AbC  ")

	recorder := serveReserve(t, handler, request)
	if recorder.Code != wantStatus {
		t.Fatalf("status = %d, want %d; body = %s", recorder.Code, wantStatus, recorder.Body)
	}
	if got := recorder.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}
	if service.reserveCalls != 1 || newIDCalls != 1 || nowCalls != 1 {
		t.Fatalf(
			"calls = service:%d newID:%d now:%d, want 1 each",
			service.reserveCalls, newIDCalls, nowCalls,
		)
	}

	wantNow := reserveTestNow.UTC()
	wantExpiresAt := wantNow.Add(15 * time.Minute)
	assertReserveCommand(t, service.gotCommand, wantExpiresAt)
	if !service.gotNow.Equal(wantNow) || service.gotNow.Location() != time.UTC {
		t.Fatalf("Reserve now = %v (%v), want %v UTC", service.gotNow, service.gotNow.Location(), wantNow)
	}

	var response reservationResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	assertReserveResponse(t, response, wantNow, wantExpiresAt)
}

func assertReserveCommand(t *testing.T, cmd flashsale.ReserveCommand, wantExpiresAt time.Time) {
	t.Helper()

	if cmd.ReservationID != reserveTestID {
		t.Errorf("ReservationID = %s, want %s", cmd.ReservationID, reserveTestID)
	}
	if cmd.UserID != reserveTestUserID {
		t.Errorf("UserID = %s, want %s", cmd.UserID, reserveTestUserID)
	}
	if cmd.SaleItemID != reserveTestItemID {
		t.Errorf("SaleItemID = %s, want %s", cmd.SaleItemID, reserveTestItemID)
	}
	if cmd.Quantity != 2 {
		t.Errorf("Quantity = %d, want 2", cmd.Quantity)
	}
	if cmd.IdempotencyKey != "  AbC  " {
		t.Errorf("IdempotencyKey = %q, want preserved opaque value", cmd.IdempotencyKey)
	}
	if !cmd.ExpiresAt.Equal(wantExpiresAt) {
		t.Errorf("ExpiresAt = %v, want %v", cmd.ExpiresAt, wantExpiresAt)
	}
}

func assertReserveResponse(
	t *testing.T,
	response reservationResponse,
	wantNow time.Time,
	wantExpiresAt time.Time,
) {
	t.Helper()

	if response.ID != reserveTestID {
		t.Errorf("response ID = %s, want %s", response.ID, reserveTestID)
	}
	if response.UserID != reserveTestUserID {
		t.Errorf("response UserID = %s, want %s", response.UserID, reserveTestUserID)
	}
	if response.SaleItemID != reserveTestItemID {
		t.Errorf("response SaleItemID = %s, want %s", response.SaleItemID, reserveTestItemID)
	}
	if response.Quantity != 2 {
		t.Errorf("response Quantity = %d, want 2", response.Quantity)
	}
	if response.State != flashsale.PendingState {
		t.Errorf("response State = %q, want %q", response.State, flashsale.PendingState)
	}
	if !response.CreatedAt.Equal(wantNow) {
		t.Errorf("response CreatedAt = %v, want %v", response.CreatedAt, wantNow)
	}
	if !response.ExpiresAt.Equal(wantExpiresAt) {
		t.Errorf("response ExpiresAt = %v, want %v", response.ExpiresAt, wantExpiresAt)
	}
}

func TestReservationHandler_ReserveRejectsInvalidRequestWithoutCallingService(t *testing.T) {
	validPrincipal := &identity.AuthenticateResult{UserID: reserveTestUserID, Role: identity.RoleUser}
	validBody := `{"sale_item_id":"22222222-2222-2222-2222-222222222222","quantity":1}`

	tests := []struct {
		name       string
		request    func(*testing.T) *http.Request
		newID      func() uuid.UUID
		wantStatus int
		wantCode   errorCode
		wantAllow  string
	}{
		{
			name: "wrong method",
			request: func(t *testing.T) *http.Request {
				return reserveRequestForTest(t, http.MethodGet, validBody, validPrincipal)
			},
			newID: uuid.New, wantStatus: http.StatusMethodNotAllowed,
			wantCode: codeMethodNotAllowed, wantAllow: http.MethodPost,
		},
		{
			name: "missing principal",
			request: func(t *testing.T) *http.Request {
				return reserveRequestForTest(t, http.MethodPost, validBody, nil)
			},
			newID: uuid.New, wantStatus: http.StatusUnauthorized, wantCode: codeUnauthorized,
		},
		{
			name: "nil principal user ID",
			request: func(t *testing.T) *http.Request {
				return reserveRequestForTest(t, http.MethodPost, validBody, &identity.AuthenticateResult{Role: identity.RoleUser})
			},
			newID: uuid.New, wantStatus: http.StatusUnauthorized, wantCode: codeUnauthorized,
		},
		{
			name: "unknown principal role",
			request: func(t *testing.T) *http.Request {
				return reserveRequestForTest(t, http.MethodPost, validBody, &identity.AuthenticateResult{
					UserID: reserveTestUserID,
					Role:   identity.Role("unknown"),
				})
			},
			newID: uuid.New, wantStatus: http.StatusUnauthorized, wantCode: codeUnauthorized,
		},
		{
			name: "missing idempotency key",
			request: func(t *testing.T) *http.Request {
				r := reserveRequestForTest(t, http.MethodPost, validBody, validPrincipal)
				r.Header.Del("Idempotency-Key")
				return r
			},
			newID: uuid.New, wantStatus: http.StatusBadRequest, wantCode: codeBadRequest,
		},
		{
			name: "multiple idempotency keys",
			request: func(t *testing.T) *http.Request {
				r := reserveRequestForTest(t, http.MethodPost, validBody, validPrincipal)
				r.Header["Idempotency-Key"] = []string{"first", "second"}
				return r
			},
			newID: uuid.New, wantStatus: http.StatusBadRequest, wantCode: codeBadRequest,
		},
		{
			name: "idempotency key too long",
			request: func(t *testing.T) *http.Request {
				r := reserveRequestForTest(t, http.MethodPost, validBody, validPrincipal)
				r.Header.Set("Idempotency-Key", strings.Repeat("x", 256))
				return r
			},
			newID: uuid.New, wantStatus: http.StatusBadRequest, wantCode: codeBadRequest,
		},
		{
			name: "unknown JSON field",
			request: func(t *testing.T) *http.Request {
				return reserveRequestForTest(t, http.MethodPost, `{"sale_item_id":"22222222-2222-2222-2222-222222222222","quantity":1,"user_id":"11111111-1111-1111-1111-111111111111"}`, validPrincipal)
			},
			newID: uuid.New, wantStatus: http.StatusBadRequest, wantCode: codeBadRequest,
		},
		{
			name: "malformed sale item ID",
			request: func(t *testing.T) *http.Request {
				return reserveRequestForTest(t, http.MethodPost, `{"sale_item_id":"bad","quantity":1}`, validPrincipal)
			},
			newID: uuid.New, wantStatus: http.StatusBadRequest, wantCode: codeBadRequest,
		},
		{
			name: "missing sale item ID",
			request: func(t *testing.T) *http.Request {
				return reserveRequestForTest(t, http.MethodPost, `{"quantity":1}`, validPrincipal)
			},
			newID: uuid.New, wantStatus: http.StatusBadRequest, wantCode: codeBadRequest,
		},
		{
			name: "zero quantity",
			request: func(t *testing.T) *http.Request {
				return reserveRequestForTest(t, http.MethodPost, `{"sale_item_id":"22222222-2222-2222-2222-222222222222","quantity":0}`, validPrincipal)
			},
			newID: uuid.New, wantStatus: http.StatusBadRequest, wantCode: codeBadRequest,
		},
		{
			name: "quantity exceeds contract maximum",
			request: func(t *testing.T) *http.Request {
				return reserveRequestForTest(t, http.MethodPost, `{"sale_item_id":"22222222-2222-2222-2222-222222222222","quantity":2147483648}`, validPrincipal)
			},
			newID: uuid.New, wantStatus: http.StatusBadRequest, wantCode: codeBadRequest,
		},
		{
			name: "nil generated reservation ID",
			request: func(t *testing.T) *http.Request {
				return reserveRequestForTest(t, http.MethodPost, validBody, validPrincipal)
			},
			newID:      func() uuid.UUID { return uuid.Nil },
			wantStatus: http.StatusInternalServerError, wantCode: codeInternalError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := &reservationServiceStub{}
			handler := newReservationHandlerForTest(t, service, tt.newID, func() time.Time {
				return reserveTestNow
			}, 15*time.Minute)

			recorder := serveReserve(t, handler, tt.request(t))
			assertErrorResponse(t, recorder, tt.wantStatus, string(tt.wantCode))
			if got := recorder.Header().Get("Allow"); got != tt.wantAllow {
				t.Fatalf("Allow = %q, want %q", got, tt.wantAllow)
			}
			if service.reserveCalls != 0 {
				t.Fatalf("Reserve calls = %d, want 0", service.reserveCalls)
			}
		})
	}
}

func TestReservationHandler_ReserveMapsServiceErrors(t *testing.T) {
	privateDetail := "private database failure"
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   errorCode
		wantSilent bool
	}{
		{"deadline", context.DeadlineExceeded, http.StatusGatewayTimeout, codeDeadlineExceeded, false},
		{"invalid quantity", flashsale.ErrInvalidQuantity, http.StatusBadRequest, codeBadRequest, false},
		{"unavailable", flashsale.ErrReservationUnavailable, http.StatusConflict, codeConflict, false},
		{"idempotency conflict", flashsale.ErrConflict, http.StatusConflict, codeConflict, false},
		{"canceled", context.Canceled, 0, "", true},
		{"unknown", errors.New(privateDetail), http.StatusInternalServerError, codeInternalError, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := &reservationServiceStub{
				reserve: func(context.Context, flashsale.ReserveCommand, time.Time) (flashsale.ReserveResult, error) {
					return flashsale.ReserveResult{}, tt.err
				},
			}
			handler := newReservationHandlerForTest(t, service, func() uuid.UUID {
				return reserveTestID
			}, func() time.Time {
				return reserveTestNow
			}, 15*time.Minute)
			request := reserveRequestForTest(
				t,
				http.MethodPost,
				`{"sale_item_id":"22222222-2222-2222-2222-222222222222","quantity":1}`,
				&identity.AuthenticateResult{UserID: reserveTestUserID, Role: identity.RoleUser},
			)

			recorder := serveReserve(t, handler, request)
			if tt.wantSilent {
				if recorder.Body.Len() != 0 {
					t.Fatalf("body = %q, want empty", recorder.Body.String())
				}
				return
			}

			assertErrorResponse(t, recorder, tt.wantStatus, string(tt.wantCode))
			if strings.Contains(recorder.Body.String(), privateDetail) {
				t.Fatalf("private error leaked to client: %s", recorder.Body)
			}
		})
	}
}

func TestReservationHandler_ReserveCanceledContextDoesNotCallServiceOrWrite(t *testing.T) {
	service := &reservationServiceStub{}
	handler := newReservationHandlerForTest(t, service, uuid.New, time.Now, time.Minute)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	request := httptest.NewRequestWithContext(ctx, http.MethodPost, "/v1/reservations", nil)
	recorder := httptest.NewRecorder()

	handler.Reserve(recorder, request)

	if service.reserveCalls != 0 {
		t.Fatalf("Reserve calls = %d, want 0", service.reserveCalls)
	}
	if recorder.Body.Len() != 0 || len(recorder.Header()) != 0 {
		t.Fatalf("response = headers %#v body %q, want untouched", recorder.Header(), recorder.Body.String())
	}
}

func TestReservationHandler_ReserveAcceptsContractBoundaries(t *testing.T) {
	tests := []struct {
		name     string
		quantity int
		key      string
	}{
		{"minimum", 1, "x"},
		{"maximum", math.MaxInt32, strings.Repeat("x", 255)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := &reservationServiceStub{}
			service.reserve = func(_ context.Context, cmd flashsale.ReserveCommand, now time.Time) (flashsale.ReserveResult, error) {
				return reservationResultForCommand(t, cmd, now, false), nil
			}
			handler := newReservationHandlerForTest(t, service, func() uuid.UUID {
				return reserveTestID
			}, func() time.Time {
				return reserveTestNow
			}, time.Minute)
			request := reserveRequestForTest(
				t,
				http.MethodPost,
				`{"sale_item_id":"22222222-2222-2222-2222-222222222222","quantity":`+strconv.Itoa(tt.quantity)+`}`,
				&identity.AuthenticateResult{UserID: reserveTestUserID, Role: identity.RoleUser},
			)
			request.Header.Set("Idempotency-Key", tt.key)

			recorder := serveReserve(t, handler, request)
			if recorder.Code != http.StatusCreated {
				t.Fatalf("status = %d, want 201; body = %s", recorder.Code, recorder.Body)
			}
			if service.gotCommand.Quantity != tt.quantity || service.gotCommand.IdempotencyKey != tt.key {
				t.Fatalf("command = %#v", service.gotCommand)
			}
		})
	}
}

func TestReservationHandler_CancelReturnsNoContent(t *testing.T) {
	var nowCalls int
	service := &reservationServiceStub{
		cancel: func(context.Context, uuid.UUID, uuid.UUID, time.Time) error {
			return nil
		},
	}
	handler := newReservationHandlerForTest(
		t,
		service,
		uuid.New,
		func() time.Time {
			nowCalls++
			return reserveTestNow
		},
		15*time.Minute,
	)
	principal := &identity.AuthenticateResult{
		UserID: reserveTestUserID,
		Role:   identity.RoleUser,
	}
	request := cancelRequestForTest(
		t,
		http.MethodPost,
		reserveTestID.String(),
		principal,
	)

	recorder := serveCancel(t, handler, request)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d; body = %s", recorder.Code, http.StatusNoContent, recorder.Body)
	}
	if recorder.Body.Len() != 0 {
		t.Fatalf("body = %q, want empty", recorder.Body.String())
	}
	if service.cancelCalls != 1 {
		t.Fatalf("Cancel calls = %d, want 1", service.cancelCalls)
	}
	if nowCalls != 1 {
		t.Fatalf("now calls = %d, want 1", nowCalls)
	}
	if service.gotCancelUserID != reserveTestUserID {
		t.Errorf("Cancel userID = %s, want %s", service.gotCancelUserID, reserveTestUserID)
	}
	if service.gotCancelReservationID != reserveTestID {
		t.Errorf("Cancel reservationID = %s, want %s", service.gotCancelReservationID, reserveTestID)
	}
	wantNow := reserveTestNow.UTC()
	if !service.gotCancelNow.Equal(wantNow) || service.gotCancelNow.Location() != time.UTC {
		t.Errorf(
			"Cancel now = %v (%v), want %v UTC",
			service.gotCancelNow,
			service.gotCancelNow.Location(),
			wantNow,
		)
	}
}

func TestReservationHandler_CancelRejectsInvalidRequestWithoutCallingService(t *testing.T) {
	validPrincipal := &identity.AuthenticateResult{
		UserID: reserveTestUserID,
		Role:   identity.RoleUser,
	}

	tests := []struct {
		name          string
		method        string
		reservationID string
		principal     *identity.AuthenticateResult
		wantStatus    int
		wantCode      errorCode
		wantAllow     string
	}{
		{
			name: "wrong method", method: http.MethodGet,
			reservationID: reserveTestID.String(), principal: validPrincipal,
			wantStatus: http.StatusMethodNotAllowed, wantCode: codeMethodNotAllowed,
			wantAllow: http.MethodPost,
		},
		{
			name: "missing principal", method: http.MethodPost,
			reservationID: reserveTestID.String(), principal: nil,
			wantStatus: http.StatusUnauthorized, wantCode: codeUnauthorized,
		},
		{
			name: "nil principal user ID", method: http.MethodPost,
			reservationID: reserveTestID.String(),
			principal:     &identity.AuthenticateResult{Role: identity.RoleUser},
			wantStatus:    http.StatusUnauthorized, wantCode: codeUnauthorized,
		},
		{
			name: "missing reservation ID", method: http.MethodPost,
			reservationID: "", principal: validPrincipal,
			wantStatus: http.StatusBadRequest, wantCode: codeBadRequest,
		},
		{
			name: "malformed reservation ID", method: http.MethodPost,
			reservationID: "not-a-uuid", principal: validPrincipal,
			wantStatus: http.StatusBadRequest, wantCode: codeBadRequest,
		},
		{
			name: "nil reservation ID", method: http.MethodPost,
			reservationID: uuid.Nil.String(), principal: validPrincipal,
			wantStatus: http.StatusBadRequest, wantCode: codeBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var nowCalls int
			service := &reservationServiceStub{}
			handler := newReservationHandlerForTest(
				t,
				service,
				uuid.New,
				func() time.Time {
					nowCalls++
					return reserveTestNow
				},
				15*time.Minute,
			)
			request := cancelRequestForTest(
				t,
				tt.method,
				tt.reservationID,
				tt.principal,
			)

			recorder := serveCancel(t, handler, request)

			assertErrorResponse(t, recorder, tt.wantStatus, string(tt.wantCode))
			if got := recorder.Header().Get("Allow"); got != tt.wantAllow {
				t.Fatalf("Allow = %q, want %q", got, tt.wantAllow)
			}
			if service.cancelCalls != 0 {
				t.Fatalf("Cancel calls = %d, want 0", service.cancelCalls)
			}
			if nowCalls != 0 {
				t.Fatalf("now calls = %d, want 0", nowCalls)
			}
		})
	}
}

func TestReservationHandler_CancelMapsServiceErrors(t *testing.T) {
	privateDetail := "private database failure"
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   errorCode
		wantSilent bool
	}{
		{"deadline", context.DeadlineExceeded, http.StatusGatewayTimeout, codeDeadlineExceeded, false},
		{"missing reservation", flashsale.ErrReservationNotFound, http.StatusNotFound, codeNotFound, false},
		{"foreign reservation", flashsale.ErrReservationNotFound, http.StatusNotFound, codeNotFound, false},
		{"forbidden transition", flashsale.ErrForbiddenTransition, http.StatusConflict, codeConflict, false},
		{"expired", flashsale.ErrExpiredTimeWindow, http.StatusConflict, codeConflict, false},
		{"canceled", context.Canceled, 0, "", true},
		{"unknown", errors.New(privateDetail), http.StatusInternalServerError, codeInternalError, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := &reservationServiceStub{
				cancel: func(context.Context, uuid.UUID, uuid.UUID, time.Time) error {
					return tt.err
				},
			}
			handler := newReservationHandlerForTest(
				t,
				service,
				uuid.New,
				func() time.Time { return reserveTestNow },
				15*time.Minute,
			)
			request := cancelRequestForTest(
				t,
				http.MethodPost,
				reserveTestID.String(),
				&identity.AuthenticateResult{UserID: reserveTestUserID, Role: identity.RoleUser},
			)

			recorder := serveCancel(t, handler, request)

			if service.cancelCalls != 1 {
				t.Fatalf("Cancel calls = %d, want 1", service.cancelCalls)
			}
			if tt.wantSilent {
				if recorder.Body.Len() != 0 {
					t.Fatalf("body = %q, want empty", recorder.Body.String())
				}
				return
			}

			assertErrorResponse(t, recorder, tt.wantStatus, string(tt.wantCode))
			if strings.Contains(recorder.Body.String(), privateDetail) {
				t.Fatalf("private error leaked to client: %s", recorder.Body)
			}
		})
	}
}

func TestReservationHandler_CancelCanceledContextDoesNotCallServiceOrWrite(t *testing.T) {
	service := &reservationServiceStub{}
	handler := newReservationHandlerForTest(t, service, uuid.New, time.Now, time.Minute)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	request := httptest.NewRequestWithContext(
		ctx,
		http.MethodPost,
		"/v1/reservations/"+reserveTestID.String()+"/cancel",
		nil,
	)
	recorder := httptest.NewRecorder()

	handler.Cancel(recorder, request)

	if service.cancelCalls != 0 {
		t.Fatalf("Cancel calls = %d, want 0", service.cancelCalls)
	}
	if recorder.Body.Len() != 0 || len(recorder.Header()) != 0 {
		t.Fatalf("response = headers %#v body %q, want untouched", recorder.Header(), recorder.Body.String())
	}
}

func TestReservationHandler_CancelServiceCancellationDoesNotWrite(t *testing.T) {
	requestContext, cancel := context.WithCancel(t.Context())
	service := &reservationServiceStub{
		cancel: func(context.Context, uuid.UUID, uuid.UUID, time.Time) error {
			cancel()
			return errors.New("operation interrupted after cancellation")
		},
	}
	handler := newReservationHandlerForTest(
		t,
		service,
		uuid.New,
		func() time.Time { return reserveTestNow },
		time.Minute,
	)
	request := httptest.NewRequestWithContext(
		requestContext,
		http.MethodPost,
		"/v1/reservations/"+reserveTestID.String()+"/cancel",
		nil,
	)
	request.SetPathValue(reservationIDPathName, reserveTestID.String())
	request = request.WithContext(context.WithValue(
		request.Context(),
		principalKey,
		&identity.AuthenticateResult{UserID: reserveTestUserID, Role: identity.RoleUser},
	))
	recorder := httptest.NewRecorder()

	handler.Cancel(recorder, request)

	if service.cancelCalls != 1 {
		t.Fatalf("Cancel calls = %d, want 1", service.cancelCalls)
	}
	if recorder.Body.Len() != 0 || len(recorder.Header()) != 0 {
		t.Fatalf("response = headers %#v body %q, want untouched", recorder.Header(), recorder.Body.String())
	}
}

func TestReservationHandler_PayCreatesOrder(t *testing.T) {
	var newIDCalls, nowCalls int
	service := &reservationServiceStub{
		pay: func(
			_ context.Context,
			userID uuid.UUID,
			reservationID uuid.UUID,
			orderID uuid.UUID,
			now time.Time,
		) (flashsale.Order, error) {
			return newPayTestOrder(t, orderID, reservationID, userID, now), nil
		},
	}
	handler := newReservationHandlerForTest(
		t,
		service,
		func() uuid.UUID {
			newIDCalls++
			return payTestOrderID
		},
		func() time.Time {
			nowCalls++
			return reserveTestNow
		},
		15*time.Minute,
	)
	request := payRequestForTest(
		t,
		http.MethodPost,
		reserveTestID.String(),
		&identity.AuthenticateResult{UserID: reserveTestUserID, Role: identity.RoleUser},
	)

	recorder := servePay(t, handler, request)

	wantNow := reserveTestNow.UTC()
	assertPaySuccessResponse(t, recorder, wantNow)
	assertPayServiceCall(t, service, newIDCalls, nowCalls, wantNow)
}

func assertPaySuccessResponse(
	t *testing.T,
	recorder *httptest.ResponseRecorder,
	wantNow time.Time,
) {
	t.Helper()

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body = %s", recorder.Code, http.StatusCreated, recorder.Body)
	}
	if got := recorder.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}

	var response orderResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.ID != payTestOrderID ||
		response.ReservationID != reserveTestID ||
		response.UserID != reserveTestUserID ||
		response.SaleItemID != reserveTestItemID ||
		response.Quantity != 2 ||
		!response.CreatedAt.Equal(wantNow) {
		t.Fatalf("response = %#v, want complete order response", response)
	}
}

func assertPayServiceCall(
	t *testing.T,
	service *reservationServiceStub,
	newIDCalls int,
	nowCalls int,
	wantNow time.Time,
) {
	t.Helper()

	if service.payCalls != 1 || newIDCalls != 1 || nowCalls != 1 {
		t.Fatalf(
			"calls = service:%d newID:%d now:%d, want 1 each",
			service.payCalls,
			newIDCalls,
			nowCalls,
		)
	}
	if service.gotPayUserID != reserveTestUserID {
		t.Errorf("Pay userID = %s, want %s", service.gotPayUserID, reserveTestUserID)
	}
	if service.gotPayReservationID != reserveTestID {
		t.Errorf("Pay reservationID = %s, want %s", service.gotPayReservationID, reserveTestID)
	}
	if service.gotPayOrderID != payTestOrderID {
		t.Errorf("Pay orderID = %s, want %s", service.gotPayOrderID, payTestOrderID)
	}
	if !service.gotPayNow.Equal(wantNow) || service.gotPayNow.Location() != time.UTC {
		t.Errorf(
			"Pay now = %v (%v), want %v UTC",
			service.gotPayNow,
			service.gotPayNow.Location(),
			wantNow,
		)
	}
}

func newPayTestOrder(
	t *testing.T,
	orderID uuid.UUID,
	reservationID uuid.UUID,
	userID uuid.UUID,
	createdAt time.Time,
) flashsale.Order {
	t.Helper()

	order, err := flashsale.NewOrder(flashsale.NewOrderInput{
		ID:            orderID,
		ReservationID: reservationID,
		UserID:        userID,
		SaleItemID:    reserveTestItemID,
		Quantity:      2,
		CreatedAt:     createdAt,
	})
	if err != nil {
		t.Fatalf("NewOrder() error = %v", err)
	}

	return order
}

func TestReservationHandler_PayRejectsInvalidRequestWithoutCallingService(t *testing.T) {
	validPrincipal := &identity.AuthenticateResult{
		UserID: reserveTestUserID,
		Role:   identity.RoleUser,
	}

	tests := []struct {
		name             string
		method           string
		reservationID    string
		principal        *identity.AuthenticateResult
		generatedOrderID uuid.UUID
		wantStatus       int
		wantCode         errorCode
		wantAllow        string
		wantNewIDCalls   int
	}{
		{
			name: "wrong method", method: http.MethodGet,
			reservationID: reserveTestID.String(), principal: validPrincipal,
			generatedOrderID: payTestOrderID,
			wantStatus:       http.StatusMethodNotAllowed, wantCode: codeMethodNotAllowed,
			wantAllow: http.MethodPost,
		},
		{
			name: "missing principal", method: http.MethodPost,
			reservationID: reserveTestID.String(), principal: nil,
			generatedOrderID: payTestOrderID,
			wantStatus:       http.StatusUnauthorized, wantCode: codeUnauthorized,
		},
		{
			name: "nil principal user ID", method: http.MethodPost,
			reservationID:    reserveTestID.String(),
			principal:        &identity.AuthenticateResult{Role: identity.RoleUser},
			generatedOrderID: payTestOrderID,
			wantStatus:       http.StatusUnauthorized, wantCode: codeUnauthorized,
		},
		{
			name: "missing reservation ID", method: http.MethodPost,
			reservationID: "", principal: validPrincipal,
			generatedOrderID: payTestOrderID,
			wantStatus:       http.StatusBadRequest, wantCode: codeBadRequest,
		},
		{
			name: "malformed reservation ID", method: http.MethodPost,
			reservationID: "not-a-uuid", principal: validPrincipal,
			generatedOrderID: payTestOrderID,
			wantStatus:       http.StatusBadRequest, wantCode: codeBadRequest,
		},
		{
			name: "nil reservation ID", method: http.MethodPost,
			reservationID: uuid.Nil.String(), principal: validPrincipal,
			generatedOrderID: payTestOrderID,
			wantStatus:       http.StatusBadRequest, wantCode: codeBadRequest,
		},
		{
			name: "nil generated order ID", method: http.MethodPost,
			reservationID: reserveTestID.String(), principal: validPrincipal,
			generatedOrderID: uuid.Nil,
			wantStatus:       http.StatusInternalServerError, wantCode: codeInternalError,
			wantNewIDCalls: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var newIDCalls, nowCalls int
			service := &reservationServiceStub{}
			handler := newReservationHandlerForTest(
				t,
				service,
				func() uuid.UUID {
					newIDCalls++
					return tt.generatedOrderID
				},
				func() time.Time {
					nowCalls++
					return reserveTestNow
				},
				15*time.Minute,
			)
			request := payRequestForTest(
				t,
				tt.method,
				tt.reservationID,
				tt.principal,
			)

			recorder := servePay(t, handler, request)

			assertErrorResponse(t, recorder, tt.wantStatus, string(tt.wantCode))
			if got := recorder.Header().Get("Allow"); got != tt.wantAllow {
				t.Fatalf("Allow = %q, want %q", got, tt.wantAllow)
			}
			if service.payCalls != 0 {
				t.Fatalf("Pay calls = %d, want 0", service.payCalls)
			}
			if newIDCalls != tt.wantNewIDCalls {
				t.Fatalf("newID calls = %d, want %d", newIDCalls, tt.wantNewIDCalls)
			}
			if nowCalls != 0 {
				t.Fatalf("now calls = %d, want 0", nowCalls)
			}
		})
	}
}

func TestReservationHandler_PayMapsServiceErrors(t *testing.T) {
	privateDetail := "private database failure"
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   errorCode
		wantSilent bool
	}{
		{"deadline", context.DeadlineExceeded, http.StatusGatewayTimeout, codeDeadlineExceeded, false},
		{"missing reservation", flashsale.ErrReservationNotFound, http.StatusNotFound, codeNotFound, false},
		{"foreign reservation", flashsale.ErrReservationNotFound, http.StatusNotFound, codeNotFound, false},
		{"forbidden transition", flashsale.ErrForbiddenTransition, http.StatusConflict, codeConflict, false},
		{"expired", flashsale.ErrExpiredTimeWindow, http.StatusConflict, codeConflict, false},
		{"order conflict", flashsale.ErrConflict, http.StatusConflict, codeConflict, false},
		{"canceled", context.Canceled, 0, "", true},
		{"unknown", errors.New(privateDetail), http.StatusInternalServerError, codeInternalError, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := &reservationServiceStub{
				pay: func(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, time.Time) (flashsale.Order, error) {
					return flashsale.Order{}, tt.err
				},
			}
			handler := newReservationHandlerForTest(
				t,
				service,
				func() uuid.UUID { return payTestOrderID },
				func() time.Time { return reserveTestNow },
				15*time.Minute,
			)
			request := payRequestForTest(
				t,
				http.MethodPost,
				reserveTestID.String(),
				&identity.AuthenticateResult{UserID: reserveTestUserID, Role: identity.RoleUser},
			)

			recorder := servePay(t, handler, request)

			if service.payCalls != 1 {
				t.Fatalf("Pay calls = %d, want 1", service.payCalls)
			}
			if tt.wantSilent {
				if recorder.Body.Len() != 0 {
					t.Fatalf("body = %q, want empty", recorder.Body.String())
				}
				return
			}

			assertErrorResponse(t, recorder, tt.wantStatus, string(tt.wantCode))
			if strings.Contains(recorder.Body.String(), privateDetail) {
				t.Fatalf("private error leaked to client: %s", recorder.Body)
			}
		})
	}
}

func TestReservationHandler_PayCanceledContextDoesNotCallServiceOrWrite(t *testing.T) {
	service := &reservationServiceStub{}
	handler := newReservationHandlerForTest(t, service, uuid.New, time.Now, time.Minute)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	request := httptest.NewRequestWithContext(
		ctx,
		http.MethodPost,
		"/v1/reservations/"+reserveTestID.String()+"/pay",
		nil,
	)
	recorder := httptest.NewRecorder()

	handler.Pay(recorder, request)

	if service.payCalls != 0 {
		t.Fatalf("Pay calls = %d, want 0", service.payCalls)
	}
	if recorder.Body.Len() != 0 || len(recorder.Header()) != 0 {
		t.Fatalf("response = headers %#v body %q, want untouched", recorder.Header(), recorder.Body.String())
	}
}

func TestReservationHandler_PayServiceCancellationDoesNotWrite(t *testing.T) {
	requestContext, cancel := context.WithCancel(t.Context())
	service := &reservationServiceStub{
		pay: func(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, time.Time) (flashsale.Order, error) {
			cancel()
			return flashsale.Order{}, errors.New("operation interrupted after cancellation")
		},
	}
	handler := newReservationHandlerForTest(
		t,
		service,
		func() uuid.UUID { return payTestOrderID },
		func() time.Time { return reserveTestNow },
		time.Minute,
	)
	request := httptest.NewRequestWithContext(
		requestContext,
		http.MethodPost,
		"/v1/reservations/"+reserveTestID.String()+"/pay",
		nil,
	)
	request.SetPathValue(reservationIDPathName, reserveTestID.String())
	request = request.WithContext(context.WithValue(
		request.Context(),
		principalKey,
		&identity.AuthenticateResult{UserID: reserveTestUserID, Role: identity.RoleUser},
	))
	recorder := httptest.NewRecorder()

	handler.Pay(recorder, request)

	if service.payCalls != 1 {
		t.Fatalf("Pay calls = %d, want 1", service.payCalls)
	}
	if recorder.Body.Len() != 0 || len(recorder.Header()) != 0 {
		t.Fatalf("response = headers %#v body %q, want untouched", recorder.Header(), recorder.Body.String())
	}
}
