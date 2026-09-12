package identity

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type LoginInput struct {
	Email    string
	Password string
}

type LoginResult struct {
	AccessToken string
	ExpiresIn   time.Duration
}

type LoginCredentials struct {
	UserID       uuid.UUID
	Role         Role
	PasswordHash PasswordHash
}

var (
	ErrInvalidCredentials = errors.New("invalid credentials")
)

func (s *Service) Login(
	ctx context.Context,
	input LoginInput,
) (LoginResult, error) {
	email, err := normalizeEmail(input.Email)
	if err != nil {
		return LoginResult{}, fmt.Errorf(
			"normalize email: %w: %w", err, ErrInvalidCredentials,
		)
	}

	credentials, found, err := s.users.FindLoginCreds(ctx, email)
	if err != nil {
		return LoginResult{}, fmt.Errorf(
			"find login credentials: %w", err,
		)
	}

	hashToCheck := s.dummyPasswordHash

	if found {
		hashToCheck = credentials.PasswordHash
	}

	encodedHash, err := hashToCheck.Encoded()
	if err != nil {
		return LoginResult{}, fmt.Errorf("get hash: %w", err)
	}

	matched, err := verifyPassword(input.Password, encodedHash)
	if err != nil {
		return LoginResult{}, fmt.Errorf("verify password: %w", err)
	}

	if !found || !matched {
		return LoginResult{}, ErrInvalidCredentials
	}

	token, err := issueAccessToken(
		credentials.UserID, credentials.Role,
		s.now().UTC(), s.tokenConfig,
	)
	if err != nil {
		return LoginResult{}, fmt.Errorf("issue access token: %w", err)
	}

	return LoginResult{
		AccessToken: token,
		ExpiresIn:   s.tokenConfig.TTL,
	}, nil
}
