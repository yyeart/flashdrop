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

type adminSaleService interface {
	CreateSale(ctx context.Context, sale flashsale.Sale) error

	AddSaleItem(
		ctx context.Context,
		saleID, itemID, productID uuid.UUID,
		name string,
		price flashsale.Money,
		totalQty int,
	) error

	ActivateSale(
		ctx context.Context,
		saleID uuid.UUID,
		now time.Time,
	) (flashsale.Sale, error)

	EndSale(
		ctx context.Context,
		saleID uuid.UUID,
	) (flashsale.Sale, error)
}

type AdminSaleHandler struct {
	sales  adminSaleService
	logger *slog.Logger
	newID  func() uuid.UUID
	now    func() time.Time
}

func NewAdminSaleHandler(
	sales adminSaleService,
	logger *slog.Logger,
	newID func() uuid.UUID,
	now func() time.Time,
) (*AdminSaleHandler, error) {
	if sales == nil {
		return nil, ErrNilAdminSaleService
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

	return &AdminSaleHandler{
		sales:  sales,
		logger: logger,
		newID:  newID,
		now:    now,
	}, nil
}

func (h *AdminSaleHandler) Create(
	w http.ResponseWriter,
	r *http.Request,
) {
	if r.Context().Err() != nil {
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

	var request createSaleRequest
	if err := decodeJSON(w, r, &request); err != nil {
		handleBadRequest(w, r, h.logger, "invalid request body")

		return
	}

	if request.StartsAt == nil || request.EndsAt == nil {
		handleBadRequest(w, r, h.logger, "invalid request body")

		return
	}

	id := h.newID()
	now := h.now()

	sale, err := flashsale.NewSale(
		id,
		*request.StartsAt,
		*request.EndsAt,
		now,
	)
	if err != nil {
		if r.Context().Err() != nil {
			return
		}

		h.handleNewSaleError(w, r, err)

		return
	}

	if err := h.sales.CreateSale(r.Context(), sale); err != nil {
		if r.Context().Err() != nil {
			return
		}

		h.handleCreateError(w, r, err)

		return
	}

	if r.Context().Err() != nil {
		return
	}

	response := saleToResponse(sale)

	if err := writeJson(w, http.StatusCreated, response); err != nil {
		logHandlerFailure(h.logger, w, r, "create_sale_response_write_failed")
	}
}

func (h *AdminSaleHandler) handleNewSaleError(
	w http.ResponseWriter,
	r *http.Request,
	err error,
) {
	switch {
	case errors.Is(err, flashsale.ErrInvalidConfiguration):
		writeErrorAndLog(
			h.logger,
			w,
			r,
			http.StatusBadRequest,
			codeBadRequest,
			"validation failed",
		)

	default:
		logHandlerFailure(h.logger, w, r, "create_sale_error")

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

func (h *AdminSaleHandler) handleCreateError(
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
		logHandlerFailure(h.logger, w, r, "create_sale_error")

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

func (h *AdminSaleHandler) AddItem(
	w http.ResponseWriter,
	r *http.Request,
) {
	if r.Context().Err() != nil {
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

	saleID, err := parseSaleID(r)
	if err != nil {
		if r.Context().Err() != nil {
			return
		}

		writeErrorAndLog(
			h.logger,
			w,
			r,
			http.StatusBadRequest,
			codeBadRequest,
			"saleID_parsing_failed",
		)

		return
	}

	request, price, validationMessage := decodeAddSaleItemRequest(w, r)
	if validationMessage != "" {
		handleBadRequest(w, r, h.logger, validationMessage)
		return
	}

	if request.Name == nil {
		handleBadRequest(w, r, h.logger, "invalid request body")
		return
	}

	itemID := h.newID()

	if err := h.sales.AddSaleItem(
		r.Context(),
		saleID, itemID, request.ProductID,
		*request.Name, price, request.TotalQuantity,
	); err != nil {
		if r.Context().Err() != nil {
			return
		}

		h.handleAddSaleItemError(w, r, err)

		return
	}

	response := saleItemResponse{
		ID:                itemID,
		SaleID:            saleID,
		ProductID:         request.ProductID,
		Name:              *request.Name,
		Price:             moneyToString(price),
		TotalQuantity:     request.TotalQuantity,
		ReservedQuantity:  0,
		SoldQuantity:      0,
		AvailableQuantity: request.TotalQuantity,
	}

	if r.Context().Err() != nil {
		return
	}

	if err := writeJson(w, http.StatusCreated, response); err != nil {
		logHandlerFailure(h.logger, w, r, "add_sale_item_response_write_failed")
	}
}

func decodeAddSaleItemRequest(
	w http.ResponseWriter,
	r *http.Request,
) (addSaleItemRequest, flashsale.Money, string) {
	var request addSaleItemRequest
	if err := decodeJSON(w, r, &request); err != nil {
		return request, flashsale.Money{}, "invalid request body"
	}

	if request.ProductID == uuid.Nil {
		return request, flashsale.Money{}, "invalid request body"
	}

	price, err := flashsale.ParseMoney(request.Price)
	if err != nil {
		return request, flashsale.Money{}, "invalid money value"
	}

	if price.AmountMinor() <= 0 ||
		request.TotalQuantity <= 0 ||
		request.TotalQuantity > math.MaxInt32 {
		return request, flashsale.Money{}, "invalid request body"
	}

	return request, price, ""
}

func (h *AdminSaleHandler) handleAddSaleItemError(
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

	case errors.Is(err, flashsale.ErrSaleNotFound):
		writeErrorAndLog(
			h.logger,
			w,
			r,
			http.StatusNotFound,
			codeNotFound,
			"sale not found",
		)

	case errors.Is(err, flashsale.ErrForbiddenTransition),
		errors.Is(err, flashsale.ErrDuplicateSaleItem),
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
		logHandlerFailure(h.logger, w, r, "add_sale_item_error")

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

func (h *AdminSaleHandler) Activate(
	w http.ResponseWriter,
	r *http.Request,
) {
	if r.Context().Err() != nil {
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

	saleID, err := parseSaleID(r)
	if err != nil {
		if r.Context().Err() != nil {
			return
		}

		writeErrorAndLog(
			h.logger,
			w,
			r,
			http.StatusBadRequest,
			codeBadRequest,
			"saleID_parsing_failed",
		)

		return
	}

	now := h.now()

	sale, err := h.sales.ActivateSale(r.Context(), saleID, now)
	if err != nil {
		if r.Context().Err() != nil {
			return
		}

		h.handleActivateEndSaleError(w, r, err, "activate_sale_error")

		return
	}

	response := saleToResponse(sale)

	if r.Context().Err() != nil {
		return
	}

	if err := writeJson(w, http.StatusOK, response); err != nil {
		logHandlerFailure(h.logger, w, r, "activate_sale_response_write_failed")
	}
}

func (h *AdminSaleHandler) End(
	w http.ResponseWriter,
	r *http.Request,
) {
	if r.Context().Err() != nil {
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

	saleID, err := parseSaleID(r)
	if err != nil {
		if r.Context().Err() != nil {
			return
		}

		writeErrorAndLog(
			h.logger,
			w,
			r,
			http.StatusBadRequest,
			codeBadRequest,
			"saleID_parsing_failed",
		)

		return
	}

	sale, err := h.sales.EndSale(r.Context(), saleID)
	if err != nil {
		if r.Context().Err() != nil {
			return
		}

		h.handleActivateEndSaleError(w, r, err, "end_sale_error")

		return
	}

	response := saleToResponse(sale)

	if r.Context().Err() != nil {
		return
	}

	if err := writeJson(w, http.StatusOK, response); err != nil {
		logHandlerFailure(h.logger, w, r, "end_sale_response_write_failed")
	}
}

func (h *AdminSaleHandler) handleActivateEndSaleError(
	w http.ResponseWriter,
	r *http.Request,
	err error,
	failureReason string,
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

	case errors.Is(err, flashsale.ErrSaleNotFound):
		writeErrorAndLog(
			h.logger,
			w,
			r,
			http.StatusNotFound,
			codeNotFound,
			"sale not found",
		)

	case errors.Is(err, flashsale.ErrForbiddenTransition),
		errors.Is(err, flashsale.ErrExpiredTimeWindow),
		errors.Is(err, flashsale.ErrInvalidConfiguration),
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
		logHandlerFailure(h.logger, w, r, failureReason)

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
