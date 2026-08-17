package flashsale

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestOrder_PublicAPIIsReadOnly(t *testing.T) {
	orderType := reflect.TypeOf(Order{})

	for i := range orderType.NumField() {
		field := orderType.Field(i)

		if field.IsExported() {
			t.Errorf("Order field %q must not be exported", field.Name)
		}
	}

	allowedMethods := map[string]struct{}{
		"ID":            {},
		"ReservationID": {},
		"UserID":        {},
		"SaleItemID":    {},
		"Quantity":      {},
		"CreatedAt":     {},
	}

	orderPtrType := reflect.TypeOf(&Order{})

	if orderPtrType.NumMethod() != len(allowedMethods) {
		t.Fatalf(
			"Order has %d exported methods, want %d",
			orderPtrType.NumMethod(),
			len(allowedMethods),
		)
	}

	for i := range orderPtrType.NumMethod() {
		method := orderPtrType.Method(i)

		if _, ok := allowedMethods[method.Name]; !ok {
			t.Errorf("Order exposes unexpected public method %q", method.Name)
		}
	}
}

func TestNewOrder_ExposesCreatedDataReadOnly(t *testing.T) {
	id := uuid.New()
	reservationID := uuid.New()
	userID := uuid.New()
	saleItemID := uuid.New()
	quantity := 3
	createdAt := time.Date(2026, time.August, 15, 12, 0, 0, 0, time.UTC)

	order, err := newOrder(
		id,
		reservationID,
		userID,
		saleItemID,
		quantity,
		createdAt,
	)
	if err != nil {
		t.Fatalf("NewOrder() error = %v", err)
	}

	if got := order.ID(); got != id {
		t.Errorf("ID() = %v, want %v", got, id)
	}

	if got := order.ReservationID(); got != reservationID {
		t.Errorf("ReservationID() = %v, want %v", got, reservationID)
	}

	if got := order.UserID(); got != userID {
		t.Errorf("UserID() = %v, want %v", got, userID)
	}

	if got := order.SaleItemID(); got != saleItemID {
		t.Errorf("SaleItemID() = %v, want %v", got, saleItemID)
	}

	if got := order.Quantity(); got != quantity {
		t.Errorf("Quantity() = %d, want %d", got, quantity)
	}

	if got := order.CreatedAt(); !got.Equal(createdAt) {
		t.Errorf("CreatedAt() = %v, want %v", got, createdAt)
	}
}

func TestNewOrder_EmptyOrderID(t *testing.T) {
	id := uuid.Nil
	reservationID := uuid.New()
	userID := uuid.New()
	saleItemID := uuid.New()
	quantity := 3
	createdAt := time.Date(2026, time.August, 15, 12, 0, 0, 0, time.UTC)

	_, err := newOrder(
		id, reservationID, userID, saleItemID, quantity, createdAt,
	)

	if !errors.Is(err, ErrInvalidConfiguration) {
		t.Fatalf(
			"newOrder() error = %v, want ErrInvalidConfiguration",
			err,
		)
	}
}

func TestNewOrder_EmptyReservationID(t *testing.T) {
	id := uuid.New()
	reservationID := uuid.Nil
	userID := uuid.New()
	saleItemID := uuid.New()
	quantity := 3
	createdAt := time.Date(2026, time.August, 15, 12, 0, 0, 0, time.UTC)

	_, err := newOrder(
		id, reservationID, userID, saleItemID, quantity, createdAt,
	)

	if !errors.Is(err, ErrInvalidConfiguration) {
		t.Fatalf(
			"newOrder() error = %v, want ErrInvalidConfiguration",
			err,
		)
	}
}

func TestNewOrder_EmptyUserID(t *testing.T) {
	id := uuid.New()
	reservationID := uuid.New()
	userID := uuid.Nil
	saleItemID := uuid.New()
	quantity := 3
	createdAt := time.Date(2026, time.August, 15, 12, 0, 0, 0, time.UTC)

	_, err := newOrder(
		id, reservationID, userID, saleItemID, quantity, createdAt,
	)

	if !errors.Is(err, ErrInvalidConfiguration) {
		t.Fatalf(
			"newOrder() error = %v, want ErrInvalidConfiguration",
			err,
		)
	}
}

func TestNewOrder_EmptySaleItemID(t *testing.T) {
	id := uuid.New()
	reservationID := uuid.New()
	userID := uuid.New()
	saleItemID := uuid.Nil
	quantity := 3
	createdAt := time.Date(2026, time.August, 15, 12, 0, 0, 0, time.UTC)

	_, err := newOrder(
		id, reservationID, userID, saleItemID, quantity, createdAt,
	)

	if !errors.Is(err, ErrInvalidConfiguration) {
		t.Fatalf(
			"newOrder() error = %v, want ErrInvalidConfiguration",
			err,
		)
	}
}

func TestNewOrder_NegativeQty(t *testing.T) {
	id := uuid.New()
	reservationID := uuid.New()
	userID := uuid.New()
	saleItemID := uuid.New()
	quantity := -1
	createdAt := time.Date(2026, time.August, 15, 12, 0, 0, 0, time.UTC)

	_, err := newOrder(
		id, reservationID, userID, saleItemID, quantity, createdAt,
	)

	if !errors.Is(err, ErrInvalidQuantity) {
		t.Fatalf(
			"newOrder() error = %v, want ErrInvalidQuantity",
			err,
		)
	}
}

func TestNewOrder_ZeroQty(t *testing.T) {
	id := uuid.New()
	reservationID := uuid.New()
	userID := uuid.New()
	saleItemID := uuid.New()
	quantity := 0
	createdAt := time.Date(2026, time.August, 15, 12, 0, 0, 0, time.UTC)

	_, err := newOrder(
		id, reservationID, userID, saleItemID, quantity, createdAt,
	)

	if !errors.Is(err, ErrInvalidQuantity) {
		t.Fatalf(
			"newOrder() error = %v, want ErrInvalidQuantity",
			err,
		)
	}
}

func TestNewOrder_ZeroCreatedAt(t *testing.T) {
	id := uuid.New()
	reservationID := uuid.New()
	userID := uuid.New()
	saleItemID := uuid.New()
	quantity := 3
	createdAt := time.Time{}

	_, err := newOrder(
		id, reservationID, userID, saleItemID, quantity, createdAt,
	)

	if !errors.Is(err, ErrInvalidConfiguration) {
		t.Fatalf(
			"newOrder() error = %v, want ErrInvalidConfiguration",
			err,
		)
	}
}
