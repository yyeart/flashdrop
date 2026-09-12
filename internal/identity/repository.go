package identity

import "context"

type userRepository interface {
	CreateUser(
		ctx context.Context,
		user User,
		passwordHash PasswordHash,
	) error

	FindLoginCreds(
		ctx context.Context,
		email string,
	) (LoginCredentials, bool, error)
}
