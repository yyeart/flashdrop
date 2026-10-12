package identity

import "errors"

var (
	ErrInvalidEmail       = errors.New("invalid email")
	ErrEmailAlreadyExists = errors.New("email already exists")
	ErrSeedAdminConflict  = errors.New("seed admin conflict")
)
