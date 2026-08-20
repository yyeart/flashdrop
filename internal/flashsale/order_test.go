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
			id:            uuid.New(),
			reservationID: uuid.New(),
			userID:        uuid.New(),
			saleItemID:    uuid.New(),
			qty:           3,
			createdAt:     time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC),
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
				input.id = uuid.Nil
			},
			wantErr: ErrInvalidConfiguration,
		},
		{
			name: "empty reservation id",
			mutate: func(input *NewOrderInput) {
				input.reservationID = uuid.Nil
			},
			wantErr: ErrInvalidConfiguration,
		},
		{
			name: "empty user id",
			mutate: func(input *NewOrderInput) {
				input.userID = uuid.Nil
			},
			wantErr: ErrInvalidConfiguration,
		},
		{
			name: "empty sale item id",
			mutate: func(input *NewOrderInput) {
				input.saleItemID = uuid.Nil
			},
			wantErr: ErrInvalidConfiguration,
		},
		{
			name: "zero quantity",
			mutate: func(input *NewOrderInput) {
				input.qty = 0
			},
			wantErr: ErrInvalidQuantity,
		},
		{
			name: "negative quantity",
			mutate: func(input *NewOrderInput) {
				input.qty = -1
			},
			wantErr: ErrInvalidQuantity,
		},
		{
			name: "zero created at",
			mutate: func(input *NewOrderInput) {
				input.createdAt = time.Time{}
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
			id:            uuid.New(),
			reservationID: uuid.New(),
			userID:        uuid.New(),
			saleItemID:    uuid.New(),
			qty:           3,
			createdAt:     time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC),
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
				snapshot.id = uuid.Nil
			},
			wantErr: ErrInvalidConfiguration,
		},
		{
			name: "empty reservation id",
			mutate: func(snapshot *OrderSnapshot) {
				snapshot.reservationID = uuid.Nil
			},
			wantErr: ErrInvalidConfiguration,
		},
		{
			name: "empty user id",
			mutate: func(snapshot *OrderSnapshot) {
				snapshot.userID = uuid.Nil
			},
			wantErr: ErrInvalidConfiguration,
		},
		{
			name: "empty sale item id",
			mutate: func(snapshot *OrderSnapshot) {
				snapshot.saleItemID = uuid.Nil
			},
			wantErr: ErrInvalidConfiguration,
		},
		{
			name: "zero quantity",
			mutate: func(snapshot *OrderSnapshot) {
				snapshot.qty = 0
			},
			wantErr: ErrInvalidQuantity,
		},
		{
			name: "negative quantity",
			mutate: func(snapshot *OrderSnapshot) {
				snapshot.qty = -1
			},
			wantErr: ErrInvalidQuantity,
		},
		{
			name: "zero created at",
			mutate: func(snapshot *OrderSnapshot) {
				snapshot.createdAt = time.Time{}
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

	if order.ID() != input.id {
		t.Errorf("ID() = %v, want %v", order.ID(), input.id)
	}

	if order.ReservationID() != input.reservationID {
		t.Errorf(
			"ReservationID() = %v, want %v",
			order.ReservationID(),
			input.reservationID,
		)
	}

	if order.UserID() != input.userID {
		t.Errorf("UserID() = %v, want %v", order.UserID(), input.userID)
	}

	if order.SaleItemID() != input.saleItemID {
		t.Errorf(
			"SaleItemID() = %v, want %v",
			order.SaleItemID(),
			input.saleItemID,
		)
	}

	if order.Quantity() != input.qty {
		t.Errorf(
			"Quantity() = %d, want %d",
			order.Quantity(),
			input.qty,
		)
	}

	if !order.CreatedAt().Equal(input.createdAt) {
		t.Errorf(
			"CreatedAt() = %v, want %v",
			order.CreatedAt(),
			input.createdAt,
		)
	}
}

func assertOrderMatchesSnapshot(
	t *testing.T,
	order Order,
	snapshot OrderSnapshot,
) {
	t.Helper()

	if order.ID() != snapshot.id {
		t.Errorf("ID() = %v, want %v", order.ID(), snapshot.id)
	}

	if order.ReservationID() != snapshot.reservationID {
		t.Errorf(
			"ReservationID() = %v, want %v",
			order.ReservationID(),
			snapshot.reservationID,
		)
	}

	if order.UserID() != snapshot.userID {
		t.Errorf(
			"UserID() = %v, want %v",
			order.UserID(),
			snapshot.userID,
		)
	}

	if order.SaleItemID() != snapshot.saleItemID {
		t.Errorf(
			"SaleItemID() = %v, want %v",
			order.SaleItemID(),
			snapshot.saleItemID,
		)
	}

	if order.Quantity() != snapshot.qty {
		t.Errorf(
			"Quantity() = %d, want %d",
			order.Quantity(),
			snapshot.qty,
		)
	}

	if !order.CreatedAt().Equal(snapshot.createdAt) {
		t.Errorf(
			"CreatedAt() = %v, want %v",
			order.CreatedAt(),
			snapshot.createdAt,
		)
	}
}
