package identity

import (
	"context"
	"crypto/ed25519"
	"errors"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

var authenticateTestNow = time.Date(2026, time.September, 12, 12, 0, 0, 0, time.UTC)

type panicUserRepository struct{}

func (panicUserRepository) CreateUser(context.Context, User, PasswordHash) error {
	panic("Authenticate must not call CreateUser")
}

func (panicUserRepository) FindLoginCreds(context.Context, string) (LoginCredentials, bool, error) {
	panic("Authenticate must not call FindLoginCreds")
}

func newAuthenticateTestService(t *testing.T) *Service {
	t.Helper()

	service, err := NewService(panicUserRepository{}, validTestTokenConfig())
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	service.now = func() time.Time { return authenticateTestNow }

	return service
}

func validAuthenticateTestClaims() accessClaims {
	return accessClaims{
		Role: RoleUser,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "00000000-0000-0000-0000-000000000123",
			Issuer:    "flashdrop",
			Audience:  jwt.ClaimStrings{"flashdrop-api"},
			IssuedAt:  jwt.NewNumericDate(authenticateTestNow.Add(-time.Minute)),
			ExpiresAt: jwt.NewNumericDate(authenticateTestNow.Add(time.Minute)),
		},
	}
}

func signAuthenticateTestToken(t *testing.T, method jwt.SigningMethod, key any, claims accessClaims) string {
	t.Helper()

	raw, err := jwt.NewWithClaims(method, claims).SignedString(key)
	if err != nil {
		t.Fatalf("SignedString() error = %v", err)
	}

	return raw
}

func TestService_Authenticate_RoundTrip(t *testing.T) {
	service := newAuthenticateTestService(t)
	userID := uuid.MustParse("00000000-0000-0000-0000-000000000123")

	for _, role := range []Role{RoleUser, RoleAdmin} {
		t.Run(string(role), func(t *testing.T) {
			raw, err := issueAccessToken(userID, role, authenticateTestNow, service.tokenConfig)
			if err != nil {
				t.Fatalf("issueAccessToken() error = %v", err)
			}

			got, err := service.Authenticate(context.Background(), raw)
			if err != nil {
				t.Fatalf("Authenticate() error = %v", err)
			}
			if got.UserID != userID || got.Role != role {
				t.Fatalf("Authenticate() = %#v, want UserID %s and role %q", got, userID, role)
			}
		})
	}
}

func TestService_Authenticate_RejectsInvalidClaims(t *testing.T) {
	service := newAuthenticateTestService(t)

	tests := []struct {
		name   string
		change func(*accessClaims)
	}{
		{name: "missing subject", change: func(c *accessClaims) { c.Subject = "" }},
		{name: "malformed subject", change: func(c *accessClaims) { c.Subject = "not-a-uuid" }},
		{name: "nil subject UUID", change: func(c *accessClaims) { c.Subject = uuid.Nil.String() }},
		{name: "missing role", change: func(c *accessClaims) { c.Role = "" }},
		{name: "unknown role", change: func(c *accessClaims) { c.Role = Role("root") }},
		{name: "missing issuer", change: func(c *accessClaims) { c.Issuer = "" }},
		{name: "wrong issuer", change: func(c *accessClaims) { c.Issuer = "other" }},
		{name: "missing audience", change: func(c *accessClaims) { c.Audience = nil }},
		{name: "wrong audience", change: func(c *accessClaims) { c.Audience = jwt.ClaimStrings{"other"} }},
		{name: "missing issued at", change: func(c *accessClaims) { c.IssuedAt = nil }},
		{name: "issued in future", change: func(c *accessClaims) { c.IssuedAt = jwt.NewNumericDate(authenticateTestNow.Add(time.Second)) }},
		{name: "missing expiration", change: func(c *accessClaims) { c.ExpiresAt = nil }},
		{name: "expires now", change: func(c *accessClaims) { c.ExpiresAt = jwt.NewNumericDate(authenticateTestNow) }},
		{name: "expired", change: func(c *accessClaims) { c.ExpiresAt = jwt.NewNumericDate(authenticateTestNow.Add(-time.Second)) }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			claims := validAuthenticateTestClaims()
			tt.change(&claims)
			raw := signAuthenticateTestToken(t, jwt.SigningMethodEdDSA, service.tokenConfig.PrivateKey, claims)

			_, err := service.Authenticate(context.Background(), raw)
			if !errors.Is(err, ErrInvalidToken) {
				t.Fatalf("Authenticate() error = %v, want ErrInvalidToken", err)
			}
		})
	}
}

func TestService_Authenticate_RejectsMalformedWrongAlgorithmAndSignature(t *testing.T) {
	service := newAuthenticateTestService(t)
	claims := validAuthenticateTestClaims()
	otherPrivateKey := ed25519.NewKeyFromSeed(make([]byte, ed25519.SeedSize))
	otherPrivateKey[0] ^= 0xff

	tests := []struct {
		name string
		raw  func(*testing.T) string
	}{
		{name: "empty", raw: func(*testing.T) string { return "" }},
		{name: "malformed", raw: func(*testing.T) string { return "not-a-jwt" }},
		{name: "wrong algorithm", raw: func(t *testing.T) string {
			return signAuthenticateTestToken(t, jwt.SigningMethodHS256, []byte("test-secret"), claims)
		}},
		{name: "wrong signature", raw: func(t *testing.T) string {
			return signAuthenticateTestToken(t, jwt.SigningMethodEdDSA, otherPrivateKey, claims)
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := service.Authenticate(context.Background(), tt.raw(t))
			if !errors.Is(err, ErrInvalidToken) {
				t.Fatalf("Authenticate() error = %v, want ErrInvalidToken", err)
			}
		})
	}
}

func TestValidateClaims_NilExpirationReturnsError(t *testing.T) {
	claims := validAuthenticateTestClaims()
	claims.ExpiresAt = nil

	if err := validateClaims(&claims); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("validateClaims() error = %v, want ErrInvalidToken", err)
	}
}
