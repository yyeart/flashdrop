package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/yyeart/flashdrop/internal/identity"
)

const (
	routerTestSaleID        = "11111111-1111-1111-1111-111111111111"
	routerTestReservationID = "22222222-2222-2222-2222-222222222222"
	routerTestOrderID       = "33333333-3333-3333-3333-333333333333"
	routerTestItemID        = "44444444-4444-4444-4444-444444444444"
	routerTestUserID        = "55555555-5555-5555-5555-555555555555"
)

type routerFixture struct {
	deps RouterDeps

	authService        *authIdentityStub
	publicSaleService  *publicSaleServiceStub
	adminSaleService   *adminSaleServiceStub
	reservationService *reservationServiceStub
	orderService       *orderHandlerServiceStub
	authenticateCalls  int
}

func newRouterFixture(t *testing.T) *routerFixture {
	t.Helper()

	fixture := &routerFixture{
		authService:        &authIdentityStub{},
		publicSaleService:  &publicSaleServiceStub{},
		adminSaleService:   &adminSaleServiceStub{},
		reservationService: &reservationServiceStub{},
		orderService:       &orderHandlerServiceStub{},
	}

	now := func() time.Time {
		return time.Date(2026, time.September, 20, 12, 0, 0, 0, time.UTC)
	}
	newID := func() uuid.UUID { return uuid.MustParse(routerTestReservationID) }
	fixture.deps = RouterDeps{
		Auth:               newAuthHandlerForTest(t, fixture.authService),
		PublicSales:        newPublicSaleHandlerForTest(t, fixture.publicSaleService),
		AdminSales:         newAdminSaleHandlerForTest(t, fixture.adminSaleService, newID, now),
		Reservations:       newReservationHandlerForTest(t, fixture.reservationService, newID, now, time.Minute),
		Orders:             newOrderHandlerForTest(t, fixture.orderService),
		RateLimiter:        AllowAllRateLimiter{},
		RequestIDGenerator: func() uuid.UUID { return uuid.MustParse(foundationRequestID) },
		Logger:             slog.New(slog.DiscardHandler),
		RequestTimeout:     time.Second,
	}
	fixture.deps.Authenticator = authenticatorFunc(func(_ context.Context, token string) (identity.AuthenticateResult, error) {
		fixture.authenticateCalls++

		var role identity.Role
		switch token {
		case "user-token":
			role = identity.RoleUser
		case "admin-token":
			role = identity.RoleAdmin
		default:
			return identity.AuthenticateResult{}, identity.ErrInvalidToken
		}

		return identity.AuthenticateResult{
			UserID: uuid.MustParse(routerTestUserID),
			Role:   role,
		}, nil
	})

	return fixture
}

func (f *routerFixture) handler(t *testing.T) http.Handler {
	t.Helper()

	handler, err := NewRouter(f.deps)
	if err != nil {
		t.Fatalf("NewRouter() error = %v", err)
	}

	return handler
}

func (f *routerFixture) serve(
	t *testing.T, handler http.Handler, method, path, body, token string,
) *httptest.ResponseRecorder {
	t.Helper()

	request := httptest.NewRequestWithContext(
		t.Context(), method, path, strings.NewReader(body),
	)
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	if method == http.MethodPost && path == "/v1/reservations" {
		request.Header.Set("Idempotency-Key", "router-test-key")
	}

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	return response
}

func (f *routerFixture) serviceCalls() map[string]int {
	return map[string]int{
		"register":  f.authService.registerCalls,
		"login":     f.authService.loginCalls,
		"list":      f.publicSaleService.listCalls,
		"get sale":  f.publicSaleService.findCalls,
		"reserve":   f.reservationService.reserveCalls,
		"cancel":    f.reservationService.cancelCalls,
		"pay":       f.reservationService.payCalls,
		"get order": f.orderService.findCalls,
		"create":    f.adminSaleService.createCalls,
		"add item":  f.adminSaleService.addItemCalls,
		"activate":  f.adminSaleService.activateCalls,
		"end":       f.adminSaleService.endCalls,
	}
}

func assertOnlyRouterServiceCalled(t *testing.T, fixture *routerFixture, want string) {
	t.Helper()

	for name, got := range fixture.serviceCalls() {
		wantCalls := 0
		if name == want {
			wantCalls = 1
		}
		if got != wantCalls {
			t.Errorf("%s calls = %d, want %d", name, got, wantCalls)
		}
	}
}

func TestRouter_DispatchesEveryOperation(t *testing.T) {
	const credentials = `{"email":"user@example.com","password":"password123"}`
	const createSale = `{"starts_at":"2026-09-21T10:00:00Z","ends_at":"2026-09-22T10:00:00Z"}`
	const addItem = `{"product_id":"66666666-6666-6666-6666-666666666666","name":"Keyboard","price":"12.50","total_quantity":1}`
	const reserve = `{"sale_item_id":"` + routerTestItemID + `","quantity":1}`

	tests := []struct {
		name, method, path, body, token, wantCall string
	}{
		{"register", http.MethodPost, "/v1/auth/register", credentials, "", "register"},
		{"login", http.MethodPost, "/v1/auth/login", credentials, "", "login"},
		{"list", http.MethodGet, "/v1/sales", "", "", "list"},
		{"get sale", http.MethodGet, "/v1/sales/" + routerTestSaleID, "", "", "get sale"},
		{"reserve", http.MethodPost, "/v1/reservations", reserve, "user-token", "reserve"},
		{"cancel", http.MethodPost, "/v1/reservations/" + routerTestReservationID + "/cancel", "", "user-token", "cancel"},
		{"pay", http.MethodPost, "/v1/reservations/" + routerTestReservationID + "/pay", "", "user-token", "pay"},
		{"get order", http.MethodGet, "/v1/orders/" + routerTestOrderID, "", "user-token", "get order"},
		{"create", http.MethodPost, "/v1/admin/sales", createSale, "admin-token", "create"},
		{"add item", http.MethodPost, "/v1/admin/sales/" + routerTestSaleID + "/items", addItem, "admin-token", "add item"},
		{"activate", http.MethodPost, "/v1/admin/sales/" + routerTestSaleID + "/activate", "", "admin-token", "activate"},
		{"end", http.MethodPost, "/v1/admin/sales/" + routerTestSaleID + "/end", "", "admin-token", "end"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newRouterFixture(t)
			response := fixture.serve(t, fixture.handler(t), test.method, test.path, test.body, test.token)
			assertErrorResponse(t, response, http.StatusInternalServerError, string(codeInternalError))
			assertOnlyRouterServiceCalled(t, fixture, test.wantCall)

			wantAuthCalls := 0
			if test.token != "" {
				wantAuthCalls = 1
			}
			if fixture.authenticateCalls != wantAuthCalls {
				t.Errorf("Authenticate calls = %d, want %d", fixture.authenticateCalls, wantAuthCalls)
			}

			assertRouterPathID(t, fixture, test.wantCall)
		})
	}
}

func assertRouterPathID(t *testing.T, fixture *routerFixture, operation string) {
	t.Helper()

	var got, want uuid.UUID
	switch operation {
	case "get sale":
		got, want = fixture.publicSaleService.gotSaleID, uuid.MustParse(routerTestSaleID)
	case "add item", "activate", "end":
		got, want = fixture.adminSaleService.gotSaleID, uuid.MustParse(routerTestSaleID)
	case "cancel":
		got, want = fixture.reservationService.gotCancelReservationID, uuid.MustParse(routerTestReservationID)
	case "pay":
		got, want = fixture.reservationService.gotPayReservationID, uuid.MustParse(routerTestReservationID)
	case "get order":
		got, want = fixture.orderService.gotOrderID, uuid.MustParse(routerTestOrderID)
	default:
		return
	}

	if got != want {
		t.Errorf("path ID passed to %s = %s, want %s", operation, got, want)
	}
}

func TestRouter_ProtectsEveryPrivateRoute(t *testing.T) {
	tests := []struct {
		name, method, path string
		adminOnly          bool
	}{
		{"reserve", http.MethodPost, "/v1/reservations", false},
		{"cancel", http.MethodPost, "/v1/reservations/" + routerTestReservationID + "/cancel", false},
		{"pay", http.MethodPost, "/v1/reservations/" + routerTestReservationID + "/pay", false},
		{"get order", http.MethodGet, "/v1/orders/" + routerTestOrderID, false},
		{"create", http.MethodPost, "/v1/admin/sales", true},
		{"add item", http.MethodPost, "/v1/admin/sales/" + routerTestSaleID + "/items", true},
		{"activate", http.MethodPost, "/v1/admin/sales/" + routerTestSaleID + "/activate", true},
		{"end", http.MethodPost, "/v1/admin/sales/" + routerTestSaleID + "/end", true},
	}

	for _, test := range tests {
		t.Run(test.name+"/missing token", func(t *testing.T) {
			fixture := newRouterFixture(t)
			response := fixture.serve(t, fixture.handler(t), test.method, test.path, "", "")
			assertErrorResponse(t, response, http.StatusUnauthorized, string(codeUnauthorized))
			if fixture.authenticateCalls != 0 {
				t.Errorf("Authenticate calls = %d, want 0", fixture.authenticateCalls)
			}
			assertNoRouterServiceCalls(t, fixture)
		})

		if test.adminOnly {
			t.Run(test.name+"/user token", func(t *testing.T) {
				fixture := newRouterFixture(t)
				response := fixture.serve(t, fixture.handler(t), test.method, test.path, "", "user-token")
				assertErrorResponse(t, response, http.StatusForbidden, string(codeForbidden))
				if fixture.authenticateCalls != 1 {
					t.Errorf("Authenticate calls = %d, want 1", fixture.authenticateCalls)
				}
				assertNoRouterServiceCalls(t, fixture)
			})
		}
	}
}

func assertNoRouterServiceCalls(t *testing.T, fixture *routerFixture) {
	t.Helper()
	for name, calls := range fixture.serviceCalls() {
		if calls != 0 {
			t.Errorf("%s calls = %d, want 0", name, calls)
		}
	}
}

func TestRouter_AdminMayUseUserRoute(t *testing.T) {
	fixture := newRouterFixture(t)
	response := fixture.serve(
		t, fixture.handler(t), http.MethodGet,
		"/v1/orders/"+routerTestOrderID, "", "admin-token",
	)

	assertErrorResponse(t, response, http.StatusInternalServerError, string(codeInternalError))
	assertOnlyRouterServiceCalled(t, fixture, "get order")
	if fixture.authenticateCalls != 1 {
		t.Errorf("Authenticate calls = %d, want 1", fixture.authenticateCalls)
	}
}

func TestRouter_NotFoundAndMethodNotAllowedUseJSONEnvelope(t *testing.T) {
	tests := []struct {
		name, method, path, token string
		wantStatus                int
		wantCode                  errorCode
		wantAllow                 string
	}{
		{"unknown path", http.MethodGet, "/v1/unknown", "", http.StatusNotFound, codeNotFound, ""},
		{"public wrong method", http.MethodPost, "/v1/sales", "", http.StatusMethodNotAllowed, codeMethodNotAllowed, http.MethodGet},
		{"user wrong method", http.MethodGet, "/v1/reservations", "user-token", http.StatusMethodNotAllowed, codeMethodNotAllowed, http.MethodPost},
		{"admin wrong method", http.MethodGet, "/v1/admin/sales", "admin-token", http.StatusMethodNotAllowed, codeMethodNotAllowed, http.MethodPost},
		{"unauthenticated wrong method", http.MethodGet, "/v1/reservations", "", http.StatusUnauthorized, codeUnauthorized, ""},
		{"forbidden wrong method", http.MethodGet, "/v1/admin/sales", "user-token", http.StatusForbidden, codeForbidden, ""},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newRouterFixture(t)
			response := fixture.serve(t, fixture.handler(t), test.method, test.path, "", test.token)
			assertErrorResponse(t, response, test.wantStatus, string(test.wantCode))
			if got := response.Header().Get("Allow"); got != test.wantAllow {
				t.Errorf("Allow = %q, want %q", got, test.wantAllow)
			}
			assertNoRouterServiceCalls(t, fixture)
		})
	}
}

func TestRouter_RateLimitRunsBeforeAuthentication(t *testing.T) {
	fixture := newRouterFixture(t)
	limiterCalls := 0
	fixture.deps.RateLimiter = limiterFunc(func(
		_ context.Context, method, path, _ string,
	) (bool, time.Duration, error) {
		limiterCalls++
		if method != http.MethodPost || path != "/v1/admin/sales" {
			t.Errorf("limiter got %s %s", method, path)
		}
		return false, 2 * time.Second, nil
	})

	response := fixture.serve(t, fixture.handler(t), http.MethodPost, "/v1/admin/sales", "", "admin-token")
	assertErrorResponse(t, response, http.StatusTooManyRequests, string(codeRateLimitExceeded))
	if got := response.Header().Get("Retry-After"); got != "2" {
		t.Errorf("Retry-After = %q, want 2", got)
	}
	if limiterCalls != 1 || fixture.authenticateCalls != 0 {
		t.Errorf("limiter calls = %d, auth calls = %d; want 1, 0", limiterCalls, fixture.authenticateCalls)
	}
	assertNoRouterServiceCalls(t, fixture)
}

func TestNewRouter_RejectsMissingDependencies(t *testing.T) {
	tests := []struct {
		name    string
		change  func(*RouterDeps)
		wantErr error
	}{
		{"auth handler", func(d *RouterDeps) { d.Auth = nil }, ErrNilHandler},
		{"public sales handler", func(d *RouterDeps) { d.PublicSales = nil }, ErrNilHandler},
		{"admin sales handler", func(d *RouterDeps) { d.AdminSales = nil }, ErrNilHandler},
		{"reservations handler", func(d *RouterDeps) { d.Reservations = nil }, ErrNilHandler},
		{"orders handler", func(d *RouterDeps) { d.Orders = nil }, ErrNilHandler},
		{"authenticator", func(d *RouterDeps) { d.Authenticator = nil }, ErrNilAuthenticator},
		{"rate limiter", func(d *RouterDeps) { d.RateLimiter = nil }, ErrNilRateLimiter},
		{"request ID generator", func(d *RouterDeps) { d.RequestIDGenerator = nil }, ErrNilRequestIDGenerator},
		{"logger", func(d *RouterDeps) { d.Logger = nil }, ErrNilLogger},
		{"zero timeout", func(d *RouterDeps) { d.RequestTimeout = 0 }, ErrInvalidTimeout},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newRouterFixture(t)
			test.change(&fixture.deps)
			handler, err := NewRouter(fixture.deps)
			if !errors.Is(err, test.wantErr) || handler != nil {
				t.Errorf("NewRouter() = (%v, %v), want (nil, %v)", handler, err, test.wantErr)
			}
		})
	}
}
