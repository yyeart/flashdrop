package identity

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type AdminSeeder struct {
	users userRepository

	newID        func() uuid.UUID
	now          func() time.Time
	hashPassword func(string) (PasswordHash, error)
}

type SeedAdminInput struct {
	Email    string
	Password string
}

func (s *AdminSeeder) SeedAdmin(
	ctx context.Context,
	input SeedAdminInput,
) error {
	email, err := normalizeEmail(input.Email)
	if err != nil {
		return err
	}

	if err := validatePassword(input.Password); err != nil {
		return err
	}

	credentials, found, err := s.users.FindLoginCreds(ctx, email)
	if err != nil {
		return fmt.Errorf("find existing user: %w", err)
	}

	if found {
		return s.validateExistingAdmin(credentials, input.Password)
	}

	passwordHash, err := s.hashPassword(input.Password)
	if err != nil {
		return fmt.Errorf("hash admin password: %w", err)
	}

	admin := User{
		id:        s.newID(),
		email:     email,
		role:      RoleAdmin,
		createdAt: s.now().UTC(),
	}

	err = s.users.CreateUser(ctx, admin, passwordHash)

	switch {
	case err == nil:
		return nil

	case !errors.Is(err, ErrEmailAlreadyExists):
		return fmt.Errorf("create admin: %w", err)

	default:
		credentials, found, err = s.users.FindLoginCreds(ctx, email)
		if err != nil {
			return fmt.Errorf("resolve seed race: %w", err)
		}

		if !found {
			return fmt.Errorf("resolve seed race: %w", ErrSeedAdminConflict)
		}

		return s.validateExistingAdmin(credentials, input.Password)
	}
}

func (s *AdminSeeder) validateExistingAdmin(
	credentials LoginCredentials,
	password string,
) error {
	if credentials.Role != RoleAdmin {
		return ErrSeedAdminConflict
	}

	encodedHash, err := credentials.PasswordHash.Encoded()
	if err != nil {
		return fmt.Errorf("read admin password hash: %w", err)
	}

	matched, err := verifyPassword(password, encodedHash)
	if err != nil {
		return fmt.Errorf("verify admin password: %w", err)
	}

	if !matched {
		return ErrSeedAdminConflict
	}

	return nil
}

func NewAdminSeeder(users userRepository) *AdminSeeder {
	seeder := AdminSeeder{
		users:        users,
		newID:        uuid.New,
		now:          time.Now,
		hashPassword: hashPassword,
	}

	return &seeder
}
