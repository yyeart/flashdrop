package identity

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

const loginTestPassword = "correct horse battery staple"

type loginTestContextKey struct{}

type loginTestRepository struct {
	credentials LoginCredentials
	found       bool
	err         error

	createUserCalls int
	findCalls       int
	email           string
	contextMark     string
}

func (r *loginTestRepository) CreateUser(context.Context, User, PasswordHash) error {
	r.createUserCalls++
	return nil
}

func (r *loginTestRepository) FindLoginCreds(
	ctx context.Context,
	email string,
) (LoginCredentials, bool, error) {
	r.findCalls++
	r.email = email
	if mark, ok := ctx.Value(loginTestContextKey{}).(string); ok {
		r.contextMark = mark
	}

	return r.credentials, r.found, r.err
}

func newLoginTestService(t *testing.T, repository userRepository) *Service {
	t.Helper()

	service, err := NewService(repository, validTestTokenConfig())
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	service.now = func() time.Time {
		return time.Date(2026, time.September, 12, 12, 0, 0, 0, time.UTC)
	}

	return service
}

func mustLoginTestPasswordHash(t *testing.T, password string) PasswordHash {
	t.Helper()

	hash, err := hashPassword(password)
	if err != nil {
		t.Fatalf("hashPassword() error = %v", err)
	}

	return hash
}

func TestService_Login_NormalizesEmailPropagatesContextAndIssuesToken(t *testing.T) {
	userID := uuid.MustParse("00000000-0000-0000-0000-000000000123")
	repository := &loginTestRepository{
		found: true,
		credentials: LoginCredentials{
			UserID:       userID,
			Role:         RoleUser,
			PasswordHash: mustLoginTestPasswordHash(t, loginTestPassword),
		},
	}
	service := newLoginTestService(t, repository)

	const contextMark = "login-request"
	ctx := context.WithValue(context.Background(), loginTestContextKey{}, contextMark)
	result, err := service.Login(ctx, LoginInput{
		Email:    "  USER@Example.COM  ",
		Password: loginTestPassword,
	})
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}

	if repository.findCalls != 1 || repository.email != "user@example.com" {
		t.Fatalf("FindLoginCreds() calls = %d, email = %q; want 1, %q", repository.findCalls, repository.email, "user@example.com")
	}
	if repository.contextMark != contextMark {
		t.Errorf("FindLoginCreds() context mark = %q, want %q", repository.contextMark, contextMark)
	}
	if repository.createUserCalls != 0 {
		t.Errorf("CreateUser() calls = %d, want 0", repository.createUserCalls)
	}
	if result.ExpiresIn != service.tokenConfig.TTL {
		t.Errorf("ExpiresIn = %v, want %v", result.ExpiresIn, service.tokenConfig.TTL)
	}

	authenticated, err := service.Authenticate(context.Background(), result.AccessToken)
	if err != nil {
		t.Fatalf("Authenticate(issued token) error = %v", err)
	}
	if authenticated.UserID != userID || authenticated.Role != RoleUser {
		t.Fatalf("Authenticate(issued token) = %#v, want UserID %s and role %q", authenticated, userID, RoleUser)
	}
}

func TestService_Login_InvalidCredentialsHaveCommonContract(t *testing.T) {
	validHash := mustLoginTestPasswordHash(t, loginTestPassword)
	tests := []struct {
		name       string
		repository *loginTestRepository
		password   string
	}{
		{
			name:       "unknown email",
			repository: &loginTestRepository{},
			password:   loginTestPassword,
		},
		{
			name: "wrong password",
			repository: &loginTestRepository{
				found: true,
				credentials: LoginCredentials{
					UserID:       uuid.MustParse("00000000-0000-0000-0000-000000000123"),
					Role:         RoleUser,
					PasswordHash: validHash,
				},
			},
			password: "wrong password",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := newLoginTestService(t, tt.repository)
			result, err := service.Login(context.Background(), LoginInput{
				Email:    "user@example.com",
				Password: tt.password,
			})
			if !errors.Is(err, ErrInvalidCredentials) {
				t.Fatalf("Login() error = %v, want ErrInvalidCredentials", err)
			}
			if result != (LoginResult{}) {
				t.Fatalf("Login() result = %#v, want zero value", result)
			}
		})
	}
}

func TestService_Login_UnknownEmailUsesDummyPasswordHash(t *testing.T) {
	repository := &loginTestRepository{}
	service := newLoginTestService(t, repository)
	service.dummyPasswordHash = PasswordHash{}

	_, err := service.Login(context.Background(), LoginInput{
		Email:    "missing@example.com",
		Password: loginTestPassword,
	})
	if !errors.Is(err, errUnsupportedHash) {
		t.Fatalf("Login() error = %v, want errUnsupportedHash from dummy verification", err)
	}
}

func TestService_Login_PreservesRepositoryAndContextErrors(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{name: "repository error", err: errors.New("repository failure")},
		{name: "context canceled", err: context.Canceled},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repository := &loginTestRepository{err: tt.err}
			service := newLoginTestService(t, repository)

			result, err := service.Login(context.Background(), LoginInput{
				Email:    "user@example.com",
				Password: loginTestPassword,
			})
			if !errors.Is(err, tt.err) {
				t.Fatalf("Login() error = %v, want wrapped %v", err, tt.err)
			}
			if result != (LoginResult{}) {
				t.Fatalf("Login() result = %#v, want zero value", result)
			}
		})
	}
}

func TestService_Login_RejectsInvalidStoredCredentials(t *testing.T) {
	validHash := mustLoginTestPasswordHash(t, loginTestPassword)
	tests := []struct {
		name        string
		credentials LoginCredentials
		wantError   error
	}{
		{
			name: "malformed password hash",
			credentials: LoginCredentials{
				UserID: uuid.MustParse("00000000-0000-0000-0000-000000000123"),
				Role:   RoleUser,
			},
			wantError: errUnsupportedHash,
		},
		{
			name: "nil user ID",
			credentials: LoginCredentials{
				Role:         RoleUser,
				PasswordHash: validHash,
			},
			wantError: ErrInvalidToken,
		},
		{
			name: "unknown role",
			credentials: LoginCredentials{
				UserID:       uuid.MustParse("00000000-0000-0000-0000-000000000123"),
				Role:         Role("root"),
				PasswordHash: validHash,
			},
			wantError: ErrInvalidToken,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := newLoginTestService(t, &loginTestRepository{
				found:       true,
				credentials: tt.credentials,
			})

			result, err := service.Login(context.Background(), LoginInput{
				Email:    "user@example.com",
				Password: loginTestPassword,
			})
			if !errors.Is(err, tt.wantError) {
				t.Fatalf("Login() error = %v, want %v", err, tt.wantError)
			}
			if result != (LoginResult{}) {
				t.Fatalf("Login() result = %#v, want zero value", result)
			}
		})
	}
}
