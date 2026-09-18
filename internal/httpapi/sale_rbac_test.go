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

type adminSaleRBACEndpoint struct {
	name          string
	handler       func(*AdminSaleHandler) http.Handler
	request       func(*testing.T) *http.Request
	successStatus int
	serviceCalls  func(*adminSaleServiceStub) int
}

type adminSaleRBACAccess struct {
	name             string
	token            string
	role             identity.Role
	wantStatus       int
	wantCode         errorCode
	wantServiceCalls int
	wantAuthenticate int
}

func TestAdminSaleHandlers_RequireAdminRole(t *testing.T) {
	t.Parallel()

	saleID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	for _, endpoint := range adminSaleRBACEndpoints(saleID) {
		for _, access := range adminSaleRBACAccessCases() {
			t.Run(endpoint.name+"/"+access.name, func(t *testing.T) {
				t.Parallel()

				testAdminSaleRBACAccess(t, endpoint, access)
			})
		}
	}
}

func adminSaleRBACEndpoints(saleID uuid.UUID) []adminSaleRBACEndpoint {
	return []adminSaleRBACEndpoint{
		{
			name: "create",
			handler: func(handler *AdminSaleHandler) http.Handler {
				return http.HandlerFunc(handler.Create)
			},
			request: func(t *testing.T) *http.Request {
				t.Helper()

				return adminCreateRequest(
					t,
					http.MethodPost,
					`{"starts_at":"2026-09-17T10:00:00Z","ends_at":"2026-09-17T11:00:00Z"}`,
				)
			},
			successStatus: http.StatusCreated,
			serviceCalls:  func(service *adminSaleServiceStub) int { return service.createCalls },
		},
		{
			name: "add item",
			handler: func(handler *AdminSaleHandler) http.Handler {
				return http.HandlerFunc(handler.AddItem)
			},
			request: func(t *testing.T) *http.Request {
				t.Helper()

				return adminAddItemRequest(
					t,
					http.MethodPost,
					saleID.String(),
					`{"product_id":"33333333-3333-3333-3333-333333333333","name":"Keyboard","price":"12.50","total_quantity":1}`,
				)
			},
			successStatus: http.StatusCreated,
			serviceCalls:  func(service *adminSaleServiceStub) int { return service.addItemCalls },
		},
		{
			name: "activate",
			handler: func(handler *AdminSaleHandler) http.Handler {
				return http.HandlerFunc(handler.Activate)
			},
			request: func(t *testing.T) *http.Request {
				t.Helper()

				return adminLifecycleRequest(
					t,
					http.MethodPost,
					"activate",
					saleID.String(),
				)
			},
			successStatus: http.StatusOK,
			serviceCalls:  func(service *adminSaleServiceStub) int { return service.activateCalls },
		},
		{
			name: "end",
			handler: func(handler *AdminSaleHandler) http.Handler {
				return http.HandlerFunc(handler.End)
			},
			request: func(t *testing.T) *http.Request {
				t.Helper()

				return adminLifecycleRequest(
					t,
					http.MethodPost,
					"end",
					saleID.String(),
				)
			},
			successStatus: http.StatusOK,
			serviceCalls:  func(service *adminSaleServiceStub) int { return service.endCalls },
		},
	}
}

func adminSaleRBACAccessCases() []adminSaleRBACAccess {
	return []adminSaleRBACAccess{
		{
			name:             "missing token",
			wantStatus:       http.StatusUnauthorized,
			wantCode:         codeUnauthorized,
			wantServiceCalls: 0,
			wantAuthenticate: 0,
		},
		{
			name:             "user role",
			token:            "user-token",
			role:             identity.RoleUser,
			wantStatus:       http.StatusForbidden,
			wantCode:         codeForbidden,
			wantServiceCalls: 0,
			wantAuthenticate: 1,
		},
		{
			name:             "admin role",
			token:            "admin-token",
			role:             identity.RoleAdmin,
			wantServiceCalls: 1,
			wantAuthenticate: 1,
		},
	}
}

func testAdminSaleRBACAccess(
	t *testing.T,
	endpoint adminSaleRBACEndpoint,
	access adminSaleRBACAccess,
) {
	t.Helper()

	service := newAdminRBACServiceStub(t)
	handler := newAdminSaleHandlerForTest(
		t,
		service,
		func() uuid.UUID {
			return uuid.MustParse("22222222-2222-2222-2222-222222222222")
		},
		func() time.Time {
			return time.Date(2026, time.September, 17, 9, 0, 0, 0, time.UTC)
		},
	)

	authenticateCalls := 0
	authentication := adminSaleAuthenticationMiddleware(t, access, &authenticateCalls)
	rbac := adminSaleRBACMiddleware(t)
	protected := Chain(
		endpoint.handler(handler),
		requestIDMiddleware(t),
		authentication,
		rbac,
	)
	request := endpoint.request(t)
	if access.token != "" {
		request.Header.Set("Authorization", "Bearer "+access.token)
	}

	recorder := httptest.NewRecorder()
	protected.ServeHTTP(recorder, request)

	wantStatus := access.wantStatus
	if access.role == identity.RoleAdmin {
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
	if got := endpoint.serviceCalls(service); got != access.wantServiceCalls {
		t.Errorf("service calls = %d, want %d", got, access.wantServiceCalls)
	}
	if authenticateCalls != access.wantAuthenticate {
		t.Errorf(
			"Authenticate() calls = %d, want %d",
			authenticateCalls,
			access.wantAuthenticate,
		)
	}
}

func adminSaleAuthenticationMiddleware(
	t *testing.T,
	access adminSaleRBACAccess,
	calls *int,
) Middleware {
	t.Helper()

	authentication, err := NewAuthentication(
		authenticatorFunc(func(_ context.Context, token string) (identity.AuthenticateResult, error) {
			(*calls)++
			if token != access.token {
				t.Errorf("Authenticate() token = %q, want %q", token, access.token)
			}

			return identity.AuthenticateResult{
				UserID: uuid.MustParse("44444444-4444-4444-4444-444444444444"),
				Role:   access.role,
			}, nil
		}),
		slog.New(slog.DiscardHandler),
	)
	if err != nil {
		t.Fatalf("NewAuthentication() error = %v, want nil", err)
	}

	return authentication
}

func adminSaleRBACMiddleware(t *testing.T) Middleware {
	t.Helper()

	rbac, err := NewRBAC(slog.New(slog.DiscardHandler), identity.RoleAdmin)
	if err != nil {
		t.Fatalf("NewRBAC() error = %v, want nil", err)
	}

	return rbac
}

func TestPublicSaleList_DoesNotRequirePrincipal(t *testing.T) {
	t.Parallel()

	service := &publicSaleServiceStub{
		list: func(context.Context, int, int) ([]flashsale.Sale, error) {
			return []flashsale.Sale{}, nil
		},
	}
	handler := newPublicSaleHandlerForTest(t, service)
	public := Chain(
		http.HandlerFunc(handler.List),
		requestIDMiddleware(t),
	)
	request := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		"/v1/sales",
		nil,
	)
	recorder := httptest.NewRecorder()

	public.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf(
			"status = %d, want %d; body = %s",
			recorder.Code,
			http.StatusOK,
			recorder.Body,
		)
	}
	if service.listCalls != 1 {
		t.Errorf("ListActiveSales() calls = %d, want 1", service.listCalls)
	}
}

func newAdminRBACServiceStub(t *testing.T) *adminSaleServiceStub {
	t.Helper()

	return &adminSaleServiceStub{
		create: func(context.Context, flashsale.Sale) error {
			return nil
		},
		addItem: func(
			context.Context,
			uuid.UUID,
			uuid.UUID,
			uuid.UUID,
			string,
			flashsale.Money,
			int,
		) error {
			return nil
		},
		activate: func(context.Context, uuid.UUID, time.Time) (flashsale.Sale, error) {
			return lifecycleSale(t, flashsale.ActiveState), nil
		},
		end: func(context.Context, uuid.UUID) (flashsale.Sale, error) {
			return lifecycleSale(t, flashsale.EndedState), nil
		},
	}
}
