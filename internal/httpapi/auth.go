package httpapi

import (
	"context"

	"github.com/yyeart/flashdrop/internal/identity"
)

type Authenticator interface {
	Authenticate(
		context.Context,
		string,
	) (identity.AuthenticateResult, error)
}
