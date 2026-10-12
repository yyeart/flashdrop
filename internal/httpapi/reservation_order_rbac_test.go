package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/yyeart/flashdrop/internal/flashsale"
	"github.com/yyeart/flashdrop/internal/identity"
)

type reservationOrderRBACEndpoint struct {
	name          string
	handler       func(*ReservationHandler, *OrderHandler) http.Handler
	request       func(*testing.T) *http.Request
	successStatus int
	serviceCalls  func(*reservationServiceStub, *orderHandlerServiceStub) int
	serviceUserID func(*reservationServiceStub, *orderHandlerServiceStub) uuid.UUID
}

type reservationOrderRBACAccess struct {
	name      string
	principal *identity.AuthenticateResult
	wantCode  errorCode
	wantCalls int
}

func TestReservationAndOrderHandlers_AllowUserAndAdminForOwnResources(t *testing.T) {
	t.Parallel()

	userID := uuid.MustParse("99999999-9999-9999-9999-999999999991")
	adminID := uuid.MustParse("99999999-9999-9999-9999-999999999992")

	accessCases := []reservationOrderRBACAccess{
		{
			name:      "missing principal",
			wantCode:  codeUnauthorized,
			wantCalls: 0,
		},
		{
			name: "user role",
			principal: &identity.AuthenticateResult{
				UserID: userID,
				Role:   identity.RoleUser,
			},
			wantCalls: 1,
		},
		{
			name: "admin role",
			principal: &identity.AuthenticateResult{
				UserID: adminID,
				Role:   identity.RoleAdmin,
			},
			wantCalls: 1,
		},
	}

	for _, endpoint := range reservationOrderRBACEndpoints() {
		for _, access := range accessCases {
			t.Run(endpoint.name+"/"+access.name, func(t *testing.T) {
				t.Parallel()

				testReservationOrderRBACAccess(t, endpoint, access)
			})
		}
	}
}

func testReservationOrderRBACAccess(
	t *testing.T,
	endpoint reservationOrderRBACEndpoint,
	access reservationOrderRBACAccess,
) {
	t.Helper()

	reservationService, orderService := newReservationOrderRBACServices(t)
	reservationHandler, orderHandler := newReservationOrderRBACHandlers(
		t,
		reservationService,
		orderService,
	)
	protected := Chain(
		endpoint.handler(reservationHandler, orderHandler),
		requestIDMiddleware(t),
		reservationOrderRBACMiddleware(t),
	)
	request := endpoint.request(t)
	if access.principal != nil {
		request = request.WithContext(context.WithValue(
			request.Context(),
			principalKey,
			access.principal,
		))
	}

	recorder := httptest.NewRecorder()
	protected.ServeHTTP(recorder, request)

	wantStatus := http.StatusUnauthorized
	if access.principal != nil {
		wantStatus = endpoint.successStatus
	}
	if recorder.Code != wantStatus {
		t.Fatalf(
			"status = %d, want %d; body = %s",
			recorder.Code,
			wantStatus,
			recorder.Body,
		)
	}
	if access.wantCode != "" {
		assertErrorResponse(t, recorder, wantStatus, string(access.wantCode))
	}
	if got := endpoint.serviceCalls(reservationService, orderService); got != access.wantCalls {
		t.Fatalf("service calls = %d, want %d", got, access.wantCalls)
	}
	if access.principal != nil {
		if got := endpoint.serviceUserID(reservationService, orderService); got != access.principal.UserID {
			t.Errorf("service user ID = %s, want principal user ID %s", got, access.principal.UserID)
		}
	}
}

func TestOrderHandler_AdminCannotBypassOwnership(t *testing.T) {
	t.Parallel()

	adminID := uuid.MustParse("99999999-9999-9999-9999-999999999992")
	service := &orderHandlerServiceStub{
		find: func(_ context.Context, userID, _ uuid.UUID) (flashsale.Order, error) {
			if userID != adminID {
				t.Errorf("FindOrder userID = %s, want admin principal ID %s", userID, adminID)
			}

			return flashsale.Order{}, flashsale.ErrOrderNotFound
		},
	}
	handler := newOrderHandlerForTest(t, service)
	protected := Chain(
		http.HandlerFunc(handler.Get),
		requestIDMiddleware(t),
		reservationOrderRBACMiddleware(t),
	)
	request := orderRequestForTest(t, http.MethodGet, orderHandlerTestID.String(), nil)
	request = request.WithContext(context.WithValue(
		request.Context(),
		principalKey,
		&identity.AuthenticateResult{UserID: adminID, Role: identity.RoleAdmin},
	))

	recorder := httptest.NewRecorder()
	protected.ServeHTTP(recorder, request)

	assertErrorResponse(t, recorder, http.StatusNotFound, string(codeNotFound))
	if service.findCalls != 1 {
		t.Fatalf("FindOrder calls = %d, want 1", service.findCalls)
	}
	if service.gotUserID != adminID {
		t.Errorf("FindOrder userID = %s, want %s", service.gotUserID, adminID)
	}
}

func reservationOrderRBACEndpoints() []reservationOrderRBACEndpoint {
	return []reservationOrderRBACEndpoint{
		{
			name: "reserve",
			handler: func(reservation *ReservationHandler, _ *OrderHandler) http.Handler {
				return http.HandlerFunc(reservation.Reserve)
			},
			request: func(t *testing.T) *http.Request {
				t.Helper()

				return reserveRequestForTest(
					t,
					http.MethodPost,
					`{"sale_item_id":"22222222-2222-2222-2222-222222222222","quantity":1}`,
					nil,
				)
			},
			successStatus: http.StatusCreated,
			serviceCalls: func(reservation *reservationServiceStub, _ *orderHandlerServiceStub) int {
				return reservation.reserveCalls
			},
			serviceUserID: func(reservation *reservationServiceStub, _ *orderHandlerServiceStub) uuid.UUID {
				return reservation.gotCommand.UserID
			},
		},
		{
			name: "cancel",
			handler: func(reservation *ReservationHandler, _ *OrderHandler) http.Handler {
				return http.HandlerFunc(reservation.Cancel)
			},
			request: func(t *testing.T) *http.Request {
				t.Helper()

				return cancelRequestForTest(t, http.MethodPost, reserveTestID.String(), nil)
			},
			successStatus: http.StatusNoContent,
			serviceCalls: func(reservation *reservationServiceStub, _ *orderHandlerServiceStub) int {
				return reservation.cancelCalls
			},
			serviceUserID: func(reservation *reservationServiceStub, _ *orderHandlerServiceStub) uuid.UUID {
				return reservation.gotCancelUserID
			},
		},
		{
			name: "pay",
			handler: func(reservation *ReservationHandler, _ *OrderHandler) http.Handler {
				return http.HandlerFunc(reservation.Pay)
			},
			request: func(t *testing.T) *http.Request {
				t.Helper()

				return payRequestForTest(t, http.MethodPost, reserveTestID.String(), nil)
			},
			successStatus: http.StatusCreated,
			serviceCalls: func(reservation *reservationServiceStub, _ *orderHandlerServiceStub) int {
				return reservation.payCalls
			},
			serviceUserID: func(reservation *reservationServiceStub, _ *orderHandlerServiceStub) uuid.UUID {
				return reservation.gotPayUserID
			},
		},
		{
			name: "get order",
			handler: func(_ *ReservationHandler, order *OrderHandler) http.Handler {
				return http.HandlerFunc(order.Get)
			},
			request: func(t *testing.T) *http.Request {
				t.Helper()

				return orderRequestForTest(t, http.MethodGet, orderHandlerTestID.String(), nil)
			},
			successStatus: http.StatusOK,
			serviceCalls: func(_ *reservationServiceStub, order *orderHandlerServiceStub) int {
				return order.findCalls
			},
			serviceUserID: func(_ *reservationServiceStub, order *orderHandlerServiceStub) uuid.UUID {
				return order.gotUserID
			},
		},
	}
}

func newReservationOrderRBACHandlers(
	t *testing.T,
	reservationService reservationService,
	orderService orderService,
) (*ReservationHandler, *OrderHandler) {
	t.Helper()

	reservationHandler := newReservationHandlerForTest(
		t,
		reservationService,
		func() uuid.UUID { return reserveTestID },
		func() time.Time { return reserveTestNow },
		15*time.Minute,
	)
	orderHandler := newOrderHandlerForTest(t, orderService)

	return reservationHandler, orderHandler
}

func newReservationOrderRBACServices(
	t *testing.T,
) (*reservationServiceStub, *orderHandlerServiceStub) {
	t.Helper()

	reservationService := &reservationServiceStub{
		reserve: func(
			_ context.Context,
			command flashsale.ReserveCommand,
			now time.Time,
		) (flashsale.ReserveResult, error) {
			return reservationResultForCommand(t, command, now, false), nil
		},
		cancel: func(context.Context, uuid.UUID, uuid.UUID, time.Time) error {
			return nil
		},
		pay: func(
			_ context.Context,
			userID uuid.UUID,
			reservationID uuid.UUID,
			orderID uuid.UUID,
			now time.Time,
		) (flashsale.Order, error) {
			return flashsale.NewOrder(flashsale.NewOrderInput{
				ID:            orderID,
				ReservationID: reservationID,
				UserID:        userID,
				SaleItemID:    reserveTestItemID,
				Quantity:      1,
				CreatedAt:     now,
			})
		},
	}
	orderService := &orderHandlerServiceStub{
		find: func(_ context.Context, userID, orderID uuid.UUID) (flashsale.Order, error) {
			return flashsale.NewOrder(flashsale.NewOrderInput{
				ID:            orderID,
				ReservationID: orderHandlerReservationID,
				UserID:        userID,
				SaleItemID:    orderHandlerSaleItemID,
				Quantity:      1,
				CreatedAt:     orderHandlerCreatedAt,
			})
		},
	}

	return reservationService, orderService
}

func reservationOrderRBACMiddleware(t *testing.T) Middleware {
	t.Helper()

	rbac, err := NewRBAC(
		slog.New(slog.DiscardHandler),
		identity.RoleUser,
		identity.RoleAdmin,
	)
	if err != nil {
		t.Fatalf("NewRBAC() error = %v", err)
	}

	return rbac
}
