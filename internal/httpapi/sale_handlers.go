package httpapi

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"

	"github.com/google/uuid"
	"github.com/yyeart/flashdrop/internal/flashsale"
)

type publicSaleService interface {
	ListActiveSales(
		ctx context.Context,
		limit int,
		offset int,
	) ([]flashsale.Sale, error)

	FindActiveSale(
		ctx context.Context,
		saleID uuid.UUID,
	) (flashsale.Sale, error)
}

type PublicSaleHandler struct {
	sales  publicSaleService
	logger *slog.Logger
}

func NewPublicSaleHandler(
	sales publicSaleService,
	logger *slog.Logger,
) (*PublicSaleHandler, error) {
	if logger == nil {
		return nil, ErrNilLogger
	}

	if sales == nil {
		return nil, ErrNilPublicSaleService
	}

	return &PublicSaleHandler{
		sales:  sales,
		logger: logger,
	}, nil
}

func (h *PublicSaleHandler) List(
	w http.ResponseWriter,
	r *http.Request,
) {
	if r.Context().Err() != nil {
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

	limit, offset, err := parseSalePagination(r.URL.Query())
	if err != nil {
		handleBadRequest(w, r, h.logger, "error parsing pagination parameters")

		return
	}

	sales, err := h.sales.ListActiveSales(r.Context(), limit, offset)
	if err != nil {
		if r.Context().Err() != nil {
			return
		}

		h.handleListError(w, r, err)
		return
	}

	saleResponses := make([]saleResponse, 0, len(sales))
	for _, sale := range sales {
		saleResponses = append(saleResponses, saleToResponse(sale))
	}

	response := salePageResponse{
		Sales:  saleResponses,
		Limit:  limit,
		Offset: offset,
	}

	if r.Context().Err() != nil {
		return
	}

	if err := writeJson(w, http.StatusOK, response); err != nil {
		logHandlerFailure(h.logger, w, r, "list_response_write_failed")
	}
}

func (h *PublicSaleHandler) handleListError(
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
			"request_deadline_exceeded",
		)

	case errors.Is(err, flashsale.ErrInvalidPagination):
		writeErrorAndLog(
			h.logger,
			w,
			r,
			http.StatusBadRequest,
			codeBadRequest,
			"invalid pagination",
		)

	default:
		logHandlerFailure(h.logger, w, r, "list_sales_failed")

		writeErrorAndLog(
			h.logger,
			w,
			r,
			http.StatusInternalServerError,
			codeInternalError,
			"internal_server_error",
		)
	}
}

func parseSalePagination(
	values url.Values,
) (limit int, offset int, err error) {
	const (
		defaultLimit  = 20
		defaultOffset = 0
		maxLimit      = 100
	)

	limit = defaultLimit
	offset = defaultOffset

	if limits, ok := values["limit"]; ok {
		if len(limits) != 1 {
			return 0, 0, fmt.Errorf(
				"limit must be specified once: %w",
				ErrInvalidPagination,
			)
		}

		limit, err = strconv.Atoi(limits[0])
		if err != nil {
			return 0, 0, fmt.Errorf(
				"invalid limit: %w: %w",
				err,
				ErrInvalidPagination,
			)
		}

		if limit < 1 || limit > maxLimit {
			return 0, 0, fmt.Errorf(
				"limit must be between 1 and 100: %w",
				ErrInvalidPagination,
			)
		}
	}

	if offsets, ok := values["offset"]; ok {
		if len(offsets) != 1 {
			return 0, 0, fmt.Errorf(
				"offset must be specified once: %w",
				ErrInvalidPagination,
			)
		}

		offset, err = strconv.Atoi(offsets[0])
		if err != nil {
			return 0, 0, fmt.Errorf(
				"invalid offset: %w: %w",
				err,
				ErrInvalidPagination,
			)
		}

		if offset < 0 {
			return 0, 0, fmt.Errorf(
				"offset must be >= 0: %w",
				ErrInvalidPagination,
			)
		}
	}

	return limit, offset, nil
}

func (h *PublicSaleHandler) Get(
	w http.ResponseWriter,
	r *http.Request,
) {
	if r.Context().Err() != nil {
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
			"method_not_allowed",
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

	sale, err := h.sales.FindActiveSale(r.Context(), saleID)
	if err != nil {
		if r.Context().Err() != nil {
			return
		}

		h.handleGetSaleError(w, r, err)

		return
	}

	response := saleToResponse(sale)

	if r.Context().Err() != nil {
		return
	}

	if err := writeJson(w, http.StatusOK, response); err != nil {
		logHandlerFailure(h.logger, w, r, "sale_response_write_failed")
	}
}

func (h *PublicSaleHandler) handleGetSaleError(
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
			"request_deadline_exceeded",
		)

	case errors.Is(err, flashsale.ErrSaleNotFound):
		writeErrorAndLog(
			h.logger,
			w,
			r,
			http.StatusNotFound,
			codeNotFound,
			"sale_not_found",
		)

	default:
		logHandlerFailure(h.logger, w, r, "get_sale_failed")

		writeErrorAndLog(
			h.logger,
			w,
			r,
			http.StatusInternalServerError,
			codeInternalError,
			"internal_server_error",
		)
	}
}
