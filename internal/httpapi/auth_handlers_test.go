package httpapi

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/yyeart/flashdrop/internal/identity"
)

type authIdentityStub struct {
	register func(context.Context, identity.RegisterInput) (identity.User, error)
	login    func(context.Context, identity.LoginInput) (identity.LoginResult, error)

	registerCalls int
	loginCalls    int
}

type authRegisterRepository struct {
	user identity.User
}

func (r *authRegisterRepository) CreateUser(
	_ context.Context,
	user identity.User,
	_ identity.PasswordHash,
) error {
	r.user = user

	return nil
}

func (*authRegisterRepository) FindLoginCreds(
	context.Context,
	string,
) (identity.LoginCredentials, bool, error) {
	return identity.LoginCredentials{}, false, nil
}

func (s *authIdentityStub) Register(
	ctx context.Context,
	input identity.RegisterInput,
) (identity.User, error) {
	s.registerCalls++
	if s.register == nil {
		return identity.User{}, errors.New("unexpected Register call")
	}

	return s.register(ctx, input)
}

func (s *authIdentityStub) Login(
	ctx context.Context,
	input identity.LoginInput,
) (identity.LoginResult, error) {
	s.loginCalls++
	if s.login == nil {
		return identity.LoginResult{}, errors.New("unexpected Login call")
	}

	return s.login(ctx, input)
}

func newAuthHandlerForTest(t *testing.T, service identityService) *AuthHandler {
	t.Helper()

	handler, err := NewAuthHandler(service, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}

	return handler
}

func newIdentityServiceForAuthTest(
	t *testing.T,
	repository *authRegisterRepository,
) *identity.Service {
	t.Helper()

	privateKey := ed25519.NewKeyFromSeed(make([]byte, ed25519.SeedSize))
	publicKey, ok := privateKey.Public().(ed25519.PublicKey)
	if !ok {
		t.Fatal("ed25519 private key returned a non-ed25519 public key")
	}

	service, err := identity.NewService(repository, identity.TokenConfig{
		Issuer:     "flashdrop",
		Audience:   "flashdrop-api",
		TTL:        15 * time.Minute,
		PrivateKey: privateKey,
		PublicKey:  publicKey,
	})
	if err != nil {
		t.Fatalf("identity.NewService() error = %v", err)
	}

	return service
}

func authRequest(
	t *testing.T,
	method string,
	path string,
	body string,
) *http.Request {
	t.Helper()

	request := httptest.NewRequestWithContext(
		t.Context(), method, path, strings.NewReader(body),
	)
	request.Header.Set("Content-Type", "application/json")

	return request
}

func serveAuthHandler(
	t *testing.T,
	handler http.Handler,
	request *http.Request,
) *httptest.ResponseRecorder {
	t.Helper()

	recorder := httptest.NewRecorder()
	requestIDMiddleware(t)(handler).ServeHTTP(recorder, request)

	return recorder
}

func TestNewAuthHandler_RejectsNilDependencies(t *testing.T) {
	tests := []struct {
		name    string
		service identityService
		logger  *slog.Logger
		wantErr error
	}{
		{
			name:    "nil identity service",
			logger:  slog.New(slog.DiscardHandler),
			wantErr: ErrNilIdentityService,
		},
		{
			name:    "nil logger",
			service: &authIdentityStub{},
			wantErr: ErrNilLogger,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			handler, err := NewAuthHandler(test.service, test.logger)

			if handler != nil {
				t.Errorf("NewAuthHandler() handler = %#v, want nil", handler)
			}
			if !errors.Is(err, test.wantErr) {
				t.Errorf("NewAuthHandler() error = %v, want %v", err, test.wantErr)
			}
		})
	}
}

func TestAuthHandler_CanceledContextDoesNotCallIdentityOrWriteResponse(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		handler func(*AuthHandler, http.ResponseWriter, *http.Request)
	}{
		{
			name:    "register",
			path:    "/v1/auth/register",
			handler: (*AuthHandler).Register,
		},
		{
			name:    "login",
			path:    "/v1/auth/login",
			handler: (*AuthHandler).Login,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service := &authIdentityStub{}
			handler := newAuthHandlerForTest(t, service)
			request := authRequest(
				t,
				http.MethodPost,
				test.path,
				`{"email":"user@example.com","password":"valid-password"}`,
			)
			ctx, cancel := context.WithCancel(request.Context())
			cancel()
			request = request.WithContext(ctx)

			recorder := httptest.NewRecorder()
			state := ensureResponseState(recorder)
			test.handler(handler, state, request)

			if service.registerCalls != 0 || service.loginCalls != 0 {
				t.Errorf(
					"identity was called: Register = %d, Login = %d",
					service.registerCalls,
					service.loginCalls,
				)
			}
			if state.Written() {
				t.Errorf("handler wrote response status %d", state.Status())
			}
			if recorder.Body.Len() != 0 {
				t.Errorf("handler wrote response body of %d bytes", recorder.Body.Len())
			}
			if len(recorder.Header()) != 0 {
				t.Errorf("handler wrote response headers: %#v", recorder.Header())
			}
		})
	}
}

func TestAuthHandler_RegisterSuccess(t *testing.T) {
	wantInput := identity.RegisterInput{
		Email:    "User.Example@example.com",
		Password: "correct horse battery staple",
	}
	repository := &authRegisterRepository{}
	identityService := newIdentityServiceForAuthTest(t, repository)

	type contextKey struct{}
	marker := &struct{}{}
	request := authRequest(
		t,
		http.MethodPost,
		"/v1/auth/register",
		`{"email":"User.Example@example.com","password":"correct horse battery staple"}`,
	)
	request = request.WithContext(context.WithValue(request.Context(), contextKey{}, marker))

	service := &authIdentityStub{
		register: func(ctx context.Context, input identity.RegisterInput) (identity.User, error) {
			if ctx.Value(contextKey{}) != marker {
				t.Error("request context was not propagated to identity.Register")
			}
			if !reflect.DeepEqual(input, wantInput) {
				t.Errorf("Register input = %#v, want %#v", input, wantInput)
			}

			return identityService.Register(ctx, input)
		},
	}
	handler := newAuthHandlerForTest(t, service)
	recorder := serveAuthHandler(t, http.HandlerFunc(handler.Register), request)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body = %s", recorder.Code, http.StatusCreated, recorder.Body)
	}
	if got := recorder.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}
	if service.registerCalls != 1 || service.loginCalls != 0 {
		t.Errorf("identity calls: Register = %d, Login = %d", service.registerCalls, service.loginCalls)
	}
	if repository.user.Email() != "user.example@example.com" ||
		repository.user.Role() != identity.RoleUser {
		t.Fatalf("registered User = %q/%q", repository.user.Email(), repository.user.Role())
	}

	var response map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	wantResponse := map[string]any{
		"id":         repository.user.ID().String(),
		"email":      repository.user.Email(),
		"role":       string(repository.user.Role()),
		"created_at": repository.user.CreatedAt().Format(time.RFC3339Nano),
	}
	if !reflect.DeepEqual(response, wantResponse) {
		t.Errorf("response = %#v, want exact User schema %#v", response, wantResponse)
	}
}

func TestAuthHandler_LoginSuccess(t *testing.T) {
	wantInput := identity.LoginInput{
		Email:    "User.Example@example.com",
		Password: "correct horse battery staple",
	}
	wantToken := "header.payload.signature"
	wantTTL := 17 * time.Minute

	type contextKey struct{}
	marker := &struct{}{}
	request := authRequest(
		t,
		http.MethodPost,
		"/v1/auth/login",
		`{"email":"User.Example@example.com","password":"correct horse battery staple"}`,
	)
	request = request.WithContext(context.WithValue(request.Context(), contextKey{}, marker))

	service := &authIdentityStub{
		login: func(ctx context.Context, input identity.LoginInput) (identity.LoginResult, error) {
			if ctx.Value(contextKey{}) != marker {
				t.Error("request context was not propagated to identity.Login")
			}
			if !reflect.DeepEqual(input, wantInput) {
				t.Errorf("Login input = %#v, want %#v", input, wantInput)
			}

			return identity.LoginResult{
				AccessToken: wantToken,
				ExpiresIn:   wantTTL,
			}, nil
		},
	}
	handler := newAuthHandlerForTest(t, service)
	recorder := serveAuthHandler(t, http.HandlerFunc(handler.Login), request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", recorder.Code, http.StatusOK, recorder.Body)
	}
	if got := recorder.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}
	if service.loginCalls != 1 || service.registerCalls != 0 {
		t.Errorf("identity calls: Register = %d, Login = %d", service.registerCalls, service.loginCalls)
	}

	var response map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	wantResponse := map[string]any{
		"access_token": wantToken,
		"token_type":   "Bearer",
		"expires_in":   wantTTL.Seconds(),
	}
	if !reflect.DeepEqual(response, wantResponse) {
		t.Errorf("response = %#v, want exact LoginResponse schema %#v", response, wantResponse)
	}
}

func TestAuthHandler_RejectsWrongMethod(t *testing.T) {
	tests := []struct {
		name    string
		handler func(*AuthHandler, http.ResponseWriter, *http.Request)
		path    string
	}{
		{"register", (*AuthHandler).Register, "/v1/auth/register"},
		{"login", (*AuthHandler).Login, "/v1/auth/login"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service := &authIdentityStub{}
			auth := newAuthHandlerForTest(t, service)
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				test.handler(auth, w, r)
			})
			recorder := serveAuthHandler(
				t, handler, authRequest(t, http.MethodGet, test.path, ""),
			)

			assertErrorResponse(t, recorder, http.StatusMethodNotAllowed, "method_not_allowed")
			if got := recorder.Header().Get("Allow"); got != http.MethodPost {
				t.Errorf("Allow = %q, want POST", got)
			}
			if service.registerCalls != 0 || service.loginCalls != 0 {
				t.Errorf("identity was called: Register = %d, Login = %d", service.registerCalls, service.loginCalls)
			}
		})
	}
}

func TestAuthHandler_RejectsInvalidJSONWithoutCallingIdentity(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{"empty", ""},
		{"malformed", `{"email":`},
		{"oversized", `{"email":"` + strings.Repeat("x", int(maxJSONBodyBytes)) + `","password":"password"}`},
		{"unknown field", `{"email":"user@example.com","password":"password","admin":true}`},
		{"second JSON value", `{"email":"user@example.com","password":"password"} {}`},
	}
	endpoints := []struct {
		name    string
		path    string
		handler func(*AuthHandler, http.ResponseWriter, *http.Request)
	}{
		{"register", "/v1/auth/register", (*AuthHandler).Register},
		{"login", "/v1/auth/login", (*AuthHandler).Login},
	}

	for _, endpoint := range endpoints {
		for _, test := range tests {
			t.Run(endpoint.name+"/"+test.name, func(t *testing.T) {
				service := &authIdentityStub{}
				auth := newAuthHandlerForTest(t, service)
				handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					endpoint.handler(auth, w, r)
				})
				recorder := serveAuthHandler(
					t,
					handler,
					authRequest(t, http.MethodPost, endpoint.path, test.body),
				)

				assertErrorResponse(t, recorder, http.StatusBadRequest, "bad_request")
				if service.registerCalls != 0 || service.loginCalls != 0 {
					t.Errorf("identity was called: Register = %d, Login = %d", service.registerCalls, service.loginCalls)
				}
			})
		}
	}
}

func TestAuthHandler_RegisterErrors(t *testing.T) {
	tests := []authHandlerErrorCase{
		{"invalid email", "not-an-email", "valid-password", identity.ErrInvalidEmail, http.StatusBadRequest, "bad_request"},
		{"short password", "user@example.com", "short", identity.ErrInvalidPasswordLength, http.StatusBadRequest, "bad_request"},
		{"long password", "user@example.com", strings.Repeat("x", 129), identity.ErrInvalidPasswordLength, http.StatusBadRequest, "bad_request"},
		{"duplicate email", "user@example.com", "valid-password", identity.ErrEmailAlreadyExists, http.StatusConflict, "conflict"},
		{"internal error", "user@example.com", "valid-password", errors.New("private register failure"), http.StatusInternalServerError, "internal_error"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			testRegisterError(t, test)
		})
	}
}

type authHandlerErrorCase struct {
	name       string
	email      string
	password   string
	serviceErr error
	wantStatus int
	wantCode   string
}

func testRegisterError(t *testing.T, test authHandlerErrorCase) {
	t.Helper()

	const privateDetail = "private identity register detail"
	service := &authIdentityStub{
		register: func(_ context.Context, input identity.RegisterInput) (identity.User, error) {
			if input.Email != test.email || input.Password != test.password {
				t.Errorf("Register input = %#v", input)
			}

			return identity.User{}, fmt.Errorf("%s: %w", privateDetail, test.serviceErr)
		},
	}
	auth := newAuthHandlerForTest(t, service)
	body := fmt.Sprintf(`{"email":%q,"password":%q}`, test.email, test.password)
	recorder := serveAuthHandler(
		t,
		http.HandlerFunc(auth.Register),
		authRequest(t, http.MethodPost, "/v1/auth/register", body),
	)

	assertErrorResponse(t, recorder, test.wantStatus, test.wantCode)
	if service.registerCalls != 1 || service.loginCalls != 0 {
		t.Errorf("identity calls: Register = %d, Login = %d", service.registerCalls, service.loginCalls)
	}
	if strings.Contains(recorder.Body.String(), privateDetail) {
		t.Errorf("internal error leaked to client: %s", recorder.Body)
	}
}

func TestAuthHandler_LoginErrors(t *testing.T) {
	tests := []authHandlerErrorCase{
		{"invalid credentials", "user@example.com", "wrong-password", identity.ErrInvalidCredentials, http.StatusUnauthorized, "unauthorized"},
		{"malformed email", "not-an-email", "valid-password", identity.ErrInvalidCredentials, http.StatusUnauthorized, "unauthorized"},
		{"short password", "user@example.com", "short", identity.ErrInvalidPasswordLength, http.StatusBadRequest, "bad_request"},
		{"long password", "user@example.com", strings.Repeat("x", 129), identity.ErrInvalidPasswordLength, http.StatusBadRequest, "bad_request"},
		{"internal error", "user@example.com", "valid-password", errors.New("private login failure"), http.StatusInternalServerError, "internal_error"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			testLoginError(t, test)
		})
	}
}

type authHandlerLogCase struct {
	name    string
	path    string
	service *authIdentityStub
	handler func(*AuthHandler, http.ResponseWriter, *http.Request)
}

func TestAuthHandler_InternalErrorLogIsCorrelatedAndDoesNotLeakSecrets(t *testing.T) {
	const (
		secretEmail    = "secret-email@example.com"
		secretPassword = "secret-password-marker"
		secretToken    = "secret-token-marker"
		privateDetail  = "private identity failure"
	)

	serviceError := errors.New(
		privateDetail + ": " + secretEmail + ": " + secretPassword + ": " + secretToken,
	)
	tests := []authHandlerLogCase{
		{
			name: "register",
			path: "/v1/auth/register",
			service: &authIdentityStub{
				register: func(context.Context, identity.RegisterInput) (identity.User, error) {
					return identity.User{}, serviceError
				},
			},
			handler: (*AuthHandler).Register,
		},
		{
			name: "login",
			path: "/v1/auth/login",
			service: &authIdentityStub{
				login: func(context.Context, identity.LoginInput) (identity.LoginResult, error) {
					return identity.LoginResult{}, serviceError
				},
			},
			handler: (*AuthHandler).Login,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			testAuthHandlerInternalErrorLog(t, test, []string{
				privateDetail,
				secretEmail,
				secretPassword,
				secretToken,
			})
		})
	}
}

func testAuthHandlerInternalErrorLog(
	t *testing.T,
	test authHandlerLogCase,
	forbiddenValues []string,
) {
	t.Helper()

	logger, output := testLogger()
	handler, err := NewAuthHandler(test.service, logger)
	if err != nil {
		t.Fatal(err)
	}
	request := authRequest(
		t,
		http.MethodPost,
		test.path,
		`{"email":"user@example.com","password":"valid-password"}`,
	)
	recorder := serveAuthHandler(
		t,
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			test.handler(handler, w, r)
		}),
		request,
	)

	assertErrorResponse(t, recorder, http.StatusInternalServerError, "internal_error")

	var record map[string]any
	if err := json.Unmarshal(output.Bytes(), &record); err != nil {
		t.Fatalf("decode error log: %v", err)
	}
	if record["request_id"] != foundationRequestID {
		t.Errorf("logged request_id = %v, want %q", record["request_id"], foundationRequestID)
	}
	if record["method"] != http.MethodPost {
		t.Errorf("logged method = %v, want POST", record["method"])
	}
	if record["path"] != test.path {
		t.Errorf("logged path = %v, want %q", record["path"], test.path)
	}

	for _, forbidden := range forbiddenValues {
		if strings.Contains(output.String(), forbidden) {
			t.Errorf("error log contains forbidden private marker %q", forbidden)
		}
	}
}

func testLoginError(t *testing.T, test authHandlerErrorCase) {
	t.Helper()

	const privateDetail = "private identity login detail"
	service := &authIdentityStub{
		login: func(_ context.Context, input identity.LoginInput) (identity.LoginResult, error) {
			if input.Email != test.email || input.Password != test.password {
				t.Errorf("Login input = %#v", input)
			}

			return identity.LoginResult{}, fmt.Errorf("%s: %w", privateDetail, test.serviceErr)
		},
	}
	auth := newAuthHandlerForTest(t, service)
	body := fmt.Sprintf(`{"email":%q,"password":%q}`, test.email, test.password)
	recorder := serveAuthHandler(
		t,
		http.HandlerFunc(auth.Login),
		authRequest(t, http.MethodPost, "/v1/auth/login", body),
	)

	assertErrorResponse(t, recorder, test.wantStatus, test.wantCode)
	if service.loginCalls != 1 || service.registerCalls != 0 {
		t.Errorf("identity calls: Register = %d, Login = %d", service.registerCalls, service.loginCalls)
	}
	if strings.Contains(recorder.Body.String(), privateDetail) {
		t.Errorf("internal error leaked to client: %s", recorder.Body)
	}
}
