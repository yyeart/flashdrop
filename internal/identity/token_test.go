package identity

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

func TestIssueAccessToken_ClaimsAndSignature(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}
	now := time.Date(2026, time.September, 12, 12, 0, 0, 0, time.UTC)
	userID := uuid.MustParse("00000000-0000-0000-0000-000000000123")
	cfg := validTestTokenConfig()
	cfg.PrivateKey = privateKey
	for _, role := range []Role{RoleUser, RoleAdmin} {
		t.Run(string(role), func(t *testing.T) {
			raw, err := issueAccessToken(userID, role, now, cfg)
			if err != nil {
				t.Fatalf("issueAccessToken() error = %v", err)
			}
			claims := &accessClaims{}
			token, err := jwt.ParseWithClaims(raw, claims, func(*jwt.Token) (any, error) {
				return publicKey, nil
			}, jwt.WithValidMethods([]string{"EdDSA"}), jwt.WithTimeFunc(func() time.Time { return now }))
			if err != nil {
				t.Fatalf("ParseWithClaims() error = %v", err)
			}
			if token == nil || !token.Valid {
				t.Fatal("parsed token is not valid")
			}
			checkAccessClaims(t, claims, userID, role, now, cfg)
		})
	}
}

func checkAccessClaims(t *testing.T, claims *accessClaims, userID uuid.UUID, role Role, now time.Time, cfg TokenConfig) {
	t.Helper()
	if claims.Subject != userID.String() {
		t.Errorf("sub = %q, want %q", claims.Subject, userID.String())
	}
	if claims.Role != role {
		t.Errorf("role = %q, want %q", claims.Role, role)
	}
	if claims.Issuer != cfg.Issuer {
		t.Errorf("iss = %q, want %q", claims.Issuer, cfg.Issuer)
	}
	if len(claims.Audience) != 1 || claims.Audience[0] != cfg.Audience {
		t.Errorf("aud = %v, want [%s]", claims.Audience, cfg.Audience)
	}
	if claims.IssuedAt == nil || !claims.IssuedAt.Equal(now) {
		t.Errorf("iat = %v, want %v", claims.IssuedAt, now)
	}
	if claims.ExpiresAt == nil || !claims.ExpiresAt.Equal(now.Add(cfg.TTL)) {
		t.Errorf("exp = %v, want %v", claims.ExpiresAt, now.Add(cfg.TTL))
	}
}

func TestIssueAccessToken_RejectsWrongPublicKey(t *testing.T) {
	cfg := validTestTokenConfig()
	otherPublicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}
	now := time.Date(2026, time.September, 12, 12, 0, 0, 0, time.UTC)
	raw, err := issueAccessToken(uuid.MustParse("00000000-0000-0000-0000-000000000123"), RoleUser, now, cfg)
	if err != nil {
		t.Fatalf("issueAccessToken() error = %v", err)
	}
	token, err := jwt.ParseWithClaims(raw, &accessClaims{}, func(*jwt.Token) (any, error) {
		return otherPublicKey, nil
	}, jwt.WithValidMethods([]string{"EdDSA"}), jwt.WithTimeFunc(func() time.Time { return now }))
	if !errors.Is(err, jwt.ErrTokenSignatureInvalid) {
		t.Fatalf("ParseWithClaims() error = %v, want ErrTokenSignatureInvalid", err)
	}
	if token != nil && token.Valid {
		t.Fatal("token verified with a different public key")
	}
}
