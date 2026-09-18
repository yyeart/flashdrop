package httpapi

import (
	"log/slog"
	"net/http"

	"github.com/google/uuid"
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
	if r.Context().Err() != nil {
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

func parseSaleID(r *http.Request) (uuid.UUID, error) {
	saleIDStr := r.PathValue("saleId")
	saleID, err := uuid.Parse(saleIDStr)
	if err != nil {
		return uuid.Nil, err
	}

	if saleID == uuid.Nil {
		return uuid.Nil, ErrInvalidSaleID
	}

	return saleID, nil
}
