package identity

import (
	"context"
	"fmt"
	"net/mail"
	"strings"
)

type RegisterInput struct {
	Email    string
	Password string
}

type userRepository interface {
	CreateUser(
		ctx context.Context,
		user User,
		passwordHash PasswordHash,
	) error
}

func (s *Service) Register(
	ctx context.Context,
	input RegisterInput,
) (User, error) {
	email, err := normalizeEmail(input.Email)
	if err != nil {
		return User{}, err
	}

	passwordHash, err := s.hashPassword(input.Password)
	if err != nil {
		return User{}, fmt.Errorf("hash password: %w", err)
	}

	user := User{
		id:        s.newID(),
		email:     email,
		role:      RoleUser,
		createdAt: s.now().UTC(),
	}

	if err := s.users.CreateUser(ctx, user, passwordHash); err != nil {
		return User{}, fmt.Errorf("register user: %w", err)
	}

	return user, nil
}

func normalizeEmail(email string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(email))

	parsed, err := mail.ParseAddress(normalized)
	if err != nil {
		return "", ErrInvalidEmail
	}

	if parsed.Address != normalized || parsed.Name != "" {
		return "", ErrInvalidEmail
	}

	return normalized, nil
}
