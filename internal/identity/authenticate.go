package identity

import (
	"context"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

type AuthenticateResult struct {
	UserID uuid.UUID
	Role   Role
}

func (s *Service) Authenticate(
	ctx context.Context,
	tokenString string,
) (AuthenticateResult, error) {
	claims := &accessClaims{}

	keyFunc := func(token *jwt.Token) (any, error) {
		method, ok := token.Method.(*jwt.SigningMethodEd25519)
		if !ok {
			return nil, fmt.Errorf(
				"unexpected signing method type: %T: %w",
				token.Method,
				ErrInvalidToken,
			)
		}

		if method.Alg() != "EdDSA" {
			return nil, fmt.Errorf(
				"unexpected alg header value: %s: %w",
				method.Alg(), ErrInvalidToken,
			)
		}

		return s.tokenConfig.PublicKey, nil
	}

	token, err := jwt.ParseWithClaims(
		tokenString,
		claims,
		keyFunc,
		jwt.WithValidMethods([]string{
			jwt.SigningMethodEdDSA.Alg(),
		}),
		jwt.WithIssuer(s.tokenConfig.Issuer),
		jwt.WithAudience(s.tokenConfig.Audience),
		jwt.WithExpirationRequired(),
		jwt.WithIssuedAt(),
		jwt.WithTimeFunc(func() time.Time {
			return s.now().UTC()
		}),
	)
	if err != nil {
		return AuthenticateResult{}, fmt.Errorf(
			"parse token: %w: %w",
			err,
			ErrInvalidToken,
		)
	}

	if err := validateToken(token); err != nil {
		return AuthenticateResult{}, err
	}

	if err := validateClaims(claims); err != nil {
		return AuthenticateResult{}, err
	}

	userID, err := uuid.Parse(claims.Subject)
	if err != nil || userID == uuid.Nil {
		return AuthenticateResult{}, fmt.Errorf(
			"invalid subject: %w", ErrInvalidToken,
		)
	}

	if !claims.Role.IsUserOrAdmin() {
		return AuthenticateResult{}, fmt.Errorf(
			"unknown role: %w", ErrInvalidToken,
		)
	}

	return AuthenticateResult{
		UserID: userID,
		Role:   claims.Role,
	}, nil
}

func validateToken(token *jwt.Token) error {
	if token == nil {
		return fmt.Errorf(
			"token is nil: %w", ErrInvalidToken,
		)
	}

	if !token.Valid {
		return ErrInvalidToken
	}

	return nil
}

func validateClaims(claims *accessClaims) error {
	if claims.IssuedAt == nil {
		return fmt.Errorf(
			"invalid issued_at: %w", ErrInvalidToken,
		)
	}

	if claims.ExpiresAt == nil {
		return fmt.Errorf("nil expires_at: %w", ErrInvalidToken)
	}

	if claims.ExpiresAt.IsZero() {
		return fmt.Errorf(
			"invalid expires_at: %w", ErrInvalidToken,
		)
	}

	if claims.Subject == "" {
		return fmt.Errorf(
			"invalid subject: %w", ErrInvalidToken,
		)
	}

	if _, err := uuid.Parse(claims.Subject); err != nil {
		return fmt.Errorf(
			"invalid subject: %w: %w", err, ErrInvalidToken,
		)
	}

	return nil
}
