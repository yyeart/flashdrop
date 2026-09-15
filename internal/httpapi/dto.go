package httpapi

import (
	"time"

	"github.com/google/uuid"
	"github.com/yyeart/flashdrop/internal/identity"
)

type registerRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type registerResponse struct {
	ID        uuid.UUID     `json:"id"`
	Email     string        `json:"email"`
	Role      identity.Role `json:"role"`
	CreatedAt time.Time     `json:"created_at"`
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type loginResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int64  `json:"expires_in"`
}
