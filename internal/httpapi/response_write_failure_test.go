package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/yyeart/flashdrop/internal/flashsale"
	"github.com/yyeart/flashdrop/internal/identity"
)

type responseWriteFailureCase struct {
	name       string
	newHandler func(*testing.T, *slog.Logger) http.Handler
	newRequest func(*testing.T) *http.Request
	wantReason string
}

func TestReservationAndOrderHandlers_ResponseWriteFailureIsLoggedSafely(
	t *testing.T,
) {
	for _, tt := range responseWriteFailureCases() {
		t.Run(tt.name, func(t *testing.T) {
			testResponseWriteFailure(t, tt)
		})
	}
}

func responseWriteFailureCases() []responseWriteFailureCase {
	return []responseWriteFailureCase{
		{
			name: "reserve",
			newHandler: func(t *testing.T, logger *slog.Logger) http.Handler {
				t.Helper()

				service := &reservationServiceStub{
					reserve: func(
						_ context.Context,
						command flashsale.ReserveCommand,
						now time.Time,
					) (flashsale.ReserveResult, error) {
						return reservationResultForCommand(t, command, now, false), nil
					},
				}
				handler, err := NewReservationHandler(
					service,
					logger,
					func() uuid.UUID { return reserveTestID },
					func() time.Time { return reserveTestNow },
					15*time.Minute,
				)
				if err != nil {
					t.Fatalf("NewReservationHandler() error = %v", err)
				}

				return http.HandlerFunc(handler.Reserve)
			},
			newRequest: func(t *testing.T) *http.Request {
				t.Helper()

				request := reserveRequestForTest(
					t,
					http.MethodPost,
					`{"sale_item_id":"22222222-2222-2222-2222-222222222222","quantity":1}`,
					&identity.AuthenticateResult{
						UserID: reserveTestUserID,
						Role:   identity.RoleUser,
					},
				)
				request.Header.Set("Idempotency-Key", "secret-idempotency-key")

				return request
			},
			wantReason: "reserve_result_write_failed",
		},
		{
			name: "pay",
			newHandler: func(t *testing.T, logger *slog.Logger) http.Handler {
				t.Helper()

				service := &reservationServiceStub{
					pay: func(
						_ context.Context,
						userID uuid.UUID,
						reservationID uuid.UUID,
						orderID uuid.UUID,
						now time.Time,
					) (flashsale.Order, error) {
						return newPayTestOrder(
							t,
							orderID,
							reservationID,
							userID,
							now,
						), nil
					},
				}
				handler, err := NewReservationHandler(
					service,
					logger,
					func() uuid.UUID { return payTestOrderID },
					func() time.Time { return reserveTestNow },
					15*time.Minute,
				)
				if err != nil {
					t.Fatalf("NewReservationHandler() error = %v", err)
				}

				return http.HandlerFunc(handler.Pay)
			},
			newRequest: func(t *testing.T) *http.Request {
				t.Helper()

				return payRequestForTest(
					t,
					http.MethodPost,
					reserveTestID.String(),
					&identity.AuthenticateResult{
						UserID: reserveTestUserID,
						Role:   identity.RoleUser,
					},
				)
			},
			wantReason: "pay_response_write_failed",
		},
		{
			name: "get order",
			newHandler: func(t *testing.T, logger *slog.Logger) http.Handler {
				t.Helper()

				service := &orderHandlerServiceStub{
					find: func(
						context.Context,
						uuid.UUID,
						uuid.UUID,
					) (flashsale.Order, error) {
						return orderForHandlerTest(t), nil
					},
				}
				handler, err := NewOrderHandler(service, logger)
				if err != nil {
					t.Fatalf("NewOrderHandler() error = %v", err)
				}

				return http.HandlerFunc(handler.Get)
			},
			newRequest: func(t *testing.T) *http.Request {
				t.Helper()

				return orderRequestForTest(
					t,
					http.MethodGet,
					orderHandlerTestID.String(),
					&identity.AuthenticateResult{
						UserID: orderHandlerUserID,
						Role:   identity.RoleUser,
					},
				)
			},
			wantReason: "get_order_response_write_failed",
		},
	}
}

func testResponseWriteFailure(t *testing.T, tt responseWriteFailureCase) {
	t.Helper()

	logger, output := testLogger()
	request := tt.newRequest(t)
	request.Header.Set("Authorization", "Bearer secret-bearer-token")
	writer := &partialWriteResponseWriter{
		header:   make(http.Header),
		writeErr: errors.New("forced response write failure"),
	}

	requestIDMiddleware(t)(tt.newHandler(t, logger)).ServeHTTP(writer, request)

	logOutput := output.String()
	if !strings.Contains(logOutput, "HTTP handler failed") ||
		!strings.Contains(logOutput, tt.wantReason) {
		t.Fatalf("log output = %q, want safe handler failure record", logOutput)
	}

	for _, secret := range []string{
		"secret-bearer-token",
		"secret-idempotency-key",
		`"sale_item_id"`,
	} {
		if strings.Contains(logOutput, secret) {
			t.Errorf("log output leaked %q: %s", secret, logOutput)
		}
	}
}
