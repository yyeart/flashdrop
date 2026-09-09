package identity

import (
	"time"

	"github.com/google/uuid"
)

type Role string

const (
	RoleUser  Role = "user"
	RoleAdmin Role = "admin"
)

type User struct {
	id        uuid.UUID
	email     string
	role      Role
	createdAt time.Time
}

func (u *User) ID() uuid.UUID {
	return u.id
}

func (u *User) Email() string {
	return u.email
}

func (u *User) Role() Role {
	return u.role
}

func (u *User) CreatedAt() time.Time {
	return u.createdAt
}
