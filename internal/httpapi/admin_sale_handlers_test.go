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

type adminSaleServiceStub struct {
	create  func(context.Context, flashsale.Sale) error
	addItem func(
		context.Context,
		uuid.UUID,
		uuid.UUID,
		uuid.UUID,
		string,
		flashsale.Money,
		int,
	) error
	activate func(
		context.Context,
		uuid.UUID,
		time.Time,
	) (flashsale.Sale, error)
	end func(
		context.Context,
		uuid.UUID,
	) (flashsale.Sale, error)

	createCalls    int
	gotSale        flashsale.Sale
	addItemCalls   int
	activateCalls  int
	endCalls       int
	gotSaleID      uuid.UUID
	gotItemID      uuid.UUID
	gotProductID   uuid.UUID
	gotName        string
	gotPrice       flashsale.Money
	gotTotalQty    int
	gotActivatedAt time.Time
}

func (s *adminSaleServiceStub) CreateSale(
	ctx context.Context,
	sale flashsale.Sale,
) error {
	s.createCalls++
	s.gotSale = sale

	if s.create == nil {
		return errors.New("unexpected CreateSale call")
	}

	return s.create(ctx, sale)
}

func (s *adminSaleServiceStub) AddSaleItem(
	ctx context.Context,
	saleID, itemID, productID uuid.UUID,
	name string,
	price flashsale.Money,
	totalQty int,
) error {
	s.addItemCalls++
	s.gotSaleID = saleID
	s.gotItemID = itemID
	s.gotProductID = productID
	s.gotName = name
	s.gotPrice = price
	s.gotTotalQty = totalQty

	if s.addItem == nil {
		return errors.New("unexpected AddSaleItem call")
	}

	return s.addItem(ctx, saleID, itemID, productID, name, price, totalQty)
}

func (s *adminSaleServiceStub) ActivateSale(
	ctx context.Context,
	saleID uuid.UUID,
	now time.Time,
) (flashsale.Sale, error) {
	s.activateCalls++
	s.gotSaleID = saleID
	s.gotActivatedAt = now

	if s.activate == nil {
		return flashsale.Sale{}, errors.New("unexpected ActivateSale call")
	}

	return s.activate(ctx, saleID, now)
}

func (s *adminSaleServiceStub) EndSale(
	ctx context.Context,
	saleID uuid.UUID,
) (flashsale.Sale, error) {
	s.endCalls++
	s.gotSaleID = saleID

	if s.end == nil {
		return flashsale.Sale{}, errors.New("unexpected EndSale call")
	}

	return s.end(ctx, saleID)
}

func newAdminSaleHandlerForTest(
	t *testing.T,
	service adminSaleService,
	newID func() uuid.UUID,
	now func() time.Time,
) *AdminSaleHandler {
	t.Helper()

	handler, err := NewAdminSaleHandler(
		service,
		slog.New(slog.DiscardHandler),
		newID,
		now,
	)
	if err != nil {
		t.Fatalf("NewAdminSaleHandler() error = %v, want nil", err)
	}

	return handler
}

func adminCreateRequest(
	t *testing.T,
	method string,
	body string,
) *http.Request {
	t.Helper()

	request := httptest.NewRequestWithContext(
		t.Context(), method, "/v1/admin/sales", strings.NewReader(body),
	)
	request.Header.Set("Content-Type", "application/json")

	return request
}

func serveAdminCreate(
	t *testing.T,
	handler *AdminSaleHandler,
	request *http.Request,
) *httptest.ResponseRecorder {
	t.Helper()

	recorder := httptest.NewRecorder()
	requestIDMiddleware(t)(http.HandlerFunc(handler.Create)).ServeHTTP(recorder, request)

	return recorder
}

func adminAddItemRequest(
	t *testing.T,
	method string,
	saleID string,
	body string,
) *http.Request {
	t.Helper()

	request := httptest.NewRequestWithContext(
		t.Context(),
		method,
		"/v1/admin/sales/"+saleID+"/items",
		strings.NewReader(body),
	)
	request.Header.Set("Content-Type", "application/json")
	request.SetPathValue("saleId", saleID)

	return request
}

func serveAdminAddItem(
	t *testing.T,
	handler *AdminSaleHandler,
	request *http.Request,
) *httptest.ResponseRecorder {
	t.Helper()

	recorder := httptest.NewRecorder()
	requestIDMiddleware(t)(http.HandlerFunc(handler.AddItem)).ServeHTTP(recorder, request)

	return recorder
}

func TestNewAdminSaleHandler_RejectsNilDependencies(t *testing.T) {
	t.Parallel()

	validIDGenerator := func() uuid.UUID { return uuid.New() }
	validClock := func() time.Time { return time.Now() }
	validLogger := slog.New(slog.DiscardHandler)
	validService := &adminSaleServiceStub{}

	tests := []struct {
		name    string
		service adminSaleService
		logger  *slog.Logger
		newID   func() uuid.UUID
		now     func() time.Time
		wantErr error
	}{
		{
			name:    "nil service",
			logger:  validLogger,
			newID:   validIDGenerator,
			now:     validClock,
			wantErr: ErrNilAdminSaleService,
		},
		{
			name:    "nil logger",
			service: validService,
			newID:   validIDGenerator,
			now:     validClock,
			wantErr: ErrNilLogger,
		},
		{
			name:    "nil id generator",
			service: validService,
			logger:  validLogger,
			now:     validClock,
			wantErr: ErrNilNewIDFunc,
		},
		{
			name:    "nil clock",
			service: validService,
			logger:  validLogger,
			newID:   validIDGenerator,
			wantErr: ErrNilNowFunc,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			handler, err := NewAdminSaleHandler(
				tt.service,
				tt.logger,
				tt.newID,
				tt.now,
			)

			if handler != nil {
				t.Errorf("NewAdminSaleHandler() handler = %#v, want nil", handler)
			}
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("NewAdminSaleHandler() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestAdminSaleHandler_CreateSuccess(t *testing.T) {
	t.Parallel()

	const body = `{"starts_at":"2026-09-16T10:00:00Z","ends_at":"2026-09-16T11:00:00Z"}`

	id := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	createdAt := time.Date(2026, time.September, 15, 9, 30, 0, 0, time.UTC)
	startsAt := time.Date(2026, time.September, 16, 10, 0, 0, 0, time.UTC)
	endsAt := startsAt.Add(time.Hour)
	wantSale, err := flashsale.NewSale(id, startsAt, endsAt, createdAt)
	if err != nil {
		t.Fatalf("flashsale.NewSale() error = %v", err)
	}

	type contextKey struct{}
	marker := &struct{}{}
	service := &adminSaleServiceStub{
		create: func(ctx context.Context, gotSale flashsale.Sale) error {
			if got := ctx.Value(contextKey{}); got != marker {
				t.Error("request context was not propagated to CreateSale")
			}
			if !reflect.DeepEqual(gotSale, wantSale) {
				t.Errorf("CreateSale() sale = %#v, want %#v", gotSale, wantSale)
			}

			return nil
		},
	}
	newIDCalls := 0
	nowCalls := 0
	handler := newAdminSaleHandlerForTest(
		t,
		service,
		func() uuid.UUID {
			newIDCalls++
			return id
		},
		func() time.Time {
			nowCalls++
			return createdAt
		},
	)
	request := adminCreateRequest(t, http.MethodPost, body)
	request = request.WithContext(context.WithValue(request.Context(), contextKey{}, marker))

	recorder := serveAdminCreate(t, handler, request)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body = %s", recorder.Code, http.StatusCreated, recorder.Body)
	}
	if got := recorder.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}
	if service.createCalls != 1 {
		t.Fatalf("CreateSale() calls = %d, want 1", service.createCalls)
	}
	if newIDCalls != 1 || nowCalls != 1 {
		t.Errorf("generator calls = (%d, %d), want (1, 1)", newIDCalls, nowCalls)
	}

	var got saleResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &got); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if want := saleToResponse(wantSale); !reflect.DeepEqual(got, want) {
		t.Errorf("response = %#v, want %#v", got, want)
	}
}

func TestAdminSaleHandler_CreateRejectsWrongMethod(t *testing.T) {
	t.Parallel()

	service := &adminSaleServiceStub{}
	handler := newAdminSaleHandlerForTest(t, service, uuid.New, time.Now)

	recorder := serveAdminCreate(
		t,
		handler,
		adminCreateRequest(t, http.MethodGet, ""),
	)

	assertErrorResponse(t, recorder, http.StatusMethodNotAllowed, string(codeMethodNotAllowed))
	if got := recorder.Header().Get("Allow"); got != http.MethodPost {
		t.Errorf("Allow = %q, want %q", got, http.MethodPost)
	}
	if service.createCalls != 0 {
		t.Errorf("CreateSale() calls = %d, want 0", service.createCalls)
	}
}

func TestAdminSaleHandler_CreateRejectsInvalidRequestWithoutCallingService(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		body        string
		contentType string
	}{
		{name: "empty body", body: "", contentType: "application/json"},
		{name: "malformed JSON", body: `{"starts_at":`, contentType: "application/json"},
		{name: "unknown field", body: `{"starts_at":"2026-09-16T10:00:00Z","ends_at":"2026-09-16T11:00:00Z","state":"active"}`, contentType: "application/json"},
		{name: "multiple JSON values", body: `{"starts_at":"2026-09-16T10:00:00Z","ends_at":"2026-09-16T11:00:00Z"} {}`, contentType: "application/json"},
		{name: "invalid timestamp", body: `{"starts_at":"tomorrow","ends_at":"2026-09-16T11:00:00Z"}`, contentType: "application/json"},
		{name: "missing starts at", body: `{"ends_at":"2026-09-16T11:00:00Z"}`, contentType: "application/json"},
		{name: "missing ends at", body: `{"starts_at":"2026-09-16T10:00:00Z"}`, contentType: "application/json"},
		{name: "wrong content type", body: `{"starts_at":"2026-09-16T10:00:00Z","ends_at":"2026-09-16T11:00:00Z"}`, contentType: "text/plain"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			service := &adminSaleServiceStub{}
			newIDCalls := 0
			nowCalls := 0
			handler := newAdminSaleHandlerForTest(
				t,
				service,
				func() uuid.UUID {
					newIDCalls++
					return uuid.New()
				},
				func() time.Time {
					nowCalls++
					return time.Now()
				},
			)
			request := adminCreateRequest(t, http.MethodPost, tt.body)
			request.Header.Set("Content-Type", tt.contentType)

			recorder := serveAdminCreate(t, handler, request)

			assertErrorResponse(t, recorder, http.StatusBadRequest, string(codeBadRequest))
			if service.createCalls != 0 {
				t.Errorf("CreateSale() calls = %d, want 0", service.createCalls)
			}
			if newIDCalls != 0 || nowCalls != 0 {
				t.Errorf("generator calls = (%d, %d), want (0, 0)", newIDCalls, nowCalls)
			}
		})
	}
}

func TestAdminSaleHandler_CreateRejectsInvalidTimeWindowWithoutCallingService(t *testing.T) {
	t.Parallel()

	service := &adminSaleServiceStub{}
	handler := newAdminSaleHandlerForTest(t, service, uuid.New, time.Now)
	recorder := serveAdminCreate(
		t,
		handler,
		adminCreateRequest(
			t,
			http.MethodPost,
			`{"starts_at":"2026-09-16T11:00:00Z","ends_at":"2026-09-16T11:00:00Z"}`,
		),
	)

	assertErrorResponse(t, recorder, http.StatusBadRequest, string(codeBadRequest))
	if service.createCalls != 0 {
		t.Errorf("CreateSale() calls = %d, want 0", service.createCalls)
	}
}

func TestAdminSaleHandler_CreateMapsServiceErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		serviceErr error
		wantStatus int
		wantCode   errorCode
	}{
		{
			name:       "conflict",
			serviceErr: fmt.Errorf("duplicate sale: %w", flashsale.ErrConflict),
			wantStatus: http.StatusConflict,
			wantCode:   codeConflict,
		},
		{
			name:       "deadline exceeded",
			serviceErr: fmt.Errorf("insert sale: %w", context.DeadlineExceeded),
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

			service := &adminSaleServiceStub{
				create: func(context.Context, flashsale.Sale) error {
					return tt.serviceErr
				},
			}
			handler := newAdminSaleHandlerForTest(t, service, uuid.New, time.Now)
			recorder := serveAdminCreate(
				t,
				handler,
				adminCreateRequest(
					t,
					http.MethodPost,
					`{"starts_at":"2026-09-16T10:00:00Z","ends_at":"2026-09-16T11:00:00Z"}`,
				),
			)

			assertErrorResponse(t, recorder, tt.wantStatus, string(tt.wantCode))
			if service.createCalls != 1 {
				t.Errorf("CreateSale() calls = %d, want 1", service.createCalls)
			}
			if tt.wantStatus == http.StatusInternalServerError &&
				strings.Contains(recorder.Body.String(), "private database failure") {
				t.Error("internal service error leaked to response")
			}
		})
	}
}

func TestAdminSaleHandler_CreateCanceledContextDoesNotCallServiceOrWriteResponse(t *testing.T) {
	t.Parallel()

	service := &adminSaleServiceStub{}
	handler := newAdminSaleHandlerForTest(t, service, uuid.New, time.Now)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	request := adminCreateRequest(
		t,
		http.MethodPost,
		`{"starts_at":"2026-09-16T10:00:00Z","ends_at":"2026-09-16T11:00:00Z"}`,
	).WithContext(ctx)
	recorder := httptest.NewRecorder()
	state := ensureResponseState(recorder)

	handler.Create(state, request)

	if service.createCalls != 0 {
		t.Errorf("CreateSale() calls = %d, want 0", service.createCalls)
	}
	if state.Written() {
		t.Errorf("handler wrote response status %d", state.Status())
	}
	if recorder.Body.Len() != 0 {
		t.Errorf("handler wrote response body of %d bytes", recorder.Body.Len())
	}
}

func TestAdminSaleHandler_AddItemSuccess(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		price          string
		wantPriceMinor int64
		wantPrice      string
	}{
		{name: "two decimal places", price: "12.50", wantPriceMinor: 1250, wantPrice: "12.50"},
		{name: "whole units", price: "12", wantPriceMinor: 1200, wantPrice: "12.00"},
		{name: "one decimal place", price: "12.5", wantPriceMinor: 1250, wantPrice: "12.50"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testAdminSaleHandlerAddItemSuccess(t, tt.price, tt.wantPriceMinor, tt.wantPrice)
		})
	}
}

func testAdminSaleHandlerAddItemSuccess(
	t *testing.T,
	price string,
	wantPriceMinor int64,
	wantPrice string,
) {
	t.Helper()

	saleID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	itemID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	productID := uuid.MustParse("33333333-3333-3333-3333-333333333333")

	type contextKey struct{}
	marker := &struct{}{}
	wantArguments := addSaleItemArgumentExpectation{
		contextKey:    contextKey{},
		contextValue:  marker,
		saleID:        saleID,
		itemID:        itemID,
		productID:     productID,
		name:          "Mechanical keyboard",
		priceMinor:    wantPriceMinor,
		totalQuantity: 10,
	}
	service := &adminSaleServiceStub{
		addItem: func(
			ctx context.Context,
			gotSaleID, gotItemID, gotProductID uuid.UUID,
			gotName string,
			gotPrice flashsale.Money,
			gotTotalQty int,
		) error {
			assertAddSaleItemArguments(t, addSaleItemArguments{
				context:       ctx,
				saleID:        gotSaleID,
				itemID:        gotItemID,
				productID:     gotProductID,
				name:          gotName,
				price:         gotPrice,
				totalQuantity: gotTotalQty,
			}, wantArguments)

			return nil
		},
	}
	newIDCalls := 0
	nowCalls := 0
	handler := newAdminSaleHandlerForTest(
		t,
		service,
		func() uuid.UUID {
			newIDCalls++
			return itemID
		},
		func() time.Time {
			nowCalls++
			return time.Now()
		},
	)
	body := fmt.Sprintf(
		`{"product_id":"%s","name":"Mechanical keyboard","price":"%s","total_quantity":10}`,
		productID,
		price,
	)
	request := adminAddItemRequest(t, http.MethodPost, saleID.String(), body)
	request = request.WithContext(context.WithValue(request.Context(), contextKey{}, marker))

	recorder := serveAdminAddItem(t, handler, request)
	assertAddSaleItemSuccessResponse(
		t,
		recorder,
		service,
		newIDCalls,
		nowCalls,
		saleID,
		itemID,
		productID,
		wantPrice,
	)
}

type addSaleItemArguments struct {
	context       context.Context
	saleID        uuid.UUID
	itemID        uuid.UUID
	productID     uuid.UUID
	name          string
	price         flashsale.Money
	totalQuantity int
}

type addSaleItemArgumentExpectation struct {
	contextKey    any
	contextValue  any
	saleID        uuid.UUID
	itemID        uuid.UUID
	productID     uuid.UUID
	name          string
	priceMinor    int64
	totalQuantity int
}

func assertAddSaleItemArguments(
	t *testing.T,
	got addSaleItemArguments,
	want addSaleItemArgumentExpectation,
) {
	t.Helper()

	if gotContextValue := got.context.Value(want.contextKey); gotContextValue != want.contextValue {
		t.Error("request context was not propagated to AddSaleItem")
	}
	if got.saleID != want.saleID || got.itemID != want.itemID || got.productID != want.productID {
		t.Errorf(
			"AddSaleItem() IDs = (%s, %s, %s), want (%s, %s, %s)",
			got.saleID, got.itemID, got.productID,
			want.saleID, want.itemID, want.productID,
		)
	}
	if got.name != want.name {
		t.Errorf("AddSaleItem() name = %q, want %q", got.name, want.name)
	}
	if gotPriceMinor := got.price.AmountMinor(); gotPriceMinor != want.priceMinor {
		t.Errorf("AddSaleItem() price minor = %d, want %d", gotPriceMinor, want.priceMinor)
	}
	if got.totalQuantity != want.totalQuantity {
		t.Errorf("AddSaleItem() quantity = %d, want %d", got.totalQuantity, want.totalQuantity)
	}
}

func assertAddSaleItemSuccessResponse(
	t *testing.T,
	recorder *httptest.ResponseRecorder,
	service *adminSaleServiceStub,
	newIDCalls, nowCalls int,
	saleID, itemID, productID uuid.UUID,
	wantPrice string,
) {
	t.Helper()

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body = %s", recorder.Code, http.StatusCreated, recorder.Body)
	}
	if got := recorder.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}
	if service.addItemCalls != 1 {
		t.Fatalf("AddSaleItem() calls = %d, want 1", service.addItemCalls)
	}
	if newIDCalls != 1 {
		t.Errorf("new ID calls = %d, want 1", newIDCalls)
	}
	if nowCalls != 0 {
		t.Errorf("clock calls = %d, want 0", nowCalls)
	}

	var got saleItemResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &got); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	want := saleItemResponse{
		ID:                itemID,
		SaleID:            saleID,
		ProductID:         productID,
		Name:              "Mechanical keyboard",
		Price:             wantPrice,
		TotalQuantity:     10,
		ReservedQuantity:  0,
		SoldQuantity:      0,
		AvailableQuantity: 10,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("response = %#v, want %#v", got, want)
	}
}

func TestAdminSaleHandler_AddItemRejectsWrongMethod(t *testing.T) {
	t.Parallel()

	service := &adminSaleServiceStub{}
	newIDCalls := 0
	handler := newAdminSaleHandlerForTest(
		t,
		service,
		func() uuid.UUID {
			newIDCalls++
			return uuid.New()
		},
		time.Now,
	)
	recorder := serveAdminAddItem(
		t,
		handler,
		adminAddItemRequest(t, http.MethodGet, uuid.NewString(), ""),
	)

	assertErrorResponse(t, recorder, http.StatusMethodNotAllowed, string(codeMethodNotAllowed))
	if got := recorder.Header().Get("Allow"); got != http.MethodPost {
		t.Errorf("Allow = %q, want %q", got, http.MethodPost)
	}
	if service.addItemCalls != 0 {
		t.Errorf("AddSaleItem() calls = %d, want 0", service.addItemCalls)
	}
	if newIDCalls != 0 {
		t.Errorf("new ID calls = %d, want 0", newIDCalls)
	}
}

func TestAdminSaleHandler_AddItemRejectsInvalidSaleIDWithoutCallingService(t *testing.T) {
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

			service := &adminSaleServiceStub{}
			newIDCalls := 0
			handler := newAdminSaleHandlerForTest(
				t,
				service,
				func() uuid.UUID {
					newIDCalls++
					return uuid.New()
				},
				time.Now,
			)
			recorder := serveAdminAddItem(
				t,
				handler,
				adminAddItemRequest(
					t,
					http.MethodPost,
					tt.saleID,
					`{"product_id":"33333333-3333-3333-3333-333333333333","name":"Keyboard","price":"12.50","total_quantity":1}`,
				),
			)

			assertErrorResponse(t, recorder, http.StatusBadRequest, string(codeBadRequest))
			if service.addItemCalls != 0 {
				t.Errorf("AddSaleItem() calls = %d, want 0", service.addItemCalls)
			}
			if newIDCalls != 0 {
				t.Errorf("new ID calls = %d, want 0", newIDCalls)
			}
		})
	}
}

func TestAdminSaleHandler_AddItemRejectsInvalidRequestWithoutCallingService(t *testing.T) {
	t.Parallel()

	validSaleID := "11111111-1111-1111-1111-111111111111"
	validBody := `{"product_id":"33333333-3333-3333-3333-333333333333","name":"Keyboard","price":"12.50","total_quantity":1}`
	tests := []struct {
		name        string
		body        string
		contentType string
	}{
		{name: "empty body", body: "", contentType: "application/json"},
		{name: "malformed JSON", body: `{"product_id":`, contentType: "application/json"},
		{name: "unknown field", body: validBody[:len(validBody)-1] + `,"state":"active"}`, contentType: "application/json"},
		{name: "multiple JSON values", body: validBody + " {}", contentType: "application/json"},
		{name: "missing product ID", body: `{"name":"Keyboard","price":"12.50","total_quantity":1}`, contentType: "application/json"},
		{name: "nil product ID", body: `{"product_id":"00000000-0000-0000-0000-000000000000","name":"Keyboard","price":"12.50","total_quantity":1}`, contentType: "application/json"},
		{name: "missing name", body: `{"product_id":"33333333-3333-3333-3333-333333333333","price":"12.50","total_quantity":1}`, contentType: "application/json"},
		{name: "null name", body: `{"product_id":"33333333-3333-3333-3333-333333333333","name":null,"price":"12.50","total_quantity":1}`, contentType: "application/json"},
		{name: "missing price", body: `{"product_id":"33333333-3333-3333-3333-333333333333","name":"Keyboard","total_quantity":1}`, contentType: "application/json"},
		{name: "price is JSON number", body: `{"product_id":"33333333-3333-3333-3333-333333333333","name":"Keyboard","price":12.50,"total_quantity":1}`, contentType: "application/json"},
		{name: "negative price", body: `{"product_id":"33333333-3333-3333-3333-333333333333","name":"Keyboard","price":"-12.50","total_quantity":1}`, contentType: "application/json"},
		{name: "zero price", body: `{"product_id":"33333333-3333-3333-3333-333333333333","name":"Keyboard","price":"0","total_quantity":1}`, contentType: "application/json"},
		{name: "too many price decimals", body: `{"product_id":"33333333-3333-3333-3333-333333333333","name":"Keyboard","price":"12.500","total_quantity":1}`, contentType: "application/json"},
		{name: "zero quantity", body: `{"product_id":"33333333-3333-3333-3333-333333333333","name":"Keyboard","price":"12.50","total_quantity":0}`, contentType: "application/json"},
		{name: "negative quantity", body: `{"product_id":"33333333-3333-3333-3333-333333333333","name":"Keyboard","price":"12.50","total_quantity":-1}`, contentType: "application/json"},
		{name: "quantity above contract maximum", body: `{"product_id":"33333333-3333-3333-3333-333333333333","name":"Keyboard","price":"12.50","total_quantity":2147483648}`, contentType: "application/json"},
		{name: "wrong content type", body: validBody, contentType: "text/plain"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			service := &adminSaleServiceStub{}
			newIDCalls := 0
			handler := newAdminSaleHandlerForTest(
				t,
				service,
				func() uuid.UUID {
					newIDCalls++
					return uuid.New()
				},
				time.Now,
			)
			request := adminAddItemRequest(t, http.MethodPost, validSaleID, tt.body)
			request.Header.Set("Content-Type", tt.contentType)

			recorder := serveAdminAddItem(t, handler, request)

			assertErrorResponse(t, recorder, http.StatusBadRequest, string(codeBadRequest))
			if service.addItemCalls != 0 {
				t.Errorf("AddSaleItem() calls = %d, want 0", service.addItemCalls)
			}
			if newIDCalls != 0 {
				t.Errorf("new ID calls = %d, want 0", newIDCalls)
			}
		})
	}
}

func TestAdminSaleHandler_AddItemMapsServiceErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		serviceErr error
		wantStatus int
		wantCode   errorCode
	}{
		{
			name:       "deadline exceeded",
			serviceErr: fmt.Errorf("insert sale item: %w", context.DeadlineExceeded),
			wantStatus: http.StatusGatewayTimeout,
			wantCode:   codeDeadlineExceeded,
		},
		{
			name:       "sale not found",
			serviceErr: fmt.Errorf("find sale: %w", flashsale.ErrSaleNotFound),
			wantStatus: http.StatusNotFound,
			wantCode:   codeNotFound,
		},
		{
			name:       "sale is not draft",
			serviceErr: fmt.Errorf("add item: %w", flashsale.ErrForbiddenTransition),
			wantStatus: http.StatusConflict,
			wantCode:   codeConflict,
		},
		{
			name:       "duplicate item",
			serviceErr: fmt.Errorf("add item: %w", flashsale.ErrDuplicateSaleItem),
			wantStatus: http.StatusConflict,
			wantCode:   codeConflict,
		},
		{
			name:       "storage conflict",
			serviceErr: fmt.Errorf("insert item: %w", flashsale.ErrConflict),
			wantStatus: http.StatusConflict,
			wantCode:   codeConflict,
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

			service := &adminSaleServiceStub{
				addItem: func(
					context.Context,
					uuid.UUID,
					uuid.UUID,
					uuid.UUID,
					string,
					flashsale.Money,
					int,
				) error {
					return tt.serviceErr
				},
			}
			newIDCalls := 0
			handler := newAdminSaleHandlerForTest(
				t,
				service,
				func() uuid.UUID {
					newIDCalls++
					return uuid.New()
				},
				time.Now,
			)
			recorder := serveAdminAddItem(
				t,
				handler,
				adminAddItemRequest(
					t,
					http.MethodPost,
					"11111111-1111-1111-1111-111111111111",
					`{"product_id":"33333333-3333-3333-3333-333333333333","name":"Keyboard","price":"12.50","total_quantity":1}`,
				),
			)

			assertErrorResponse(t, recorder, tt.wantStatus, string(tt.wantCode))
			if service.addItemCalls != 1 {
				t.Errorf("AddSaleItem() calls = %d, want 1", service.addItemCalls)
			}
			if newIDCalls != 1 {
				t.Errorf("new ID calls = %d, want 1", newIDCalls)
			}
			if tt.wantStatus == http.StatusInternalServerError &&
				strings.Contains(recorder.Body.String(), "private database failure") {
				t.Error("internal service error leaked to response")
			}
		})
	}
}

func TestAdminSaleHandler_AddItemCanceledContextDoesNotCallServiceOrWriteResponse(t *testing.T) {
	t.Parallel()

	service := &adminSaleServiceStub{}
	newIDCalls := 0
	handler := newAdminSaleHandlerForTest(
		t,
		service,
		func() uuid.UUID {
			newIDCalls++
			return uuid.New()
		},
		time.Now,
	)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	request := adminAddItemRequest(
		t,
		http.MethodPost,
		"11111111-1111-1111-1111-111111111111",
		`{"product_id":"33333333-3333-3333-3333-333333333333","name":"Keyboard","price":"12.50","total_quantity":1}`,
	).WithContext(ctx)
	recorder := httptest.NewRecorder()
	state := ensureResponseState(recorder)

	handler.AddItem(state, request)

	if service.addItemCalls != 0 {
		t.Errorf("AddSaleItem() calls = %d, want 0", service.addItemCalls)
	}
	if newIDCalls != 0 {
		t.Errorf("new ID calls = %d, want 0", newIDCalls)
	}
	if state.Written() {
		t.Errorf("handler wrote response status %d", state.Status())
	}
	if recorder.Body.Len() != 0 {
		t.Errorf("handler wrote response body of %d bytes", recorder.Body.Len())
	}
}

func TestAdminSaleHandler_AddItemServiceCancellationDoesNotWriteResponse(t *testing.T) {
	t.Parallel()

	service := &adminSaleServiceStub{
		addItem: func(
			context.Context,
			uuid.UUID,
			uuid.UUID,
			uuid.UUID,
			string,
			flashsale.Money,
			int,
		) error {
			return fmt.Errorf("insert sale item: %w", context.Canceled)
		},
	}
	handler := newAdminSaleHandlerForTest(t, service, uuid.New, time.Now)
	recorder := serveAdminAddItem(
		t,
		handler,
		adminAddItemRequest(
			t,
			http.MethodPost,
			"11111111-1111-1111-1111-111111111111",
			`{"product_id":"33333333-3333-3333-3333-333333333333","name":"Keyboard","price":"12.50","total_quantity":1}`,
		),
	)

	if service.addItemCalls != 1 {
		t.Errorf("AddSaleItem() calls = %d, want 1", service.addItemCalls)
	}
	if recorder.Body.Len() != 0 {
		t.Errorf("body = %q, want empty response", recorder.Body.String())
	}
}

func adminLifecycleRequest(
	t *testing.T,
	method string,
	action string,
	saleID string,
) *http.Request {
	t.Helper()

	request := httptest.NewRequestWithContext(
		t.Context(),
		method,
		"/v1/admin/sales/"+saleID+"/"+action,
		nil,
	)
	request.SetPathValue("saleId", saleID)

	return request
}

func serveAdminLifecycle(
	t *testing.T,
	handler *AdminSaleHandler,
	action string,
	request *http.Request,
) *httptest.ResponseRecorder {
	t.Helper()

	endpoint := adminLifecycleEndpoint(t, handler, action)
	recorder := httptest.NewRecorder()
	requestIDMiddleware(t)(endpoint).ServeHTTP(recorder, request)

	return recorder
}

func adminLifecycleEndpoint(
	t *testing.T,
	handler *AdminSaleHandler,
	action string,
) http.HandlerFunc {
	t.Helper()

	var endpoint http.HandlerFunc
	switch action {
	case "activate":
		endpoint = handler.Activate
	case "end":
		endpoint = handler.End
	default:
		t.Fatalf("unknown lifecycle action %q", action)
	}

	return endpoint
}

func lifecycleSale(t *testing.T, state flashsale.SaleState) flashsale.Sale {
	t.Helper()

	saleID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	startsAt := time.Date(2026, time.September, 17, 10, 0, 0, 0, time.UTC)
	sale, err := flashsale.RehydrateSale(flashsale.SaleSnapshot{
		ID:        saleID,
		State:     state,
		StartsAt:  startsAt,
		EndsAt:    startsAt.Add(time.Hour),
		CreatedAt: startsAt.Add(-time.Hour),
		Items: []flashsale.SaleItemSnapshot{
			{
				ID:         uuid.MustParse("22222222-2222-2222-2222-222222222222"),
				SaleID:     saleID,
				ProductID:  uuid.MustParse("33333333-3333-3333-3333-333333333333"),
				Name:       "Mechanical keyboard",
				PriceMinor: 1250,
				TotalQty:   10,
			},
		},
	})
	if err != nil {
		t.Fatalf("RehydrateSale() error = %v, want nil", err)
	}

	return sale
}

func TestAdminSaleHandler_ActivateSuccess(t *testing.T) {
	t.Parallel()

	saleID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	activatedAt := time.Date(2026, time.September, 17, 10, 15, 0, 0, time.UTC)
	wantSale := lifecycleSale(t, flashsale.ActiveState)
	type contextKey struct{}
	marker := &struct{}{}
	service := &adminSaleServiceStub{
		activate: func(
			ctx context.Context,
			gotSaleID uuid.UUID,
			gotNow time.Time,
		) (flashsale.Sale, error) {
			if got := ctx.Value(contextKey{}); got != marker {
				t.Error("request context was not propagated to ActivateSale")
			}
			if gotSaleID != saleID {
				t.Errorf("ActivateSale() sale ID = %s, want %s", gotSaleID, saleID)
			}
			if !gotNow.Equal(activatedAt) {
				t.Errorf("ActivateSale() time = %s, want %s", gotNow, activatedAt)
			}

			return wantSale, nil
		},
	}
	newIDCalls := 0
	nowCalls := 0
	handler := newAdminSaleHandlerForTest(
		t,
		service,
		func() uuid.UUID {
			newIDCalls++
			return uuid.New()
		},
		func() time.Time {
			nowCalls++
			return activatedAt
		},
	)
	request := adminLifecycleRequest(t, http.MethodPost, "activate", saleID.String())
	request = request.WithContext(context.WithValue(request.Context(), contextKey{}, marker))

	recorder := serveAdminLifecycle(t, handler, "activate", request)

	assertLifecycleSuccessResponse(t, recorder, wantSale)
	if service.activateCalls != 1 {
		t.Errorf("ActivateSale() calls = %d, want 1", service.activateCalls)
	}
	if nowCalls != 1 {
		t.Errorf("clock calls = %d, want 1", nowCalls)
	}
	if newIDCalls != 0 {
		t.Errorf("new ID calls = %d, want 0", newIDCalls)
	}
}

func TestAdminSaleHandler_EndSuccess(t *testing.T) {
	t.Parallel()

	saleID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	wantSale := lifecycleSale(t, flashsale.EndedState)
	type contextKey struct{}
	marker := &struct{}{}
	service := &adminSaleServiceStub{
		end: func(ctx context.Context, gotSaleID uuid.UUID) (flashsale.Sale, error) {
			if got := ctx.Value(contextKey{}); got != marker {
				t.Error("request context was not propagated to EndSale")
			}
			if gotSaleID != saleID {
				t.Errorf("EndSale() sale ID = %s, want %s", gotSaleID, saleID)
			}

			return wantSale, nil
		},
	}
	newIDCalls := 0
	nowCalls := 0
	handler := newAdminSaleHandlerForTest(
		t,
		service,
		func() uuid.UUID {
			newIDCalls++
			return uuid.New()
		},
		func() time.Time {
			nowCalls++
			return time.Now()
		},
	)
	request := adminLifecycleRequest(t, http.MethodPost, "end", saleID.String())
	request = request.WithContext(context.WithValue(request.Context(), contextKey{}, marker))

	recorder := serveAdminLifecycle(t, handler, "end", request)

	assertLifecycleSuccessResponse(t, recorder, wantSale)
	if service.endCalls != 1 {
		t.Errorf("EndSale() calls = %d, want 1", service.endCalls)
	}
	if nowCalls != 0 {
		t.Errorf("clock calls = %d, want 0", nowCalls)
	}
	if newIDCalls != 0 {
		t.Errorf("new ID calls = %d, want 0", newIDCalls)
	}
}

func assertLifecycleSuccessResponse(
	t *testing.T,
	recorder *httptest.ResponseRecorder,
	wantSale flashsale.Sale,
) {
	t.Helper()

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", recorder.Code, http.StatusOK, recorder.Body)
	}
	if got := recorder.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}

	var got saleResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &got); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if want := saleToResponse(wantSale); !reflect.DeepEqual(got, want) {
		t.Errorf("response = %#v, want %#v", got, want)
	}
}

func TestAdminSaleHandler_LifecycleRejectsWrongMethod(t *testing.T) {
	t.Parallel()

	for _, action := range []string{"activate", "end"} {
		t.Run(action, func(t *testing.T) {
			t.Parallel()

			service := &adminSaleServiceStub{}
			nowCalls := 0
			handler := newAdminSaleHandlerForTest(
				t,
				service,
				uuid.New,
				func() time.Time {
					nowCalls++
					return time.Now()
				},
			)
			recorder := serveAdminLifecycle(
				t,
				handler,
				action,
				adminLifecycleRequest(t, http.MethodGet, action, uuid.NewString()),
			)

			assertErrorResponse(t, recorder, http.StatusMethodNotAllowed, string(codeMethodNotAllowed))
			if got := recorder.Header().Get("Allow"); got != http.MethodPost {
				t.Errorf("Allow = %q, want %q", got, http.MethodPost)
			}
			if service.activateCalls != 0 || service.endCalls != 0 {
				t.Errorf(
					"service calls = (%d, %d), want (0, 0)",
					service.activateCalls,
					service.endCalls,
				)
			}
			if nowCalls != 0 {
				t.Errorf("clock calls = %d, want 0", nowCalls)
			}
		})
	}
}

func TestAdminSaleHandler_LifecycleRejectsInvalidSaleID(t *testing.T) {
	t.Parallel()

	invalidIDs := []struct {
		name string
		id   string
	}{
		{name: "missing", id: ""},
		{name: "malformed", id: "not-a-uuid"},
		{name: "nil UUID", id: uuid.Nil.String()},
	}

	for _, action := range []string{"activate", "end"} {
		for _, invalidID := range invalidIDs {
			t.Run(action+"/"+invalidID.name, func(t *testing.T) {
				t.Parallel()

				service := &adminSaleServiceStub{}
				nowCalls := 0
				handler := newAdminSaleHandlerForTest(
					t,
					service,
					uuid.New,
					func() time.Time {
						nowCalls++
						return time.Now()
					},
				)
				recorder := serveAdminLifecycle(
					t,
					handler,
					action,
					adminLifecycleRequest(t, http.MethodPost, action, invalidID.id),
				)

				assertErrorResponse(t, recorder, http.StatusBadRequest, string(codeBadRequest))
				if service.activateCalls != 0 || service.endCalls != 0 {
					t.Errorf(
						"service calls = (%d, %d), want (0, 0)",
						service.activateCalls,
						service.endCalls,
					)
				}
				if nowCalls != 0 {
					t.Errorf("clock calls = %d, want 0", nowCalls)
				}
			})
		}
	}
}

type lifecycleErrorTestCase struct {
	name       string
	serviceErr error
	wantStatus int
	wantCode   errorCode
}

func TestAdminSaleHandler_LifecycleMapsServiceErrors(t *testing.T) {
	t.Parallel()

	tests := []lifecycleErrorTestCase{
		{
			name:       "deadline exceeded",
			serviceErr: fmt.Errorf("lifecycle: %w", context.DeadlineExceeded),
			wantStatus: http.StatusGatewayTimeout,
			wantCode:   codeDeadlineExceeded,
		},
		{
			name:       "sale not found",
			serviceErr: fmt.Errorf("lifecycle: %w", flashsale.ErrSaleNotFound),
			wantStatus: http.StatusNotFound,
			wantCode:   codeNotFound,
		},
		{
			name:       "forbidden transition",
			serviceErr: fmt.Errorf("lifecycle: %w", flashsale.ErrForbiddenTransition),
			wantStatus: http.StatusConflict,
			wantCode:   codeConflict,
		},
		{
			name:       "expired time window",
			serviceErr: fmt.Errorf("lifecycle: %w", flashsale.ErrExpiredTimeWindow),
			wantStatus: http.StatusConflict,
			wantCode:   codeConflict,
		},
		{
			name:       "invalid configuration",
			serviceErr: fmt.Errorf("lifecycle: %w", flashsale.ErrInvalidConfiguration),
			wantStatus: http.StatusConflict,
			wantCode:   codeConflict,
		},
		{
			name:       "storage conflict",
			serviceErr: fmt.Errorf("lifecycle: %w", flashsale.ErrConflict),
			wantStatus: http.StatusConflict,
			wantCode:   codeConflict,
		},
		{
			name:       "internal failure",
			serviceErr: errors.New("private database failure"),
			wantStatus: http.StatusInternalServerError,
			wantCode:   codeInternalError,
		},
	}

	for _, action := range []string{"activate", "end"} {
		for _, tt := range tests {
			t.Run(action+"/"+tt.name, func(t *testing.T) {
				t.Parallel()

				testLifecycleErrorMapping(t, action, tt)
			})
		}
	}
}

func testLifecycleErrorMapping(
	t *testing.T,
	action string,
	tt lifecycleErrorTestCase,
) {
	t.Helper()

	service := &adminSaleServiceStub{
		activate: func(
			context.Context,
			uuid.UUID,
			time.Time,
		) (flashsale.Sale, error) {
			return flashsale.Sale{}, tt.serviceErr
		},
		end: func(context.Context, uuid.UUID) (flashsale.Sale, error) {
			return flashsale.Sale{}, tt.serviceErr
		},
	}
	handler := newAdminSaleHandlerForTest(t, service, uuid.New, time.Now)
	recorder := serveAdminLifecycle(
		t,
		handler,
		action,
		adminLifecycleRequest(t, http.MethodPost, action, uuid.NewString()),
	)

	assertErrorResponse(t, recorder, tt.wantStatus, string(tt.wantCode))
	if action == "activate" && service.activateCalls != 1 {
		t.Errorf("ActivateSale() calls = %d, want 1", service.activateCalls)
	}
	if action == "end" && service.endCalls != 1 {
		t.Errorf("EndSale() calls = %d, want 1", service.endCalls)
	}
	if tt.wantStatus == http.StatusInternalServerError &&
		strings.Contains(recorder.Body.String(), "private database failure") {
		t.Error("internal service error leaked to response")
	}
}

func TestAdminSaleHandler_LifecycleCanceledContextDoesNotCallServiceOrWriteResponse(
	t *testing.T,
) {
	t.Parallel()

	for _, action := range []string{"activate", "end"} {
		t.Run(action, func(t *testing.T) {
			t.Parallel()

			testLifecycleCanceledContext(t, action)
		})
	}
}

func testLifecycleCanceledContext(t *testing.T, action string) {
	t.Helper()

	service := &adminSaleServiceStub{}
	nowCalls := 0
	handler := newAdminSaleHandlerForTest(
		t,
		service,
		uuid.New,
		func() time.Time {
			nowCalls++
			return time.Now()
		},
	)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	request := adminLifecycleRequest(
		t,
		http.MethodPost,
		action,
		uuid.NewString(),
	).WithContext(ctx)
	recorder := httptest.NewRecorder()
	state := ensureResponseState(recorder)

	if action == "activate" {
		handler.Activate(state, request)
	} else {
		handler.End(state, request)
	}

	if service.activateCalls != 0 || service.endCalls != 0 {
		t.Errorf(
			"service calls = (%d, %d), want (0, 0)",
			service.activateCalls,
			service.endCalls,
		)
	}
	if nowCalls != 0 {
		t.Errorf("clock calls = %d, want 0", nowCalls)
	}
	if state.Written() {
		t.Errorf("handler wrote response status %d", state.Status())
	}
	if recorder.Body.Len() != 0 {
		t.Errorf("handler wrote response body of %d bytes", recorder.Body.Len())
	}
}

func TestAdminSaleHandler_LifecycleServiceCancellationDoesNotWriteResponse(t *testing.T) {
	t.Parallel()

	for _, action := range []string{"activate", "end"} {
		t.Run(action, func(t *testing.T) {
			t.Parallel()

			service := &adminSaleServiceStub{
				activate: func(
					context.Context,
					uuid.UUID,
					time.Time,
				) (flashsale.Sale, error) {
					return flashsale.Sale{}, fmt.Errorf("activate: %w", context.Canceled)
				},
				end: func(context.Context, uuid.UUID) (flashsale.Sale, error) {
					return flashsale.Sale{}, fmt.Errorf("end: %w", context.Canceled)
				},
			}
			handler := newAdminSaleHandlerForTest(t, service, uuid.New, time.Now)
			request := adminLifecycleRequest(
				t,
				http.MethodPost,
				action,
				uuid.NewString(),
			)
			recorder := httptest.NewRecorder()
			state := ensureResponseState(recorder)

			adminLifecycleEndpoint(t, handler, action)(state, request)

			if action == "activate" && service.activateCalls != 1 {
				t.Errorf("ActivateSale() calls = %d, want 1", service.activateCalls)
			}
			if action == "end" && service.endCalls != 1 {
				t.Errorf("EndSale() calls = %d, want 1", service.endCalls)
			}
			if state.Written() {
				t.Errorf("handler wrote response status %d", state.Status())
			}
			if recorder.Body.Len() != 0 {
				t.Errorf("body = %q, want empty response", recorder.Body.String())
			}
		})
	}
}
