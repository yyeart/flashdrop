package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/yyeart/flashdrop/internal/flashsale"
	"github.com/yyeart/flashdrop/internal/identity"
)

var (
	orderHandlerTestID = uuid.MustParse(
		"55555555-5555-5555-5555-555555555555",
	)
	orderHandlerReservationID = uuid.MustParse(
		"66666666-6666-6666-6666-666666666666",
	)
	orderHandlerUserID = uuid.MustParse(
		"77777777-7777-7777-7777-777777777777",
	)
	orderHandlerSaleItemID = uuid.MustParse(
		"88888888-8888-8888-8888-888888888888",
	)
	orderHandlerCreatedAt = time.Date(
		2026,
		time.September,
		19,
		12,
		0,
		0,
		0,
		time.UTC,
	)
)

type orderHandlerServiceStub struct {
	find func(context.Context, uuid.UUID, uuid.UUID) (flashsale.Order, error)

	findCalls  int
	gotUserID  uuid.UUID
	gotOrderID uuid.UUID
}

func (s *orderHandlerServiceStub) FindOrder(
	ctx context.Context,
	userID uuid.UUID,
	orderID uuid.UUID,
) (flashsale.Order, error) {
	s.findCalls++
	s.gotUserID = userID
	s.gotOrderID = orderID

	if s.find == nil {
		return flashsale.Order{}, errors.New("unexpected FindOrder call")
	}

	return s.find(ctx, userID, orderID)
}

func newOrderHandlerForTest(
	t *testing.T,
	service orderService,
) *OrderHandler {
	t.Helper()

	handler, err := NewOrderHandler(
		service,
		slog.New(slog.DiscardHandler),
	)
	if err != nil {
		t.Fatalf("NewOrderHandler() error = %v", err)
	}

	return handler
}

func orderRequestForTest(
	t *testing.T,
	method string,
	orderID string,
	principal *identity.AuthenticateResult,
) *http.Request {
	t.Helper()

	request := httptest.NewRequestWithContext(
		t.Context(),
		method,
		"/v1/orders/"+orderID,
		nil,
	)
	if orderID != "" {
		request.SetPathValue(orderIDPathName, orderID)
	}
	if principal != nil {
		request = request.WithContext(context.WithValue(
			request.Context(),
			principalKey,
			principal,
		))
	}

	return request
}

func serveOrderGet(
	t *testing.T,
	handler *OrderHandler,
	request *http.Request,
) *httptest.ResponseRecorder {
	t.Helper()

	recorder := httptest.NewRecorder()
	requestIDMiddleware(t)(http.HandlerFunc(handler.Get)).ServeHTTP(
		recorder,
		request,
	)

	return recorder
}

func orderForHandlerTest(t *testing.T) flashsale.Order {
	t.Helper()

	order, err := flashsale.NewOrder(flashsale.NewOrderInput{
		ID:            orderHandlerTestID,
		ReservationID: orderHandlerReservationID,
		UserID:        orderHandlerUserID,
		SaleItemID:    orderHandlerSaleItemID,
		Quantity:      2,
		CreatedAt:     orderHandlerCreatedAt,
	})
	if err != nil {
		t.Fatalf("NewOrder() error = %v", err)
	}

	return order
}

func TestOrderHandler_GetReturnsOwnedOrder(t *testing.T) {
	wantOrder := orderForHandlerTest(t)
	service := &orderHandlerServiceStub{
		find: func(
			_ context.Context,
			userID uuid.UUID,
			orderID uuid.UUID,
		) (flashsale.Order, error) {
			if userID != orderHandlerUserID {
				t.Errorf("FindOrder userID = %s, want %s", userID, orderHandlerUserID)
			}
			if orderID != orderHandlerTestID {
				t.Errorf("FindOrder orderID = %s, want %s", orderID, orderHandlerTestID)
			}

			return wantOrder, nil
		},
	}
	handler := newOrderHandlerForTest(t, service)
	request := orderRequestForTest(
		t,
		http.MethodGet,
		orderHandlerTestID.String(),
		&identity.AuthenticateResult{
			UserID: orderHandlerUserID,
			Role:   identity.RoleUser,
		},
	)

	recorder := serveOrderGet(t, handler, request)

	assertOrderHandlerServiceCall(t, service)
	assertOrderHandlerSuccessResponse(t, recorder)
}

func assertOrderHandlerServiceCall(
	t *testing.T,
	service *orderHandlerServiceStub,
) {
	t.Helper()

	if service.findCalls != 1 {
		t.Fatalf("FindOrder calls = %d, want 1", service.findCalls)
	}
	if service.gotUserID != orderHandlerUserID {
		t.Errorf(
			"FindOrder userID = %s, want %s",
			service.gotUserID,
			orderHandlerUserID,
		)
	}
	if service.gotOrderID != orderHandlerTestID {
		t.Errorf(
			"FindOrder orderID = %s, want %s",
			service.gotOrderID,
			orderHandlerTestID,
		)
	}
}

func assertOrderHandlerSuccessResponse(
	t *testing.T,
	recorder *httptest.ResponseRecorder,
) {
	t.Helper()

	if recorder.Code != http.StatusOK {
		t.Fatalf(
			"status = %d, want %d; body = %s",
			recorder.Code,
			http.StatusOK,
			recorder.Body,
		)
	}
	if got := recorder.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}

	var response orderResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	want := orderResponse{
		ID:            orderHandlerTestID,
		ReservationID: orderHandlerReservationID,
		UserID:        orderHandlerUserID,
		SaleItemID:    orderHandlerSaleItemID,
		Quantity:      2,
		CreatedAt:     orderHandlerCreatedAt,
	}
	if !reflect.DeepEqual(response, want) {
		t.Fatalf("response = %#v, want %#v", response, want)
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(recorder.Body.Bytes(), &fields); err != nil {
		t.Fatalf("decode response fields: %v", err)
	}
	if len(fields) != 6 {
		t.Fatalf("response fields = %v, want exactly 6 fields", fields)
	}
}

func TestOrderHandler_GetRejectsInvalidRequestWithoutCallingService(
	t *testing.T,
) {
	validPrincipal := &identity.AuthenticateResult{
		UserID: orderHandlerUserID,
		Role:   identity.RoleUser,
	}

	tests := []struct {
		name       string
		method     string
		orderID    string
		principal  *identity.AuthenticateResult
		wantStatus int
		wantCode   errorCode
		wantAllow  string
	}{
		{
			name: "wrong method", method: http.MethodPost,
			orderID: orderHandlerTestID.String(), principal: validPrincipal,
			wantStatus: http.StatusMethodNotAllowed, wantCode: codeMethodNotAllowed,
			wantAllow: http.MethodGet,
		},
		{
			name: "missing principal", method: http.MethodGet,
			orderID: orderHandlerTestID.String(), principal: nil,
			wantStatus: http.StatusUnauthorized, wantCode: codeUnauthorized,
		},
		{
			name: "nil principal user ID", method: http.MethodGet,
			orderID:    orderHandlerTestID.String(),
			principal:  &identity.AuthenticateResult{Role: identity.RoleUser},
			wantStatus: http.StatusUnauthorized, wantCode: codeUnauthorized,
		},
		{
			name: "missing order ID", method: http.MethodGet,
			orderID: "", principal: validPrincipal,
			wantStatus: http.StatusBadRequest, wantCode: codeBadRequest,
		},
		{
			name: "malformed order ID", method: http.MethodGet,
			orderID: "not-a-uuid", principal: validPrincipal,
			wantStatus: http.StatusBadRequest, wantCode: codeBadRequest,
		},
		{
			name: "nil order ID", method: http.MethodGet,
			orderID: uuid.Nil.String(), principal: validPrincipal,
			wantStatus: http.StatusBadRequest, wantCode: codeBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := &orderHandlerServiceStub{}
			handler := newOrderHandlerForTest(t, service)
			request := orderRequestForTest(
				t,
				tt.method,
				tt.orderID,
				tt.principal,
			)

			recorder := serveOrderGet(t, handler, request)

			assertErrorResponse(
				t,
				recorder,
				tt.wantStatus,
				string(tt.wantCode),
			)
			if got := recorder.Header().Get("Allow"); got != tt.wantAllow {
				t.Fatalf("Allow = %q, want %q", got, tt.wantAllow)
			}
			if service.findCalls != 0 {
				t.Fatalf("FindOrder calls = %d, want 0", service.findCalls)
			}
		})
	}
}

func TestOrderHandler_GetMapsServiceErrors(t *testing.T) {
	privateDetail := "private database failure"
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   errorCode
		wantSilent bool
	}{
		{
			"deadline",
			context.DeadlineExceeded,
			http.StatusGatewayTimeout,
			codeDeadlineExceeded,
			false,
		},
		{
			"missing order",
			flashsale.ErrOrderNotFound,
			http.StatusNotFound,
			codeNotFound,
			false,
		},
		{
			"foreign order",
			flashsale.ErrOrderNotFound,
			http.StatusNotFound,
			codeNotFound,
			false,
		},
		{"canceled", context.Canceled, 0, "", true},
		{
			"unknown",
			errors.New(privateDetail),
			http.StatusInternalServerError,
			codeInternalError,
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := &orderHandlerServiceStub{
				find: func(
					context.Context,
					uuid.UUID,
					uuid.UUID,
				) (flashsale.Order, error) {
					return flashsale.Order{}, tt.err
				},
			}
			handler := newOrderHandlerForTest(t, service)
			request := orderRequestForTest(
				t,
				http.MethodGet,
				orderHandlerTestID.String(),
				&identity.AuthenticateResult{
					UserID: orderHandlerUserID,
					Role:   identity.RoleUser,
				},
			)

			recorder := serveOrderGet(t, handler, request)

			if service.findCalls != 1 {
				t.Fatalf("FindOrder calls = %d, want 1", service.findCalls)
			}
			if tt.wantSilent {
				if recorder.Body.Len() != 0 {
					t.Fatalf("body = %q, want empty", recorder.Body.String())
				}

				return
			}

			assertErrorResponse(
				t,
				recorder,
				tt.wantStatus,
				string(tt.wantCode),
			)
			if strings.Contains(recorder.Body.String(), privateDetail) {
				t.Fatalf("private error leaked to client: %s", recorder.Body)
			}
		})
	}
}

func TestOrderHandler_GetCanceledContextDoesNotCallServiceOrWrite(
	t *testing.T,
) {
	service := &orderHandlerServiceStub{}
	handler := newOrderHandlerForTest(t, service)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	request := httptest.NewRequestWithContext(
		ctx,
		http.MethodGet,
		"/v1/orders/"+orderHandlerTestID.String(),
		nil,
	)
	recorder := httptest.NewRecorder()

	handler.Get(recorder, request)

	if service.findCalls != 0 {
		t.Fatalf("FindOrder calls = %d, want 0", service.findCalls)
	}
	if recorder.Body.Len() != 0 || len(recorder.Header()) != 0 {
		t.Fatalf(
			"response = headers %#v body %q, want untouched",
			recorder.Header(),
			recorder.Body.String(),
		)
	}
}

func TestOrderHandler_GetServiceCancellationDoesNotWrite(t *testing.T) {
	requestContext, cancel := context.WithCancel(t.Context())
	service := &orderHandlerServiceStub{
		find: func(
			context.Context,
			uuid.UUID,
			uuid.UUID,
		) (flashsale.Order, error) {
			cancel()

			return flashsale.Order{}, errors.New(
				"operation interrupted after cancellation",
			)
		},
	}
	handler := newOrderHandlerForTest(t, service)
	request := httptest.NewRequestWithContext(
		requestContext,
		http.MethodGet,
		"/v1/orders/"+orderHandlerTestID.String(),
		nil,
	)
	request.SetPathValue(orderIDPathName, orderHandlerTestID.String())
	request = request.WithContext(context.WithValue(
		request.Context(),
		principalKey,
		&identity.AuthenticateResult{
			UserID: orderHandlerUserID,
			Role:   identity.RoleUser,
		},
	))
	recorder := httptest.NewRecorder()

	handler.Get(recorder, request)

	if service.findCalls != 1 {
		t.Fatalf("FindOrder calls = %d, want 1", service.findCalls)
	}
	if recorder.Body.Len() != 0 || len(recorder.Header()) != 0 {
		t.Fatalf(
			"response = headers %#v body %q, want untouched",
			recorder.Header(),
			recorder.Body.String(),
		)
	}
}
