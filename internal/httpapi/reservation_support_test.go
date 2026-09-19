package httpapi

import (
	"context"
	"encoding/json"
	"errors"
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

type reservationServiceStub struct {
	reserve func(context.Context, flashsale.ReserveCommand, time.Time) (flashsale.ReserveResult, error)
	cancel  func(context.Context, uuid.UUID, uuid.UUID, time.Time) error
	pay     func(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, time.Time) (flashsale.Order, error)

	reserveCalls int
	gotCommand   flashsale.ReserveCommand
	gotNow       time.Time

	cancelCalls            int
	gotCancelUserID        uuid.UUID
	gotCancelReservationID uuid.UUID
	gotCancelNow           time.Time

	payCalls            int
	gotPayUserID        uuid.UUID
	gotPayReservationID uuid.UUID
	gotPayOrderID       uuid.UUID
	gotPayNow           time.Time
}

func (s *reservationServiceStub) Reserve(
	ctx context.Context,
	cmd flashsale.ReserveCommand,
	now time.Time,
) (flashsale.ReserveResult, error) {
	s.reserveCalls++
	s.gotCommand = cmd
	s.gotNow = now
	if s.reserve == nil {
		return flashsale.ReserveResult{}, errors.New("unexpected Reserve call")
	}

	return s.reserve(ctx, cmd, now)
}

func (s *reservationServiceStub) Cancel(
	ctx context.Context,
	userID uuid.UUID,
	reservationID uuid.UUID,
	now time.Time,
) error {
	s.cancelCalls++
	s.gotCancelUserID = userID
	s.gotCancelReservationID = reservationID
	s.gotCancelNow = now
	if s.cancel == nil {
		return errors.New("unexpected Cancel call")
	}

	return s.cancel(ctx, userID, reservationID, now)
}

func (s *reservationServiceStub) Pay(
	ctx context.Context,
	userID uuid.UUID,
	reservationID uuid.UUID,
	orderID uuid.UUID,
	now time.Time,
) (flashsale.Order, error) {
	s.payCalls++
	s.gotPayUserID = userID
	s.gotPayReservationID = reservationID
	s.gotPayOrderID = orderID
	s.gotPayNow = now
	if s.pay == nil {
		return flashsale.Order{}, errors.New("unexpected Pay call")
	}

	return s.pay(ctx, userID, reservationID, orderID, now)
}

type orderServiceStub struct{}

func (*orderServiceStub) FindOrder(
	context.Context,
	uuid.UUID,
	uuid.UUID,
) (flashsale.Order, error) {
	return flashsale.Order{}, errors.New("unexpected FindOrder call")
}

func TestNewReservationHandler_RejectsInvalidDependencies(t *testing.T) {
	service := &reservationServiceStub{}
	logger := slog.New(slog.DiscardHandler)
	newID := uuid.New
	now := time.Now

	tests := []struct {
		name    string
		service reservationService
		logger  *slog.Logger
		newID   func() uuid.UUID
		now     func() time.Time
		ttl     time.Duration
		wantErr error
	}{
		{"nil service", nil, logger, newID, now, time.Minute, ErrNilReservationService},
		{"nil logger", service, nil, newID, now, time.Minute, ErrNilLogger},
		{"nil ID generator", service, logger, nil, now, time.Minute, ErrNilNewIDFunc},
		{"nil clock", service, logger, newID, nil, time.Minute, ErrNilNowFunc},
		{"zero TTL", service, logger, newID, now, 0, ErrInvalidDuration},
		{"negative TTL", service, logger, newID, now, -time.Second, ErrInvalidDuration},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler, err := NewReservationHandler(tt.service, tt.logger, tt.newID, tt.now, tt.ttl)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("NewReservationHandler() error = %v, want %v", err, tt.wantErr)
			}
			if handler != nil {
				t.Fatalf("NewReservationHandler() handler = %#v, want nil", handler)
			}
		})
	}
}

func TestNewReservationHandler_AcceptsValidDependencies(t *testing.T) {
	handler, err := NewReservationHandler(
		&reservationServiceStub{},
		slog.New(slog.DiscardHandler),
		uuid.New,
		time.Now,
		15*time.Minute,
	)
	if err != nil {
		t.Fatalf("NewReservationHandler() error = %v", err)
	}
	if handler == nil {
		t.Fatal("NewReservationHandler() handler = nil")
	}
}

func TestNewOrderHandler_RejectsInvalidDependencies(t *testing.T) {
	logger := slog.New(slog.DiscardHandler)

	if handler, err := NewOrderHandler(nil, logger); !errors.Is(err, ErrNilOrderService) || handler != nil {
		t.Fatalf("NewOrderHandler(nil service) = (%#v, %v)", handler, err)
	}
	if handler, err := NewOrderHandler(&orderServiceStub{}, nil); !errors.Is(err, ErrNilLogger) || handler != nil {
		t.Fatalf("NewOrderHandler(nil logger) = (%#v, %v)", handler, err)
	}
}

func TestParseResourceIDs(t *testing.T) {
	id := uuid.MustParse("11111111-1111-1111-1111-111111111111")

	tests := []struct {
		name   string
		key    string
		value  string
		parse  func(*http.Request) (uuid.UUID, error)
		wantID uuid.UUID
		wantOK bool
	}{
		{"sale", saleIDPathName, id.String(), parseSaleID, id, true},
		{"reservation", reservationIDPathName, id.String(), parseReservationID, id, true},
		{"order", orderIDPathName, id.String(), parseOrderID, id, true},
		{"missing", reservationIDPathName, "", parseReservationID, uuid.Nil, false},
		{"malformed", reservationIDPathName, "not-a-uuid", parseReservationID, uuid.Nil, false},
		{"nil UUID", reservationIDPathName, uuid.Nil.String(), parseReservationID, uuid.Nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
			if tt.value != "" {
				request.SetPathValue(tt.key, tt.value)
			}

			got, err := tt.parse(request)
			if tt.wantOK && err != nil {
				t.Fatalf("parse() error = %v", err)
			}
			if !tt.wantOK && err == nil {
				t.Fatal("parse() error = nil, want non-nil")
			}
			if got != tt.wantID {
				t.Fatalf("parse() = %s, want %s", got, tt.wantID)
			}
		})
	}
}

func TestParseIdempotencyKey(t *testing.T) {
	maxKey := strings.Repeat("x", 255)
	tests := []struct {
		name    string
		values  []string
		want    string
		wantErr error
	}{
		{"missing", nil, "", ErrInvalidIdempotencyKey},
		{"empty", []string{""}, "", ErrInvalidIdempotencyKey},
		{"multiple", []string{"first", "second"}, "", ErrMultipleIdempotencyKeys},
		{"too long", []string{maxKey + "x"}, "", ErrInvalidIdempotencyKey},
		{"one byte", []string{"x"}, "x", nil},
		{"255 bytes", []string{maxKey}, maxKey, nil},
		{"opaque whitespace and case", []string{"  AbC  "}, "  AbC  ", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", nil)
			request.Header["Idempotency-Key"] = tt.values

			got, err := parseIdempotencyKey(request)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("parseIdempotencyKey() error = %v, want %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("parseIdempotencyKey() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestReservationToResponse_MapsExactContract(t *testing.T) {
	snapshot := flashsale.ReservationSnapshot{
		ID:         uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		UserID:     uuid.MustParse("22222222-2222-2222-2222-222222222222"),
		SaleItemID: uuid.MustParse("33333333-3333-3333-3333-333333333333"),
		Quantity:   3,
		State:      flashsale.PendingState,
		CreatedAt:  time.Date(2026, time.September, 19, 12, 0, 0, 0, time.UTC),
		ExpiresAt:  time.Date(2026, time.September, 19, 12, 15, 0, 0, time.UTC),
	}
	reservation, err := flashsale.RehydrateReservation(snapshot)
	if err != nil {
		t.Fatal(err)
	}

	got := reservationToResponse(reservation)
	want := reservationResponse{
		ID:         snapshot.ID,
		UserID:     snapshot.UserID,
		SaleItemID: snapshot.SaleItemID,
		Quantity:   snapshot.Quantity,
		State:      snapshot.State,
		CreatedAt:  snapshot.CreatedAt,
		ExpiresAt:  snapshot.ExpiresAt,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("reservationToResponse() = %#v, want %#v", got, want)
	}

	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	if len(fields) != 7 {
		t.Fatalf("reservation response fields = %v, want exactly 7 fields", fields)
	}
}

func TestOrderToResponse_MapsExactContract(t *testing.T) {
	createdAt := time.Date(2026, time.September, 19, 12, 0, 0, 0, time.UTC)
	order, err := flashsale.NewOrder(flashsale.NewOrderInput{
		ID:            uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		ReservationID: uuid.MustParse("22222222-2222-2222-2222-222222222222"),
		UserID:        uuid.MustParse("33333333-3333-3333-3333-333333333333"),
		SaleItemID:    uuid.MustParse("44444444-4444-4444-4444-444444444444"),
		Quantity:      2,
		CreatedAt:     createdAt,
	})
	if err != nil {
		t.Fatal(err)
	}

	got := orderToResponse(order)
	if got.ID != order.ID() || got.ReservationID != order.ReservationID() ||
		got.UserID != order.UserID() || got.SaleItemID != order.SaleItemID() ||
		got.Quantity != order.Quantity() || !got.CreatedAt.Equal(order.CreatedAt()) {
		t.Fatalf("orderToResponse() = %#v, want fields from %#v", got, order)
	}

	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	if len(fields) != 6 {
		t.Fatalf("order response fields = %v, want exactly 6 fields", fields)
	}
}
