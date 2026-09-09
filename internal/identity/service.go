package identity

import (
	"time"

	"github.com/google/uuid"
)

type Service struct {
	users        userRepository
	newID        func() uuid.UUID
	now          func() time.Time
	hashPassword func(string) (string, error)
}

func NewService(users userRepository) *Service {
	return &Service{
		users:        users,
		newID:        uuid.New,
		now:          time.Now,
		hashPassword: hashPassword,
	}
}
