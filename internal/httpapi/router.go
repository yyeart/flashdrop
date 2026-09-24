package httpapi

import (
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

type RouterDeps struct {
	Auth               *AuthHandler
	PublicSales        *PublicSaleHandler
	AdminSales         *AdminSaleHandler
	Reservations       *ReservationHandler
	Orders             *OrderHandler
	Authenticator      Authenticator
	RateLimiter        RateLimiter
	RequestIDGenerator RequestIDGenerator
	Logger             *slog.Logger
	RequestTimeout     time.Duration
}

func NewRouter(deps RouterDeps) (http.Handler, error) {
	if err := validateHandlers(deps); err != nil {
		return nil, err
	}

	middlewares, err := newRouterMiddleware(deps)
	if err != nil {
		return nil, err
	}

	mux := http.NewServeMux()

	userRoute := func(h http.HandlerFunc) http.Handler {
		return Chain(h, middlewares.authentication, middlewares.authenticatedUserRBAC)
	}
	adminRoute := func(h http.HandlerFunc) http.Handler {
		return Chain(h, middlewares.authentication, middlewares.adminRBAC)
	}

	mux.HandleFunc("/v1/auth/register", deps.Auth.Register)
	mux.HandleFunc("/v1/auth/login", deps.Auth.Login)
	mux.HandleFunc("/v1/sales", deps.PublicSales.List)
	mux.HandleFunc("/v1/sales/{saleId}", deps.PublicSales.Get)

	mux.Handle("/v1/reservations", userRoute(deps.Reservations.Reserve))
	mux.Handle(
		"/v1/reservations/{reservationId}/cancel",
		userRoute(deps.Reservations.Cancel),
	)
	mux.Handle(
		"/v1/reservations/{reservationId}/pay",
		userRoute(deps.Reservations.Pay),
	)
	mux.Handle("/v1/orders/{orderId}", userRoute(deps.Orders.Get))

	mux.Handle("/v1/admin/sales", adminRoute(deps.AdminSales.Create))
	mux.Handle("/v1/admin/sales/{saleId}/items", adminRoute(deps.AdminSales.AddItem))
	mux.Handle("/v1/admin/sales/{saleId}/activate", adminRoute(deps.AdminSales.Activate))
	mux.Handle("/v1/admin/sales/{saleId}/end", adminRoute(deps.AdminSales.End))

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeErrorAndLog(
			deps.Logger, w, r,
			http.StatusNotFound, codeNotFound, "not found",
		)
	})

	return Chain(
		mux,
		middlewares.recovery,
		middlewares.requestID,
		middlewares.logging,
		middlewares.timeout,
		middlewares.rateLimit,
	), nil
}

func validateHandlers(deps RouterDeps) error {
	checks := []struct {
		name    string
		missing bool
	}{
		{"auth handler", deps.Auth == nil},
		{"public sales handler", deps.PublicSales == nil},
		{"admin sales handler", deps.AdminSales == nil},
		{"reservations handler", deps.Reservations == nil},
		{"orders handler", deps.Orders == nil},
	}

	for _, check := range checks {
		if check.missing {
			return fmt.Errorf(
				"%s: %w",
				check.name, ErrNilHandler,
			)
		}
	}

	return nil
}
