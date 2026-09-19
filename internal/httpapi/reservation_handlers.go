package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/yyeart/flashdrop/internal/flashsale"
)

type reservationService interface {
	Reserve(
		context.Context,
		flashsale.ReserveCommand,
		time.Time,
	) (flashsale.ReserveResult, error)

	Cancel(
		context.Context,
		uuid.UUID,
		uuid.UUID,
		time.Time,
	) error

	Pay(
		context.Context,
		uuid.UUID,
		uuid.UUID,
		uuid.UUID,
		time.Time,
	) (flashsale.Order, error)
}

type ReservationHandler struct {
	service        reservationService
	logger         *slog.Logger
	reservationTTL time.Duration

	newID func() uuid.UUID
	now   func() time.Time
}

func NewReservationHandler(
	service reservationService,
	logger *slog.Logger,
	newID func() uuid.UUID,
	now func() time.Time,
	reservationTTL time.Duration,
) (*ReservationHandler, error) {
	if service == nil {
		return nil, ErrNilReservationService
	}

	if logger == nil {
		return nil, ErrNilLogger
	}

	if newID == nil {
		return nil, ErrNilNewIDFunc
	}

	if now == nil {
		return nil, ErrNilNowFunc
	}

	if reservationTTL <= 0 {
		return nil, ErrInvalidDuration
	}

	return &ReservationHandler{
		service:        service,
		logger:         logger,
		reservationTTL: reservationTTL,
		newID:          newID,
		now:            now,
	}, nil
}

func (h *ReservationHandler) Reserve(
	w http.ResponseWriter,
	r *http.Request,
) {
	if handleRequestContextError(w, r, h.logger) {
		return
	}

	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)

		writeErrorAndLog(
			h.logger,
			w,
			r,
			http.StatusMethodNotAllowed,
			codeMethodNotAllowed,
			"method not allowed",
		)

		return
	}

	userID, ok := authenticatedUserID(w, r, h.logger)
	if !ok {
		return
	}

	command, currentTime, ok := h.reserveCommand(w, r, userID)
	if !ok {
		return
	}

	result, err := h.service.Reserve(
		r.Context(),
		command,
		currentTime,
	)
	if err != nil {
		if handleRequestContextError(w, r, h.logger) {
			return
		}

		h.handleReserveError(w, r, err)

		return
	}

	if handleRequestContextError(w, r, h.logger) {
		return
	}

	var status int
	if result.Replayed {
		status = http.StatusOK
	} else {
		status = http.StatusCreated
	}

	response := reservationToResponse(result.Reservation)

	if err := writeJson(w, status, response); err != nil {
		logHandlerFailure(h.logger, w, r, "reserve_result_write_failed")
	}
}

func (h *ReservationHandler) reserveCommand(
	w http.ResponseWriter,
	r *http.Request,
	userID uuid.UUID,
) (flashsale.ReserveCommand, time.Time, bool) {
	key, err := parseIdempotencyKey(r)
	if err != nil {
		handleBadRequest(w, r, h.logger, "error parsing idempotency key")

		return flashsale.ReserveCommand{}, time.Time{}, false
	}

	var request reserveRequest
	if err := decodeJSON(w, r, &request); err != nil {
		handleBadRequest(w, r, h.logger, "invalid request body")

		return flashsale.ReserveCommand{}, time.Time{}, false
	}

	if request.SaleItemID == uuid.Nil || request.Quantity < 1 || request.Quantity > math.MaxInt32 {
		handleBadRequest(w, r, h.logger, "invalid request body")

		return flashsale.ReserveCommand{}, time.Time{}, false
	}

	reservationID := h.newID()
	if reservationID == uuid.Nil {
		writeErrorAndLog(
			h.logger,
			w,
			r,
			http.StatusInternalServerError,
			codeInternalError,
			"internal server error",
		)

		return flashsale.ReserveCommand{}, time.Time{}, false
	}

	currentTime := h.now().UTC()

	return flashsale.ReserveCommand{
		ReservationID:  reservationID,
		UserID:         userID,
		SaleItemID:     request.SaleItemID,
		Quantity:       request.Quantity,
		ExpiresAt:      currentTime.Add(h.reservationTTL),
		IdempotencyKey: key,
	}, currentTime, true
}

func (h *ReservationHandler) handleReserveError(
	w http.ResponseWriter,
	r *http.Request,
	err error,
) {
	switch {
	case errors.Is(err, context.Canceled):
		return

	case errors.Is(err, context.DeadlineExceeded):
		writeErrorAndLog(
			h.logger,
			w,
			r,
			http.StatusGatewayTimeout,
			codeDeadlineExceeded,
			"request deadline exceeded",
		)

	case errors.Is(err, flashsale.ErrInvalidQuantity):
		writeErrorAndLog(
			h.logger,
			w,
			r,
			http.StatusBadRequest,
			codeBadRequest,
			"invalid quantity",
		)

	case errors.Is(err, flashsale.ErrReservationUnavailable):
		writeErrorAndLog(
			h.logger,
			w,
			r,
			http.StatusConflict,
			codeConflict,
			"reservation unavailable",
		)

	case errors.Is(err, flashsale.ErrConflict):
		writeErrorAndLog(
			h.logger,
			w,
			r,
			http.StatusConflict,
			codeConflict,
			"conflict",
		)

	default:
		logHandlerFailure(h.logger, w, r, "reserve_failed")

		writeErrorAndLog(
			h.logger,
			w,
			r,
			http.StatusInternalServerError,
			codeInternalError,
			"internal server error",
		)
	}
}

func (h *ReservationHandler) Cancel(
	w http.ResponseWriter,
	r *http.Request,
) {
	if handleRequestContextError(w, r, h.logger) {
		return
	}

	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)

		writeErrorAndLog(
			h.logger,
			w,
			r,
			http.StatusMethodNotAllowed,
			codeMethodNotAllowed,
			"method not allowed",
		)

		return
	}

	userID, ok := authenticatedUserID(w, r, h.logger)
	if !ok {
		return
	}

	reservationID, err := parseReservationID(r)
	if err != nil {
		if handleRequestContextError(w, r, h.logger) {
			return
		}

		writeErrorAndLog(
			h.logger,
			w,
			r,
			http.StatusBadRequest,
			codeBadRequest,
			"error parsing reservation id",
		)
		return
	}

	currentTime := h.now().UTC()

	if err := h.service.Cancel(
		r.Context(),
		userID,
		reservationID,
		currentTime,
	); err != nil {
		if handleRequestContextError(w, r, h.logger) {
			return
		}

		h.handleCancelError(w, r, err)

		return
	}

	if handleRequestContextError(w, r, h.logger) {
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *ReservationHandler) handleCancelError(
	w http.ResponseWriter,
	r *http.Request,
	err error,
) {
	switch {
	case errors.Is(err, context.Canceled):
		return

	case errors.Is(err, context.DeadlineExceeded):
		writeErrorAndLog(
			h.logger,
			w,
			r,
			http.StatusGatewayTimeout,
			codeDeadlineExceeded,
			"request deadline exceeded",
		)

	case errors.Is(err, flashsale.ErrReservationNotFound):
		writeErrorAndLog(
			h.logger,
			w,
			r,
			http.StatusNotFound,
			codeNotFound,
			"reservation not found",
		)

	case errors.Is(err, flashsale.ErrForbiddenTransition):
		writeErrorAndLog(
			h.logger,
			w,
			r,
			http.StatusConflict,
			codeConflict,
			"forbidden transition",
		)

	case errors.Is(err, flashsale.ErrExpiredTimeWindow):
		writeErrorAndLog(
			h.logger,
			w,
			r,
			http.StatusConflict,
			codeConflict,
			"expired time window",
		)

	default:
		logHandlerFailure(h.logger, w, r, "cancel_failed")

		writeErrorAndLog(
			h.logger,
			w,
			r,
			http.StatusInternalServerError,
			codeInternalError,
			"internal server error",
		)
	}
}

func (h *ReservationHandler) Pay(
	w http.ResponseWriter,
	r *http.Request,
) {
	if handleRequestContextError(w, r, h.logger) {
		return
	}

	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)

		writeErrorAndLog(
			h.logger,
			w,
			r,
			http.StatusMethodNotAllowed,
			codeMethodNotAllowed,
			"method not allowed",
		)

		return
	}

	userID, ok := authenticatedUserID(w, r, h.logger)
	if !ok {
		return
	}

	reservationID, err := parseReservationID(r)
	if err != nil {
		if handleRequestContextError(w, r, h.logger) {
			return
		}

		writeErrorAndLog(
			h.logger,
			w,
			r,
			http.StatusBadRequest,
			codeBadRequest,
			"error parsing reservation id",
		)

		return
	}

	orderID := h.newID()
	if orderID == uuid.Nil {
		if handleRequestContextError(w, r, h.logger) {
			return
		}

		writeErrorAndLog(
			h.logger,
			w,
			r,
			http.StatusInternalServerError,
			codeInternalError,
			"internal server error",
		)

		return
	}

	currentTime := h.now().UTC()

	order, err := h.service.Pay(
		r.Context(),
		userID, reservationID, orderID,
		currentTime,
	)
	if err != nil {
		if handleRequestContextError(w, r, h.logger) {
			return
		}

		h.handlePayError(w, r, err)

		return
	}

	if handleRequestContextError(w, r, h.logger) {
		return
	}

	response := orderToResponse(order)

	if err := writeJson(w, http.StatusCreated, response); err != nil {
		logHandlerFailure(h.logger, w, r, "pay_response_write_failed")
	}
}

func (h *ReservationHandler) handlePayError(
	w http.ResponseWriter,
	r *http.Request,
	err error,
) {
	switch {
	case errors.Is(err, context.Canceled):
		return

	case errors.Is(err, context.DeadlineExceeded):
		writeErrorAndLog(
			h.logger,
			w,
			r,
			http.StatusGatewayTimeout,
			codeDeadlineExceeded,
			"request deadline exceeded",
		)

	case errors.Is(err, flashsale.ErrReservationNotFound):
		writeErrorAndLog(
			h.logger,
			w,
			r,
			http.StatusNotFound,
			codeNotFound,
			"reservation not found",
		)

	case errors.Is(err, flashsale.ErrForbiddenTransition),
		errors.Is(err, flashsale.ErrExpiredTimeWindow),
		errors.Is(err, flashsale.ErrConflict):
		writeErrorAndLog(
			h.logger,
			w,
			r,
			http.StatusConflict,
			codeConflict,
			"conflict",
		)

	default:
		logHandlerFailure(h.logger, w, r, "pay_failed")

		writeErrorAndLog(
			h.logger,
			w,
			r,
			http.StatusInternalServerError,
			codeInternalError,
			"internal server error",
		)
	}
}
