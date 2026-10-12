package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/yyeart/flashdrop/internal/flashsale"
	"github.com/yyeart/flashdrop/internal/identity"
)

type controllableDeadlineContext struct {
	context.Context
	done chan struct{}
	err  error
}

func newControllableDeadlineContext(parent context.Context) *controllableDeadlineContext {
	return &controllableDeadlineContext{
		Context: parent,
		done:    make(chan struct{}),
	}
}

func (c *controllableDeadlineContext) Done() <-chan struct{} {
	return c.done
}

func (c *controllableDeadlineContext) Err() error {
	return c.err
}

func (c *controllableDeadlineContext) expire() {
	c.err = context.DeadlineExceeded
	close(c.done)
}

type handlerDeadlineCase struct {
	name  string
	setup func(*testing.T) (http.Handler, *http.Request, func() int)
}

func TestReservationAndOrderHandlers_ExpiredRequestContextReturnsGatewayTimeout(
	t *testing.T,
) {
	for _, tt := range handlerDeadlineCases() {
		t.Run(tt.name, func(t *testing.T) {
			handler, request, serviceCalls := tt.setup(t)
			recorder := httptest.NewRecorder()

			requestIDMiddleware(t)(handler).ServeHTTP(recorder, request)

			assertErrorResponse(
				t,
				recorder,
				http.StatusGatewayTimeout,
				string(codeDeadlineExceeded),
			)
			if got := serviceCalls(); got != 1 {
				t.Fatalf("service calls = %d, want 1", got)
			}
		})
	}
}

func handlerDeadlineCases() []handlerDeadlineCase {
	return []handlerDeadlineCase{
		{
			name:  "reserve",
			setup: setupReserveDeadlineCase,
		},
		{
			name:  "cancel",
			setup: setupCancelDeadlineCase,
		},
		{
			name:  "pay",
			setup: setupPayDeadlineCase,
		},
		{
			name:  "get order",
			setup: setupGetOrderDeadlineCase,
		},
	}
}

func setupReserveDeadlineCase(
	t *testing.T,
) (http.Handler, *http.Request, func() int) {
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
	deadlineContext := newControllableDeadlineContext(request.Context())
	service := &reservationServiceStub{
		reserve: func(
			context.Context,
			flashsale.ReserveCommand,
			time.Time,
		) (flashsale.ReserveResult, error) {
			deadlineContext.expire()

			return flashsale.ReserveResult{}, context.DeadlineExceeded
		},
	}
	handler := newReservationHandlerForTest(
		t,
		service,
		func() uuid.UUID { return reserveTestID },
		func() time.Time { return reserveTestNow },
		15*time.Minute,
	)

	return http.HandlerFunc(handler.Reserve),
		request.WithContext(deadlineContext),
		func() int { return service.reserveCalls }
}

func setupCancelDeadlineCase(
	t *testing.T,
) (http.Handler, *http.Request, func() int) {
	t.Helper()

	request := cancelRequestForTest(
		t,
		http.MethodPost,
		reserveTestID.String(),
		&identity.AuthenticateResult{
			UserID: reserveTestUserID,
			Role:   identity.RoleUser,
		},
	)
	deadlineContext := newControllableDeadlineContext(request.Context())
	service := &reservationServiceStub{
		cancel: func(context.Context, uuid.UUID, uuid.UUID, time.Time) error {
			deadlineContext.expire()

			return context.DeadlineExceeded
		},
	}
	handler := newReservationHandlerForTest(
		t,
		service,
		uuid.New,
		func() time.Time { return reserveTestNow },
		15*time.Minute,
	)

	return http.HandlerFunc(handler.Cancel),
		request.WithContext(deadlineContext),
		func() int { return service.cancelCalls }
}

func setupPayDeadlineCase(
	t *testing.T,
) (http.Handler, *http.Request, func() int) {
	t.Helper()

	request := payRequestForTest(
		t,
		http.MethodPost,
		reserveTestID.String(),
		&identity.AuthenticateResult{
			UserID: reserveTestUserID,
			Role:   identity.RoleUser,
		},
	)
	deadlineContext := newControllableDeadlineContext(request.Context())
	service := &reservationServiceStub{
		pay: func(
			context.Context,
			uuid.UUID,
			uuid.UUID,
			uuid.UUID,
			time.Time,
		) (flashsale.Order, error) {
			deadlineContext.expire()

			return flashsale.Order{}, context.DeadlineExceeded
		},
	}
	handler := newReservationHandlerForTest(
		t,
		service,
		func() uuid.UUID { return payTestOrderID },
		func() time.Time { return reserveTestNow },
		15*time.Minute,
	)

	return http.HandlerFunc(handler.Pay),
		request.WithContext(deadlineContext),
		func() int { return service.payCalls }
}

func setupGetOrderDeadlineCase(
	t *testing.T,
) (http.Handler, *http.Request, func() int) {
	t.Helper()

	request := orderRequestForTest(
		t,
		http.MethodGet,
		orderHandlerTestID.String(),
		&identity.AuthenticateResult{
			UserID: orderHandlerUserID,
			Role:   identity.RoleUser,
		},
	)
	deadlineContext := newControllableDeadlineContext(request.Context())
	service := &orderHandlerServiceStub{
		find: func(
			context.Context,
			uuid.UUID,
			uuid.UUID,
		) (flashsale.Order, error) {
			deadlineContext.expire()

			return flashsale.Order{}, context.DeadlineExceeded
		},
	}
	handler := newOrderHandlerForTest(t, service)

	return http.HandlerFunc(handler.Get),
		request.WithContext(deadlineContext),
		func() int { return service.findCalls }
}
