package httpapi

import (
	"fmt"

	"github.com/yyeart/flashdrop/internal/identity"
)

type routerMiddlewares struct {
	recovery, requestID, logging, timeout, rateLimit Middleware
	authentication, authenticatedUserRBAC, adminRBAC Middleware
}

func newRouterMiddleware(deps RouterDeps) (routerMiddlewares, error) {
	recovery, err := NewRecovery(deps.Logger)
	if err != nil {
		return routerMiddlewares{}, fmt.Errorf("create recovery middleware: %w", err)
	}

	requestID, err := NewRequestID(deps.RequestIDGenerator)
	if err != nil {
		return routerMiddlewares{}, fmt.Errorf("create request id middleware: %w", err)
	}

	logging, err := NewLogging(deps.Logger)
	if err != nil {
		return routerMiddlewares{}, fmt.Errorf("create logging middleware: %w", err)
	}

	timeout, err := NewTimeout(deps.RequestTimeout, deps.Logger)
	if err != nil {
		return routerMiddlewares{}, fmt.Errorf("create timeout middleware: %w", err)
	}

	rateLimiter, err := NewRateLimit(deps.RateLimiter, deps.Logger)
	if err != nil {
		return routerMiddlewares{}, fmt.Errorf("create rate limit middleware: %w", err)
	}

	authentication, err := NewAuthentication(deps.Authenticator, deps.Logger)
	if err != nil {
		return routerMiddlewares{}, fmt.Errorf("create authentication middleware: %w", err)
	}

	authenticatedUserRBAC, err := NewRBAC(
		deps.Logger,
		identity.RoleUser,
		identity.RoleAdmin,
	)
	if err != nil {
		return routerMiddlewares{}, fmt.Errorf("create rbac middleware: %w", err)
	}

	adminRBAC, err := NewRBAC(deps.Logger, identity.RoleAdmin)
	if err != nil {
		return routerMiddlewares{}, fmt.Errorf("create rbac middleware: %w", err)
	}

	return routerMiddlewares{
		recovery:              recovery,
		requestID:             requestID,
		logging:               logging,
		timeout:               timeout,
		rateLimit:             rateLimiter,
		authentication:        authentication,
		authenticatedUserRBAC: authenticatedUserRBAC,
		adminRBAC:             adminRBAC,
	}, nil
}
