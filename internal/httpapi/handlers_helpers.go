package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/google/uuid"
)

const (
	saleIDPathName        = "saleId"
	reservationIDPathName = "reservationId"
	orderIDPathName       = "orderId"
)

func logHandlerFailure(
	logger *slog.Logger,
	w http.ResponseWriter,
	r *http.Request,
	reason string,
) {
	requestID := requestIDForResponse(r.Context(), w)

	logger.ErrorContext(
		r.Context(),
		"HTTP handler failed",
		slog.String("reason", reason),
		slog.String("request_id", requestID),
		slog.String("method", r.Method),
		slog.String("path", r.URL.Path),
	)
}

func handleBadRequest(
	w http.ResponseWriter,
	r *http.Request,
	logger *slog.Logger,
	message string,
) {
	if handleRequestContextError(w, r, logger) {
		return
	}

	writeErrorAndLog(
		logger,
		w,
		r,
		http.StatusBadRequest,
		codeBadRequest,
		message,
	)
}

func handleRequestContextError(
	w http.ResponseWriter,
	r *http.Request,
	logger *slog.Logger,
) bool {
	switch err := r.Context().Err(); {
	case err == nil:
		return false

	case errors.Is(err, context.Canceled):
		return true

	case errors.Is(err, context.DeadlineExceeded):
		writeErrorAndLog(
			logger,
			w,
			r,
			http.StatusGatewayTimeout,
			codeDeadlineExceeded,
			"request deadline exceeded",
		)

		return true

	default:
		return false
	}
}

func parsePathValue(r *http.Request, key string) (uuid.UUID, error) {
	valStr := r.PathValue(key)
	val, err := uuid.Parse(valStr)
	if err != nil {
		return uuid.Nil, err
	}

	if val == uuid.Nil {
		return uuid.Nil, ErrInvalidID
	}

	return val, nil
}

func parseSaleID(r *http.Request) (uuid.UUID, error) {
	return parsePathValue(r, saleIDPathName)
}

func parseReservationID(r *http.Request) (uuid.UUID, error) {
	return parsePathValue(r, reservationIDPathName)
}

func parseOrderID(r *http.Request) (uuid.UUID, error) {
	return parsePathValue(r, orderIDPathName)
}

func parseIdempotencyKey(r *http.Request) (string, error) {
	values := r.Header.Values("Idempotency-Key")

	if len(values) == 0 {
		return "", ErrInvalidIdempotencyKey
	}

	if len(values) > 1 {
		return "", ErrMultipleIdempotencyKeys
	}

	if len(values[0]) < 1 || len(values[0]) > 255 {
		return "", ErrInvalidIdempotencyKey
	}

	return values[0], nil
}

func authenticatedUserID(
	w http.ResponseWriter,
	r *http.Request,
	logger *slog.Logger,
) (uuid.UUID, bool) {
	principal, err := principalFromContext(r.Context())
	if err == nil &&
		principal.UserID != uuid.Nil &&
		principal.Role.IsUserOrAdmin() {
		return principal.UserID, true
	}

	writeErrorAndLog(
		logger,
		w,
		r,
		http.StatusUnauthorized,
		codeUnauthorized,
		"unauthorized",
	)

	return uuid.Nil, false
}
