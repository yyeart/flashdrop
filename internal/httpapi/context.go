package httpapi

import (
	"context"

	"github.com/yyeart/flashdrop/internal/identity"
)

type requestIDContextKey struct{}
type principalContextKey struct{}

var (
	requestKey   requestIDContextKey
	principalKey principalContextKey
)

func requestIDFromContext(ctx context.Context) (string, bool) {
	requestID, ok := ctx.Value(requestKey).(string)

	return requestID, ok
}

func principalFromContext(
	ctx context.Context,
) (*identity.AuthenticateResult, error) {
	p, ok := ctx.Value(principalKey).(*identity.AuthenticateResult)
	if !ok || p == nil {
		return nil, ErrNotAuthenticated
	}

	return p, nil
}
