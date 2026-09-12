package identity

import (
	"bytes"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type Service struct {
	users             userRepository
	dummyPasswordHash PasswordHash
	tokenConfig       TokenConfig

	newID        func() uuid.UUID
	now          func() time.Time
	hashPassword func(string) (PasswordHash, error)
}

func NewService(users userRepository, cfg TokenConfig) (*Service, error) {
	if err := validateTokenConfig(cfg); err != nil {
		return nil, fmt.Errorf("validate token config: %w", err)
	}

	cfg.PrivateKey = bytes.Clone(cfg.PrivateKey)
	cfg.PublicKey = bytes.Clone(cfg.PublicKey)

	dummyHash, err := hashPassword("password-for-non-existent-user")
	if err != nil {
		return nil, fmt.Errorf("create dummy password: %w", err)
	}

	return &Service{
		users:             users,
		dummyPasswordHash: dummyHash,
		tokenConfig:       cfg,
		newID:             uuid.New,
		now:               time.Now,
		hashPassword:      hashPassword,
	}, nil
}
