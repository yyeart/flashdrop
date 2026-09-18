package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/yyeart/flashdrop/internal/flashsale"
)

type publicSaleServiceStub struct {
	list func(context.Context, int, int) ([]flashsale.Sale, error)
	find func(context.Context, uuid.UUID) (flashsale.Sale, error)

	listCalls int
	gotLimit  int
	gotOffset int
	findCalls int
	gotSaleID uuid.UUID
}

func (s *publicSaleServiceStub) ListActiveSales(
	ctx context.Context,
	limit int,
	offset int,
) ([]flashsale.Sale, error) {
	s.listCalls++
	s.gotLimit = limit
	s.gotOffset = offset

	if s.list == nil {
		return nil, errors.New("unexpected ListActiveSales call")
	}

	return s.list(ctx, limit, offset)
}

func (s *publicSaleServiceStub) FindActiveSale(
	ctx context.Context,
	saleID uuid.UUID,
) (flashsale.Sale, error) {
	s.findCalls++
	s.gotSaleID = saleID

	if s.find == nil {
		return flashsale.Sale{}, errors.New("unexpected FindActiveSale call")
	}

	return s.find(ctx, saleID)
}

func newPublicSaleHandlerForTest(
	t *testing.T,
	service publicSaleService,
) *PublicSaleHandler {
	t.Helper()

	handler, err := NewPublicSaleHandler(
		service,
		slog.New(slog.DiscardHandler),
	)
	if err != nil {
		t.Fatalf("NewPublicSaleHandler() error = %v, want nil", err)
	}

	return handler
}

func servePublicSaleList(
	t *testing.T,
	handler *PublicSaleHandler,
	request *http.Request,
) *httptest.ResponseRecorder {
	t.Helper()

	recorder := httptest.NewRecorder()
	requestIDMiddleware(t)(http.HandlerFunc(handler.List)).ServeHTTP(recorder, request)

	return recorder
}

func servePublicSaleGet(
	t *testing.T,
	handler *PublicSaleHandler,
	request *http.Request,
) *httptest.ResponseRecorder {
	t.Helper()

	recorder := httptest.NewRecorder()
	requestIDMiddleware(t)(http.HandlerFunc(handler.Get)).ServeHTTP(recorder, request)

	return recorder
}

func activeSaleForHandlerTest(
	t *testing.T,
	saleID uuid.UUID,
) flashsale.Sale {
	t.Helper()

	startsAt := time.Date(2026, time.September, 15, 10, 0, 0, 0, time.UTC)
	sale, err := flashsale.RehydrateSale(flashsale.SaleSnapshot{
		ID:        saleID,
		State:     flashsale.ActiveState,
		StartsAt:  startsAt,
		EndsAt:    startsAt.Add(time.Hour),
		CreatedAt: startsAt.Add(-time.Hour),
		Items: []flashsale.SaleItemSnapshot{
			{
				ID:          uuid.MustParse("22222222-2222-2222-2222-222222222222"),
				SaleID:      saleID,
				ProductID:   uuid.MustParse("33333333-3333-3333-3333-333333333333"),
				Name:        "Mechanical keyboard",
				PriceMinor:  1250,
				TotalQty:    10,
				ReservedQty: 3,
				SoldQty:     2,
			},
		},
	})
	if err != nil {
		t.Fatalf("RehydrateSale() error = %v, want nil", err)
	}

	return sale
}

func TestPublicSaleHandler_ListUsesDefaultPagination(t *testing.T) {
	t.Parallel()

	service := &publicSaleServiceStub{
		list: func(_ context.Context, _, _ int) ([]flashsale.Sale, error) {
			return nil, nil
		},
	}
	handler := newPublicSaleHandlerForTest(t, service)
	request := httptest.NewRequestWithContext(
		t.Context(), http.MethodGet, "/v1/sales", nil,
	)

	recorder := servePublicSaleList(t, handler, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", recorder.Code, http.StatusOK, recorder.Body)
	}
	if service.listCalls != 1 {
		t.Fatalf("ListActiveSales() calls = %d, want 1", service.listCalls)
	}
	if service.gotLimit != 20 || service.gotOffset != 0 {
		t.Errorf(
			"ListActiveSales() pagination = (%d, %d), want (20, 0)",
			service.gotLimit,
			service.gotOffset,
		)
	}
}

func TestPublicSaleHandler_ListPassesCustomAndBoundaryPagination(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		query      string
		wantLimit  int
		wantOffset int
	}{
		{name: "custom values", query: "?limit=37&offset=12", wantLimit: 37, wantOffset: 12},
		{name: "minimum values", query: "?limit=1&offset=0", wantLimit: 1, wantOffset: 0},
		{name: "maximum limit", query: "?limit=100&offset=42", wantLimit: 100, wantOffset: 42},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			service := &publicSaleServiceStub{
				list: func(_ context.Context, _, _ int) ([]flashsale.Sale, error) {
					return nil, nil
				},
			}
			handler := newPublicSaleHandlerForTest(t, service)
			request := httptest.NewRequestWithContext(
				t.Context(), http.MethodGet, "/v1/sales"+tt.query, nil,
			)

			recorder := servePublicSaleList(t, handler, request)

			if recorder.Code != http.StatusOK {
				t.Fatalf(
					"status = %d, want %d; body = %s",
					recorder.Code,
					http.StatusOK,
					recorder.Body,
				)
			}
			if service.listCalls != 1 {
				t.Fatalf("ListActiveSales() calls = %d, want 1", service.listCalls)
			}
			if service.gotLimit != tt.wantLimit || service.gotOffset != tt.wantOffset {
				t.Errorf(
					"ListActiveSales() pagination = (%d, %d), want (%d, %d)",
					service.gotLimit,
					service.gotOffset,
					tt.wantLimit,
					tt.wantOffset,
				)
			}
		})
	}
}

func TestPublicSaleHandler_ListRejectsInvalidPaginationWithoutCallingService(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		query string
	}{
		{name: "zero limit", query: "?limit=0"},
		{name: "limit above maximum", query: "?limit=101"},
		{name: "non-numeric limit", query: "?limit=many"},
		{name: "negative offset", query: "?offset=-1"},
		{name: "non-numeric offset", query: "?offset=next"},
		{name: "repeated limit", query: "?limit=1&limit=2"},
		{name: "repeated offset", query: "?offset=1&offset=2"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			service := &publicSaleServiceStub{}
			handler := newPublicSaleHandlerForTest(t, service)
			request := httptest.NewRequestWithContext(
				t.Context(), http.MethodGet, "/v1/sales"+tt.query, nil,
			)

			recorder := servePublicSaleList(t, handler, request)

			assertErrorResponse(t, recorder, http.StatusBadRequest, string(codeBadRequest))
			if service.listCalls != 0 {
				t.Errorf("ListActiveSales() calls = %d, want 0", service.listCalls)
			}
		})
	}
}

func TestPublicSaleHandler_ListRejectsWrongMethod(t *testing.T) {
	t.Parallel()

	service := &publicSaleServiceStub{}
	handler := newPublicSaleHandlerForTest(t, service)
	request := httptest.NewRequestWithContext(
		t.Context(), http.MethodPost, "/v1/sales", nil,
	)

	recorder := servePublicSaleList(t, handler, request)

	assertErrorResponse(t, recorder, http.StatusMethodNotAllowed, string(codeMethodNotAllowed))
	if got := recorder.Header().Get("Allow"); got != http.MethodGet {
		t.Errorf("Allow = %q, want %q", got, http.MethodGet)
	}
	if service.listCalls != 0 {
		t.Errorf("ListActiveSales() calls = %d, want 0", service.listCalls)
	}
}

func TestPublicSaleHandler_ListReturnsEmptyPageAsArrays(t *testing.T) {
	t.Parallel()

	service := &publicSaleServiceStub{
		list: func(_ context.Context, _, _ int) ([]flashsale.Sale, error) {
			return nil, nil
		},
	}
	handler := newPublicSaleHandlerForTest(t, service)
	request := httptest.NewRequestWithContext(
		t.Context(), http.MethodGet, "/v1/sales?limit=10&offset=5", nil,
	)

	recorder := servePublicSaleList(t, handler, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", recorder.Code, http.StatusOK, recorder.Body)
	}
	if got, want := recorder.Body.String(), "{\"sales\":[],\"limit\":10,\"offset\":5}\n"; got != want {
		t.Errorf("body = %q, want %q", got, want)
	}
}

func TestPublicSaleHandler_ListMapsServiceErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		serviceErr error
		wantStatus int
		wantCode   errorCode
	}{
		{
			name:       "invalid pagination",
			serviceErr: fmt.Errorf("store rejected pagination: %w", flashsale.ErrInvalidPagination),
			wantStatus: http.StatusBadRequest,
			wantCode:   codeBadRequest,
		},
		{
			name:       "deadline exceeded",
			serviceErr: fmt.Errorf("query timed out: %w", context.DeadlineExceeded),
			wantStatus: http.StatusGatewayTimeout,
			wantCode:   codeDeadlineExceeded,
		},
		{
			name:       "internal failure",
			serviceErr: errors.New("private database failure"),
			wantStatus: http.StatusInternalServerError,
			wantCode:   codeInternalError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			service := &publicSaleServiceStub{
				list: func(_ context.Context, _, _ int) ([]flashsale.Sale, error) {
					return nil, tt.serviceErr
				},
			}
			handler := newPublicSaleHandlerForTest(t, service)
			request := httptest.NewRequestWithContext(
				t.Context(), http.MethodGet, "/v1/sales", nil,
			)

			recorder := servePublicSaleList(t, handler, request)

			assertErrorResponse(t, recorder, tt.wantStatus, string(tt.wantCode))
			if service.listCalls != 1 {
				t.Errorf("ListActiveSales() calls = %d, want 1", service.listCalls)
			}
			if tt.wantStatus == http.StatusInternalServerError &&
				strings.Contains(recorder.Body.String(), "private database failure") {
				t.Error("internal service error leaked to response")
			}
		})
	}
}

func TestPublicSaleHandler_GetReturnsActiveSale(t *testing.T) {
	t.Parallel()

	saleID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	sale := activeSaleForHandlerTest(t, saleID)
	service := &publicSaleServiceStub{
		find: func(_ context.Context, gotSaleID uuid.UUID) (flashsale.Sale, error) {
			return sale, nil
		},
	}
	handler := newPublicSaleHandlerForTest(t, service)
	request := httptest.NewRequestWithContext(
		t.Context(), http.MethodGet, "/v1/sales/"+saleID.String(), nil,
	)
	request.SetPathValue("saleId", saleID.String())

	recorder := servePublicSaleGet(t, handler, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", recorder.Code, http.StatusOK, recorder.Body)
	}
	if service.findCalls != 1 {
		t.Fatalf("FindActiveSale() calls = %d, want 1", service.findCalls)
	}
	if service.gotSaleID != saleID {
		t.Errorf("FindActiveSale() sale ID = %s, want %s", service.gotSaleID, saleID)
	}

	var got saleResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &got); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	want := saleToResponse(sale)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("response = %#v, want %#v", got, want)
	}
}

func TestPublicSaleHandler_GetRejectsInvalidSaleIDWithoutCallingService(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		saleID string
	}{
		{name: "missing path value", saleID: ""},
		{name: "malformed UUID", saleID: "not-a-uuid"},
		{name: "nil UUID", saleID: uuid.Nil.String()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			service := &publicSaleServiceStub{}
			handler := newPublicSaleHandlerForTest(t, service)
			request := httptest.NewRequestWithContext(
				t.Context(), http.MethodGet, "/v1/sales/"+tt.saleID, nil,
			)
			request.SetPathValue("saleId", tt.saleID)

			recorder := servePublicSaleGet(t, handler, request)

			assertErrorResponse(t, recorder, http.StatusBadRequest, string(codeBadRequest))
			if service.findCalls != 0 {
				t.Errorf("FindActiveSale() calls = %d, want 0", service.findCalls)
			}
		})
	}
}

func TestPublicSaleHandler_GetRejectsWrongMethod(t *testing.T) {
	t.Parallel()

	saleID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	service := &publicSaleServiceStub{}
	handler := newPublicSaleHandlerForTest(t, service)
	request := httptest.NewRequestWithContext(
		t.Context(), http.MethodPost, "/v1/sales/"+saleID.String(), nil,
	)
	request.SetPathValue("saleId", saleID.String())

	recorder := servePublicSaleGet(t, handler, request)

	assertErrorResponse(t, recorder, http.StatusMethodNotAllowed, string(codeMethodNotAllowed))
	if got := recorder.Header().Get("Allow"); got != http.MethodGet {
		t.Errorf("Allow = %q, want %q", got, http.MethodGet)
	}
	if service.findCalls != 0 {
		t.Errorf("FindActiveSale() calls = %d, want 0", service.findCalls)
	}
}

func TestPublicSaleHandler_GetMapsServiceErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		serviceErr error
		wantStatus int
		wantCode   errorCode
	}{
		{
			name:       "sale not found",
			serviceErr: fmt.Errorf("find active sale: %w", flashsale.ErrSaleNotFound),
			wantStatus: http.StatusNotFound,
			wantCode:   codeNotFound,
		},
		{
			name:       "deadline exceeded",
			serviceErr: fmt.Errorf("find active sale: %w", context.DeadlineExceeded),
			wantStatus: http.StatusGatewayTimeout,
			wantCode:   codeDeadlineExceeded,
		},
		{
			name:       "internal failure",
			serviceErr: errors.New("private database failure"),
			wantStatus: http.StatusInternalServerError,
			wantCode:   codeInternalError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			saleID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
			service := &publicSaleServiceStub{
				find: func(_ context.Context, _ uuid.UUID) (flashsale.Sale, error) {
					return flashsale.Sale{}, tt.serviceErr
				},
			}
			handler := newPublicSaleHandlerForTest(t, service)
			request := httptest.NewRequestWithContext(
				t.Context(), http.MethodGet, "/v1/sales/"+saleID.String(), nil,
			)
			request.SetPathValue("saleId", saleID.String())

			recorder := servePublicSaleGet(t, handler, request)

			assertErrorResponse(t, recorder, tt.wantStatus, string(tt.wantCode))
			if service.findCalls != 1 {
				t.Errorf("FindActiveSale() calls = %d, want 1", service.findCalls)
			}
			if tt.wantStatus == http.StatusInternalServerError &&
				strings.Contains(recorder.Body.String(), "private database failure") {
				t.Error("internal service error leaked to response")
			}
		})
	}
}

func TestPublicSaleHandler_GetCanceledContextDoesNotCallServiceOrWriteResponse(t *testing.T) {
	t.Parallel()

	saleID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	service := &publicSaleServiceStub{}
	handler := newPublicSaleHandlerForTest(t, service)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	request := httptest.NewRequestWithContext(
		ctx, http.MethodGet, "/v1/sales/"+saleID.String(), nil,
	)
	request.SetPathValue("saleId", saleID.String())

	recorder := servePublicSaleGet(t, handler, request)

	if service.findCalls != 0 {
		t.Errorf("FindActiveSale() calls = %d, want 0", service.findCalls)
	}
	if recorder.Body.Len() != 0 {
		t.Errorf("body = %q, want empty response", recorder.Body.String())
	}
}

func TestPublicSaleHandler_GetServiceCancellationDoesNotWriteResponse(t *testing.T) {
	t.Parallel()

	saleID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	service := &publicSaleServiceStub{
		find: func(_ context.Context, _ uuid.UUID) (flashsale.Sale, error) {
			return flashsale.Sale{}, fmt.Errorf("find active sale: %w", context.Canceled)
		},
	}
	handler := newPublicSaleHandlerForTest(t, service)
	request := httptest.NewRequestWithContext(
		t.Context(), http.MethodGet, "/v1/sales/"+saleID.String(), nil,
	)
	request.SetPathValue("saleId", saleID.String())

	recorder := servePublicSaleGet(t, handler, request)

	if service.findCalls != 1 {
		t.Errorf("FindActiveSale() calls = %d, want 1", service.findCalls)
	}
	if recorder.Body.Len() != 0 {
		t.Errorf("body = %q, want empty response", recorder.Body.String())
	}
}
