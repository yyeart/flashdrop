package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/google/uuid"
	"github.com/yyeart/flashdrop/internal/flashsale"
)

type orderService interface {
	FindOrder(
		context.Context,
		uuid.UUID,
		uuid.UUID,
	) (flashsale.Order, error)
}

type OrderHandler struct {
	service orderService
	logger  *slog.Logger
}

func NewOrderHandler(
	service orderService,
	logger *slog.Logger,
) (*OrderHandler, error) {
	if service == nil {
		return nil, ErrNilOrderService
	}

	if logger == nil {
		return nil, ErrNilLogger
	}

	return &OrderHandler{
		service: service,
		logger:  logger,
	}, nil
}

func (h *OrderHandler) Get(
	w http.ResponseWriter,
	r *http.Request,
) {
	if handleRequestContextError(w, r, h.logger) {
		return
	}

	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)

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

	orderID, err := parseOrderID(r)
	if err != nil {
		if handleRequestContextError(w, r, h.logger) {
			return
		}

		handleBadRequest(w, r, h.logger, "error parsing order id")

		return
	}

	order, err := h.service.FindOrder(r.Context(), userID, orderID)
	if err != nil {
		if handleRequestContextError(w, r, h.logger) {
			return
		}

		h.handleGetError(w, r, err)

		return
	}

	if handleRequestContextError(w, r, h.logger) {
		return
	}

	response := orderToResponse(order)

	if err := writeJson(w, http.StatusOK, response); err != nil {
		logHandlerFailure(h.logger, w, r, "get_order_response_write_failed")
	}
}

func (h *OrderHandler) handleGetError(
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

	case errors.Is(err, flashsale.ErrOrderNotFound):
		writeErrorAndLog(
			h.logger,
			w,
			r,
			http.StatusNotFound,
			codeNotFound,
			"order not found",
		)

	default:
		logHandlerFailure(h.logger, w, r, "get_order_failed")

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
