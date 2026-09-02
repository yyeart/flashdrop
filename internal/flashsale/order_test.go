package flashsale

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestNewOrder(t *testing.T) {
	validInput := func() NewOrderInput {
		return NewOrderInput{
			ID:            uuid.New(),
			ReservationID: uuid.New(),
			UserID:        uuid.New(),
			SaleItemID:    uuid.New(),
			Quantity:      3,
			CreatedAt:     time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC),
		}
	}

	tests := []struct {
		name    string
		mutate  func(*NewOrderInput)
		wantErr error
	}{
		{
			name:   "valid order",
			mutate: func(*NewOrderInput) {},
		},
		{
			name: "empty order id",
			mutate: func(input *NewOrderInput) {
				input.ID = uuid.Nil
			},
			wantErr: ErrInvalidConfiguration,
		},
		{
			name: "empty reservation id",
			mutate: func(input *NewOrderInput) {
				input.ReservationID = uuid.Nil
			},
			wantErr: ErrInvalidConfiguration,
		},
		{
			name: "empty user id",
			mutate: func(input *NewOrderInput) {
				input.UserID = uuid.Nil
			},
			wantErr: ErrInvalidConfiguration,
		},
		{
			name: "empty sale item id",
			mutate: func(input *NewOrderInput) {
				input.SaleItemID = uuid.Nil
			},
			wantErr: ErrInvalidConfiguration,
		},
		{
			name: "zero quantity",
			mutate: func(input *NewOrderInput) {
				input.Quantity = 0
			},
			wantErr: ErrInvalidQuantity,
		},
		{
			name: "negative quantity",
			mutate: func(input *NewOrderInput) {
				input.Quantity = -1
			},
			wantErr: ErrInvalidQuantity,
		},
		{
			name: "zero created at",
			mutate: func(input *NewOrderInput) {
				input.CreatedAt = time.Time{}
			},
			wantErr: ErrInvalidConfiguration,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := validInput()
			tt.mutate(&input)

			order, err := NewOrder(input)

			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("NewOrder() error = %v, want %v", err, tt.wantErr)
				}
				return
			}

			if err != nil {
				t.Fatalf("NewOrder() unexpected error = %v", err)
			}

			assertOrderMatchesNewOrderInput(t, order, input)
		})
	}
}

func TestRehydrateOrder(t *testing.T) {
	validSnapshot := func() OrderSnapshot {
		return OrderSnapshot{
			ID:            uuid.New(),
			ReservationID: uuid.New(),
			UserID:        uuid.New(),
			SaleItemID:    uuid.New(),
			Quantity:      3,
			CreatedAt:     time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC),
		}
	}

	tests := []struct {
		name    string
		mutate  func(*OrderSnapshot)
		wantErr error
	}{
		{
			name:   "valid snapshot",
			mutate: func(*OrderSnapshot) {},
		},
		{
			name: "empty order id",
			mutate: func(snapshot *OrderSnapshot) {
				snapshot.ID = uuid.Nil
			},
			wantErr: ErrInvalidConfiguration,
		},
		{
			name: "empty reservation id",
			mutate: func(snapshot *OrderSnapshot) {
				snapshot.ReservationID = uuid.Nil
			},
			wantErr: ErrInvalidConfiguration,
		},
		{
			name: "empty user id",
			mutate: func(snapshot *OrderSnapshot) {
				snapshot.UserID = uuid.Nil
			},
			wantErr: ErrInvalidConfiguration,
		},
		{
			name: "empty sale item id",
			mutate: func(snapshot *OrderSnapshot) {
				snapshot.SaleItemID = uuid.Nil
			},
			wantErr: ErrInvalidConfiguration,
		},
		{
			name: "zero quantity",
			mutate: func(snapshot *OrderSnapshot) {
				snapshot.Quantity = 0
			},
			wantErr: ErrInvalidQuantity,
		},
		{
			name: "negative quantity",
			mutate: func(snapshot *OrderSnapshot) {
				snapshot.Quantity = -1
			},
			wantErr: ErrInvalidQuantity,
		},
		{
			name: "zero created at",
			mutate: func(snapshot *OrderSnapshot) {
				snapshot.CreatedAt = time.Time{}
			},
			wantErr: ErrInvalidConfiguration,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			snapshot := validSnapshot()
			tt.mutate(&snapshot)

			order, err := RehydrateOrder(snapshot)

			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf(
						"RehydrateOrder() error = %v, want %v",
						err,
						tt.wantErr,
					)
				}
				return
			}

			if err != nil {
				t.Fatalf("RehydrateOrder() unexpected error = %v", err)
			}

			assertOrderMatchesSnapshot(t, order, snapshot)
		})
	}
}

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
			t.Errorf(
				"Order exposes unexpected public method %q",
				method.Name,
			)
		}
	}
}

func assertOrderMatchesNewOrderInput(
	t *testing.T,
	order Order,
	input NewOrderInput,
) {
	t.Helper()

	if order.ID() != input.ID {
		t.Errorf("ID() = %v, want %v", order.ID(), input.ID)
	}

	if order.ReservationID() != input.ReservationID {
		t.Errorf(
			"ReservationID() = %v, want %v",
			order.ReservationID(),
			input.ReservationID,
		)
	}

	if order.UserID() != input.UserID {
		t.Errorf("UserID() = %v, want %v", order.UserID(), input.UserID)
	}

	if order.SaleItemID() != input.SaleItemID {
		t.Errorf(
			"SaleItemID() = %v, want %v",
			order.SaleItemID(),
			input.SaleItemID,
		)
	}

	if order.Quantity() != input.Quantity {
		t.Errorf(
			"Quantity() = %d, want %d",
			order.Quantity(),
			input.Quantity,
		)
	}

	if !order.CreatedAt().Equal(input.CreatedAt) {
		t.Errorf(
			"CreatedAt() = %v, want %v",
			order.CreatedAt(),
			input.CreatedAt,
		)
	}
}

func assertOrderMatchesSnapshot(
	t *testing.T,
	order Order,
	snapshot OrderSnapshot,
) {
	t.Helper()

	if order.ID() != snapshot.ID {
		t.Errorf("ID() = %v, want %v", order.ID(), snapshot.ID)
	}

	if order.ReservationID() != snapshot.ReservationID {
		t.Errorf(
			"ReservationID() = %v, want %v",
			order.ReservationID(),
			snapshot.ReservationID,
		)
	}

	if order.UserID() != snapshot.UserID {
		t.Errorf(
			"UserID() = %v, want %v",
			order.UserID(),
			snapshot.UserID,
		)
	}

	if order.SaleItemID() != snapshot.SaleItemID {
		t.Errorf(
			"SaleItemID() = %v, want %v",
			order.SaleItemID(),
			snapshot.SaleItemID,
		)
	}

	if order.Quantity() != snapshot.Quantity {
		t.Errorf(
			"Quantity() = %d, want %d",
			order.Quantity(),
			snapshot.Quantity,
		)
	}

	if !order.CreatedAt().Equal(snapshot.CreatedAt) {
		t.Errorf(
			"CreatedAt() = %v, want %v",
			order.CreatedAt(),
			snapshot.CreatedAt,
		)
	}
}
