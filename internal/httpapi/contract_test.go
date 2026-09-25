package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
	"github.com/getkin/kin-openapi/routers/gorillamux"
	"github.com/google/uuid"
	"github.com/yyeart/flashdrop/internal/flashsale"
	"github.com/yyeart/flashdrop/internal/identity"
)

type contractCase struct {
	name        string
	operationID string
	method      string
	path        string
	body        string
	token       string
	wantStatus  int
	setup       func(*testing.T, *routerFixture)
}

func loadContract(t *testing.T) (*openapi3.T, routers.Router) {
	t.Helper()

	doc, err := openapi3.NewLoader().LoadFromFile("../../api/openapi/openapi.yaml")
	if err != nil {
		t.Fatalf("load OpenAPI contract: %v", err)
	}
	if err := doc.Validate(t.Context()); err != nil {
		t.Fatalf("validate OpenAPI contract: %v", err)
	}

	router, err := gorillamux.NewRouter(doc)
	if err != nil {
		t.Fatalf("create OpenAPI router: %v", err)
	}
	return doc, router
}

func newContractRequest(t *testing.T, test contractCase) *http.Request {
	t.Helper()

	request := httptest.NewRequestWithContext(
		t.Context(), test.method, test.path, strings.NewReader(test.body),
	)
	if test.body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	if test.token != "" {
		request.Header.Set("Authorization", "Bearer "+test.token)
	}
	if test.operationID == "reserveSaleItem" {
		request.Header.Set("Idempotency-Key", "contract-test-key")
	}
	return request
}

func contractInput(
	t *testing.T,
	contractRouter routers.Router,
	request *http.Request,
) *openapi3filter.RequestValidationInput {
	t.Helper()

	route, pathParams, err := contractRouter.FindRoute(request)
	if err != nil {
		t.Fatalf("find OpenAPI operation for %s %s: %v", request.Method, request.URL.Path, err)
	}
	return &openapi3filter.RequestValidationInput{
		Request:    request,
		PathParams: pathParams,
		Route:      route,
		Options: &openapi3filter.Options{
			AuthenticationFunc: openapi3filter.NoopAuthenticationFunc,
		},
	}
}

func assertContractResponse(
	t *testing.T,
	input *openapi3filter.RequestValidationInput,
	recorder *httptest.ResponseRecorder,
	wantStatus int,
) {
	t.Helper()

	if recorder.Code != wantStatus {
		t.Fatalf("status = %d, want %d; body = %s", recorder.Code, wantStatus, recorder.Body)
	}
	if recorder.Header().Get("X-Request-ID") == "" {
		t.Error("X-Request-ID header is missing")
	}
	if wantStatus == http.StatusNoContent {
		if recorder.Body.Len() != 0 {
			t.Errorf("204 response body = %q, want empty", recorder.Body.String())
		}
	} else if got := recorder.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}

	response := recorder.Result()
	defer func() {
		if err := response.Body.Close(); err != nil {
			t.Errorf("close response body: %v", err)
		}
	}()
	if err := openapi3filter.ValidateResponse(t.Context(), &openapi3filter.ResponseValidationInput{
		RequestValidationInput: input,
		Status:                 response.StatusCode,
		Header:                 response.Header,
		Body:                   response.Body,
		Options: &openapi3filter.Options{
			IncludeResponseStatus: true,
		},
	}); err != nil {
		t.Errorf("response violates OpenAPI contract: %v; body = %s", err, recorder.Body)
	}
}

func contractSuccessCases() []contractCase {
	const credentials = `{"email":"user@example.com","password":"password123"}`
	const createSale = `{"starts_at":"2026-09-21T10:00:00Z","ends_at":"2026-09-22T10:00:00Z"}`
	const addItem = `{"product_id":"66666666-6666-6666-6666-666666666666","name":"Keyboard","price":"12.50","total_quantity":1}`
	const reserve = `{"sale_item_id":"` + routerTestItemID + `","quantity":1}`

	return []contractCase{
		{
			name: "register", operationID: "registerUser", method: http.MethodPost,
			path: "/v1/auth/register", body: credentials, wantStatus: http.StatusCreated,
			setup: func(t *testing.T, f *routerFixture) {
				service := newIdentityServiceForAuthTest(t, &authRegisterRepository{})
				f.authService.register = service.Register
			},
		},
		{
			name: "login", operationID: "loginUser", method: http.MethodPost,
			path: "/v1/auth/login", body: credentials, wantStatus: http.StatusOK,
			setup: func(_ *testing.T, f *routerFixture) {
				f.authService.login = func(context.Context, identity.LoginInput) (identity.LoginResult, error) {
					return identity.LoginResult{AccessToken: "header.payload.signature", ExpiresIn: 15 * time.Minute}, nil
				}
			},
		},
		{
			name: "list sales", operationID: "listActiveSales", method: http.MethodGet,
			path: "/v1/sales", wantStatus: http.StatusOK,
			setup: func(t *testing.T, f *routerFixture) {
				sale := activeSaleForHandlerTest(t, uuid.MustParse(routerTestSaleID))
				f.publicSaleService.list = func(context.Context, int, int) ([]flashsale.Sale, error) {
					return []flashsale.Sale{sale}, nil
				}
			},
		},
		{
			name: "get sale", operationID: "getActiveSale", method: http.MethodGet,
			path: "/v1/sales/" + routerTestSaleID, wantStatus: http.StatusOK,
			setup: func(t *testing.T, f *routerFixture) {
				sale := activeSaleForHandlerTest(t, uuid.MustParse(routerTestSaleID))
				f.publicSaleService.find = func(context.Context, uuid.UUID) (flashsale.Sale, error) {
					return sale, nil
				}
			},
		},
		{
			name: "reserve", operationID: "reserveSaleItem", method: http.MethodPost,
			path: "/v1/reservations", body: reserve, token: "user-token", wantStatus: http.StatusCreated,
			setup: func(t *testing.T, f *routerFixture) {
				f.reservationService.reserve = func(_ context.Context, cmd flashsale.ReserveCommand, now time.Time) (flashsale.ReserveResult, error) {
					return reservationResultForCommand(t, cmd, now, false), nil
				}
			},
		},
		{
			name: "cancel reservation", operationID: "cancelReservation", method: http.MethodPost,
			path: "/v1/reservations/" + routerTestReservationID + "/cancel", token: "user-token", wantStatus: http.StatusNoContent,
			setup: func(_ *testing.T, f *routerFixture) {
				f.reservationService.cancel = func(context.Context, uuid.UUID, uuid.UUID, time.Time) error { return nil }
			},
		},
		{
			name: "pay reservation", operationID: "payReservation", method: http.MethodPost,
			path: "/v1/reservations/" + routerTestReservationID + "/pay", token: "user-token", wantStatus: http.StatusCreated,
			setup: func(t *testing.T, f *routerFixture) {
				f.reservationService.pay = func(_ context.Context, userID, reservationID, orderID uuid.UUID, now time.Time) (flashsale.Order, error) {
					return newPayTestOrder(t, orderID, reservationID, userID, now), nil
				}
			},
		},
		{
			name: "get order", operationID: "getOrder", method: http.MethodGet,
			path: "/v1/orders/" + routerTestOrderID, token: "user-token", wantStatus: http.StatusOK,
			setup: func(t *testing.T, f *routerFixture) {
				f.orderService.find = func(_ context.Context, userID, orderID uuid.UUID) (flashsale.Order, error) {
					return newPayTestOrder(t, orderID, uuid.MustParse(routerTestReservationID), userID, time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)), nil
				}
			},
		},
		{
			name: "create sale", operationID: "createSale", method: http.MethodPost,
			path: "/v1/admin/sales", body: createSale, token: "admin-token", wantStatus: http.StatusCreated,
			setup: func(_ *testing.T, f *routerFixture) {
				f.adminSaleService.create = func(context.Context, flashsale.Sale) error { return nil }
			},
		},
		{
			name: "add sale item", operationID: "addSaleItem", method: http.MethodPost,
			path: "/v1/admin/sales/" + routerTestSaleID + "/items", body: addItem, token: "admin-token", wantStatus: http.StatusCreated,
			setup: func(_ *testing.T, f *routerFixture) {
				f.adminSaleService.addItem = func(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, string, flashsale.Money, int) error { return nil }
			},
		},
		{
			name: "activate sale", operationID: "activateSale", method: http.MethodPost,
			path: "/v1/admin/sales/" + routerTestSaleID + "/activate", token: "admin-token", wantStatus: http.StatusOK,
			setup: func(t *testing.T, f *routerFixture) {
				sale := lifecycleSale(t, flashsale.ActiveState)
				f.adminSaleService.activate = func(context.Context, uuid.UUID, time.Time) (flashsale.Sale, error) { return sale, nil }
			},
		},
		{
			name: "end sale", operationID: "endSale", method: http.MethodPost,
			path: "/v1/admin/sales/" + routerTestSaleID + "/end", token: "admin-token", wantStatus: http.StatusOK,
			setup: func(t *testing.T, f *routerFixture) {
				sale := lifecycleSale(t, flashsale.EndedState)
				f.adminSaleService.end = func(context.Context, uuid.UUID) (flashsale.Sale, error) { return sale, nil }
			},
		},
	}
}

func TestContract_SuccessResponses(t *testing.T) {
	doc, contractRouter := loadContract(t)
	cases := contractSuccessCases()
	covered := make(map[string]bool, len(cases))
	for _, test := range cases {
		if covered[test.operationID] {
			t.Fatalf("duplicate success case for operationId %q", test.operationID)
		}
		covered[test.operationID] = true

		t.Run(test.name, func(t *testing.T) {
			fixture := newRouterFixture(t)
			test.setup(t, fixture)
			input := contractInput(t, contractRouter, newContractRequest(t, test))
			if got := input.Route.Operation.OperationID; got != test.operationID {
				t.Fatalf("operationId = %q, want %q", got, test.operationID)
			}
			if err := openapi3filter.ValidateRequest(t.Context(), input); err != nil {
				t.Fatalf("request violates OpenAPI contract: %v", err)
			}

			recorder := httptest.NewRecorder()
			fixture.handler(t).ServeHTTP(recorder, newContractRequest(t, test))
			assertContractResponse(t, input, recorder, test.wantStatus)
		})
	}

	for path, item := range doc.Paths.Map() {
		for method, operation := range item.Operations() {
			if !covered[operation.OperationID] {
				t.Errorf("OpenAPI operation %s %s (%s) has no success case", method, path, operation.OperationID)
			}
		}
	}
}

func TestContract_ReservationReplay(t *testing.T) {
	_, contractRouter := loadContract(t)
	test := contractCase{
		name: "replayed reservation", operationID: "reserveSaleItem", method: http.MethodPost,
		path: "/v1/reservations", body: `{"sale_item_id":"` + routerTestItemID + `","quantity":1}`,
		token: "user-token", wantStatus: http.StatusOK,
		setup: func(t *testing.T, f *routerFixture) {
			f.reservationService.reserve = func(_ context.Context, cmd flashsale.ReserveCommand, now time.Time) (flashsale.ReserveResult, error) {
				return reservationResultForCommand(t, cmd, now, true), nil
			}
		},
	}

	fixture := newRouterFixture(t)
	test.setup(t, fixture)
	input := contractInput(t, contractRouter, newContractRequest(t, test))
	if err := openapi3filter.ValidateRequest(t.Context(), input); err != nil {
		t.Fatalf("request violates OpenAPI contract: %v", err)
	}

	recorder := httptest.NewRecorder()
	fixture.handler(t).ServeHTTP(recorder, newContractRequest(t, test))
	assertContractResponse(t, input, recorder, test.wantStatus)
}

type contractErrorCase struct {
	contractCase
	wantCode     errorCode
	validRequest bool
}

func TestContract_ErrorResponses(t *testing.T) {
	_, contractRouter := loadContract(t)
	tests := []contractErrorCase{
		{
			contractCase: contractCase{name: "malformed JSON", operationID: "registerUser", method: http.MethodPost,
				path: "/v1/auth/register", body: `{"email":`, wantStatus: http.StatusBadRequest},
			wantCode: codeBadRequest,
		},
		{
			contractCase: contractCase{name: "missing token", operationID: "getOrder", method: http.MethodGet,
				path: "/v1/orders/" + routerTestOrderID, wantStatus: http.StatusUnauthorized},
			wantCode: codeUnauthorized, validRequest: true,
		},
		{
			contractCase: contractCase{name: "user denied admin route", operationID: "createSale", method: http.MethodPost,
				path: "/v1/admin/sales", body: `{"starts_at":"2026-09-21T10:00:00Z","ends_at":"2026-09-22T10:00:00Z"}`,
				token: "user-token", wantStatus: http.StatusForbidden},
			wantCode: codeForbidden, validRequest: true,
		},
		{
			contractCase: contractCase{name: "sale not found", operationID: "getActiveSale", method: http.MethodGet,
				path: "/v1/sales/" + routerTestSaleID, wantStatus: http.StatusNotFound,
				setup: func(_ *testing.T, f *routerFixture) {
					f.publicSaleService.find = func(context.Context, uuid.UUID) (flashsale.Sale, error) {
						return flashsale.Sale{}, flashsale.ErrSaleNotFound
					}
				}},
			wantCode: codeNotFound, validRequest: true,
		},
		{
			contractCase: contractCase{name: "reservation conflict", operationID: "reserveSaleItem", method: http.MethodPost,
				path: "/v1/reservations", body: `{"sale_item_id":"` + routerTestItemID + `","quantity":1}`,
				token: "user-token", wantStatus: http.StatusConflict,
				setup: func(_ *testing.T, f *routerFixture) {
					f.reservationService.reserve = func(context.Context, flashsale.ReserveCommand, time.Time) (flashsale.ReserveResult, error) {
						return flashsale.ReserveResult{}, flashsale.ErrConflict
					}
				}},
			wantCode: codeConflict, validRequest: true,
		},
		{
			contractCase: contractCase{name: "internal error", operationID: "listActiveSales", method: http.MethodGet,
				path: "/v1/sales", wantStatus: http.StatusInternalServerError,
				setup: func(_ *testing.T, f *routerFixture) {
					f.publicSaleService.list = func(context.Context, int, int) ([]flashsale.Sale, error) {
						return nil, errors.New("test storage failure")
					}
				}},
			wantCode: codeInternalError, validRequest: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runContractErrorCase(t, contractRouter, test)
		})
	}
}

func runContractErrorCase(t *testing.T, contractRouter routers.Router, test contractErrorCase) {
	t.Helper()

	fixture := newRouterFixture(t)
	if test.setup != nil {
		test.setup(t, fixture)
	}
	input := contractInput(t, contractRouter, newContractRequest(t, test.contractCase))
	err := openapi3filter.ValidateRequest(t.Context(), input)
	if test.validRequest && err != nil {
		t.Fatalf("valid request violates OpenAPI contract: %v", err)
	}
	if !test.validRequest && err == nil {
		t.Fatal("malformed request unexpectedly passed OpenAPI validation")
	}

	recorder := httptest.NewRecorder()
	fixture.handler(t).ServeHTTP(recorder, newContractRequest(t, test.contractCase))
	assertContractResponse(t, input, recorder, test.wantStatus)

	var envelope errorEnvelope
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode error envelope: %v", err)
	}
	if envelope.Error.Code != test.wantCode {
		t.Errorf("error code = %q, want %q", envelope.Error.Code, test.wantCode)
	}
	if got, want := envelope.Error.RequestID, recorder.Header().Get("X-Request-ID"); got != want {
		t.Errorf("error request_id = %q, want %q", got, want)
	}
}

func TestContract_ValidatorRejectsUndocumentedResponseStatus(t *testing.T) {
	_, contractRouter := loadContract(t)
	test := contractCase{operationID: "listActiveSales", method: http.MethodGet, path: "/v1/sales"}
	input := contractInput(t, contractRouter, newContractRequest(t, test))
	if err := openapi3filter.ValidateResponse(t.Context(), &openapi3filter.ResponseValidationInput{
		RequestValidationInput: input,
		Status:                 http.StatusAccepted,
		Header:                 http.Header{},
		Body:                   io.NopCloser(bytes.NewReader(nil)),
		Options:                &openapi3filter.Options{IncludeResponseStatus: true},
	}); err == nil {
		t.Fatal("undocumented 202 response passed OpenAPI validation")
	}
}
