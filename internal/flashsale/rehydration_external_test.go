package flashsale_test

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/yyeart/flashdrop/internal/flashsale"
)

func TestRehydrateSaleRestoresAllStates(t *testing.T) {
	states := []flashsale.SaleState{
		flashsale.DraftState,
		flashsale.ActiveState,
		flashsale.EndedState,
	}

	for _, state := range states {

		t.Run(string(state), func(t *testing.T) {
			snapshot := validSaleSnapshot()
			snapshot.State = state

			sale, err := flashsale.RehydrateSale(snapshot)
			if err != nil {
				t.Fatalf("RehydrateSale() error = %v", err)
			}

			assertSaleMatchesSnapshot(t, sale, snapshot)
		})
	}
}

func assertSaleMatchesSnapshot(
	t *testing.T,
	sale flashsale.Sale,
	snapshot flashsale.SaleSnapshot,
) {
	t.Helper()

	if sale.ID() != snapshot.ID {
		t.Fatalf("ID() = %v, want %v", sale.ID(), snapshot.ID)
	}

	if sale.State() != snapshot.State {
		t.Fatalf("State() = %q, want %q", sale.State(), snapshot.State)
	}

	if !sale.StartsAt().Equal(snapshot.StartsAt) {
		t.Fatalf("StartsAt() = %v, want %v", sale.StartsAt(), snapshot.StartsAt)
	}

	if !sale.EndsAt().Equal(snapshot.EndsAt) {
		t.Fatalf("EndsAt() = %v, want %v", sale.EndsAt(), snapshot.EndsAt)
	}

	if !sale.CreatedAt().Equal(snapshot.CreatedAt) {
		t.Fatalf("CreatedAt() = %v, want %v", sale.CreatedAt(), snapshot.CreatedAt)
	}

	assertSaleItemMatchesSnapshot(t, sale, snapshot.Items[0])
}

func TestRehydrateSaleAllowsDraftWithoutItems(t *testing.T) {
	snapshot := validSaleSnapshot()
	snapshot.State = flashsale.DraftState
	snapshot.Items = nil

	sale, err := flashsale.RehydrateSale(snapshot)
	if err != nil {
		t.Fatalf("RehydrateSale() error = %v", err)
	}

	if len(sale.Items()) != 0 {
		t.Fatalf("len(Items()) = %d, want 0", len(sale.Items()))
	}
}

func TestRehydrateSaleCopiesItems(t *testing.T) {
	snapshot := validSaleSnapshot()
	originalItem := snapshot.Items[0]

	sale, err := flashsale.RehydrateSale(snapshot)
	if err != nil {
		t.Fatalf("RehydrateSale() error = %v", err)
	}

	snapshot.Items[0].ID = uuid.New()
	snapshot.Items[0].ReservedQty = 9
	snapshot.Items = nil

	assertSaleItemMatchesSnapshot(t, sale, originalItem)

	items := sale.Items()
	items[0] = flashsale.SaleItem{}

	assertSaleItemMatchesSnapshot(t, sale, originalItem)
}

func TestRehydrateSaleRejectsInvalidSnapshots(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*flashsale.SaleSnapshot)
		wantErr error
	}{
		{
			name: "unknown state",
			mutate: func(snapshot *flashsale.SaleSnapshot) {
				snapshot.State = flashsale.SaleState("unknown")
			},
			wantErr: flashsale.ErrInvalidConfiguration,
		},
		{
			name: "active sale without items",
			mutate: func(snapshot *flashsale.SaleSnapshot) {
				snapshot.State = flashsale.ActiveState
				snapshot.Items = nil
			},
			wantErr: flashsale.ErrInvalidConfiguration,
		},
		{
			name: "ended sale without items",
			mutate: func(snapshot *flashsale.SaleSnapshot) {
				snapshot.State = flashsale.EndedState
				snapshot.Items = nil
			},
			wantErr: flashsale.ErrInvalidConfiguration,
		},
		{
			name: "empty sale item id",
			mutate: func(snapshot *flashsale.SaleSnapshot) {
				snapshot.Items[0].ID = uuid.Nil
			},
			wantErr: flashsale.ErrInvalidConfiguration,
		},
		{
			name: "sale item belongs to another sale",
			mutate: func(snapshot *flashsale.SaleSnapshot) {
				snapshot.Items[0].SaleID = uuid.New()
			},
			wantErr: flashsale.ErrInvalidConfiguration,
		},
		{
			name: "empty product id",
			mutate: func(snapshot *flashsale.SaleSnapshot) {
				snapshot.Items[0].ProductID = uuid.Nil
			},
			wantErr: flashsale.ErrInvalidConfiguration,
		},
		{
			name: "negative price",
			mutate: func(snapshot *flashsale.SaleSnapshot) {
				snapshot.Items[0].PriceMinor = -1
			},
			wantErr: flashsale.ErrInvalidMoney,
		},
		{
			name: "zero price",
			mutate: func(snapshot *flashsale.SaleSnapshot) {
				snapshot.Items[0].PriceMinor = 0
			},
			wantErr: flashsale.ErrInvalidMoney,
		},
		{
			name: "zero total quantity",
			mutate: func(snapshot *flashsale.SaleSnapshot) {
				snapshot.Items[0].TotalQty = 0
			},
			wantErr: flashsale.ErrInvalidQuantity,
		},
		{
			name: "negative reserved quantity",
			mutate: func(snapshot *flashsale.SaleSnapshot) {
				snapshot.Items[0].ReservedQty = -1
			},
			wantErr: flashsale.ErrInvalidQuantity,
		},
		{
			name: "negative sold quantity",
			mutate: func(snapshot *flashsale.SaleSnapshot) {
				snapshot.Items[0].SoldQty = -1
			},
			wantErr: flashsale.ErrInvalidQuantity,
		},
		{
			name: "reserved quantity exceeds total",
			mutate: func(snapshot *flashsale.SaleSnapshot) {
				snapshot.Items[0].ReservedQty = snapshot.Items[0].TotalQty + 1
			},
			wantErr: flashsale.ErrInvalidQuantity,
		},
		{
			name: "sold quantity exceeds available",
			mutate: func(snapshot *flashsale.SaleSnapshot) {
				snapshot.Items[0].ReservedQty = 6
				snapshot.Items[0].SoldQty = 5
			},
			wantErr: flashsale.ErrInvalidQuantity,
		},
		{
			name: "duplicate item id",
			mutate: func(snapshot *flashsale.SaleSnapshot) {
				snapshot.Items = append(snapshot.Items, snapshot.Items[0])
			},
			wantErr: flashsale.ErrDuplicateSaleItem,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			snapshot := validSaleSnapshot()
			tt.mutate(&snapshot)

			_, err := flashsale.RehydrateSale(snapshot)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf(
					"RehydrateSale() error = %v, want %v",
					err,
					tt.wantErr,
				)
			}
		})
	}
}

func TestRehydrateReservationRestoresAllStates(t *testing.T) {
	states := []flashsale.ReservationState{
		flashsale.PendingState,
		flashsale.PaidState,
		flashsale.CancelledState,
		flashsale.ExpiredState,
	}

	for _, state := range states {

		t.Run(string(state), func(t *testing.T) {
			snapshot := validReservationSnapshot()
			snapshot.State = state

			reservation, err := flashsale.RehydrateReservation(snapshot)
			if err != nil {
				t.Fatalf("RehydrateReservation() error = %v", err)
			}

			assertReservationMatchesSnapshot(t, reservation, snapshot)
		})
	}
}

func assertReservationMatchesSnapshot(
	t *testing.T,
	reservation flashsale.Reservation,
	snapshot flashsale.ReservationSnapshot,
) {
	t.Helper()

	if reservation.ID() != snapshot.ID {
		t.Fatalf("ID() = %v, want %v", reservation.ID(), snapshot.ID)
	}

	if reservation.UserID() != snapshot.UserID {
		t.Fatalf("UserID() = %v, want %v", reservation.UserID(), snapshot.UserID)
	}

	if reservation.SaleItemID() != snapshot.SaleItemID {
		t.Fatalf(
			"SaleItemID() = %v, want %v",
			reservation.SaleItemID(),
			snapshot.SaleItemID,
		)
	}

	if reservation.State() != snapshot.State {
		t.Fatalf("State() = %q, want %q", reservation.State(), snapshot.State)
	}

	if reservation.Quantity() != snapshot.Quantity {
		t.Fatalf("Quantity() = %d, want %d", reservation.Quantity(), snapshot.Quantity)
	}

	if !reservation.CreatedAt().Equal(snapshot.CreatedAt) {
		t.Fatalf(
			"CreatedAt() = %v, want %v",
			reservation.CreatedAt(),
			snapshot.CreatedAt,
		)
	}

	if !reservation.ExpiresAt().Equal(snapshot.ExpiresAt) {
		t.Fatalf("ExpiresAt() = %v, want %v", reservation.ExpiresAt(), snapshot.ExpiresAt)
	}
}

func TestRehydrateReservationAllowsExpiredPendingSnapshot(t *testing.T) {
	snapshot := validReservationSnapshot()
	snapshot.State = flashsale.PendingState
	snapshot.CreatedAt = time.Date(2020, time.January, 1, 12, 0, 0, 0, time.UTC)
	snapshot.ExpiresAt = snapshot.CreatedAt.Add(time.Minute)

	if _, err := flashsale.RehydrateReservation(snapshot); err != nil {
		t.Fatalf("RehydrateReservation() error = %v", err)
	}
}

func TestRehydrateReservationRejectsUnknownState(t *testing.T) {
	snapshot := validReservationSnapshot()
	snapshot.State = flashsale.ReservationState("unknown")

	_, err := flashsale.RehydrateReservation(snapshot)
	if !errors.Is(err, flashsale.ErrInvalidConfiguration) {
		t.Fatalf(
			"RehydrateReservation() error = %v, want ErrInvalidConfiguration",
			err,
		)
	}
}

func TestRehydrateOrderRestoresAllFields(t *testing.T) {
	snapshot := validOrderSnapshot()

	order, err := flashsale.RehydrateOrder(snapshot)
	if err != nil {
		t.Fatalf("RehydrateOrder() error = %v", err)
	}

	if order.ID() != snapshot.ID {
		t.Fatalf("ID() = %v, want %v", order.ID(), snapshot.ID)
	}

	if order.ReservationID() != snapshot.ReservationID {
		t.Fatalf(
			"ReservationID() = %v, want %v",
			order.ReservationID(),
			snapshot.ReservationID,
		)
	}

	if order.UserID() != snapshot.UserID {
		t.Fatalf("UserID() = %v, want %v", order.UserID(), snapshot.UserID)
	}

	if order.SaleItemID() != snapshot.SaleItemID {
		t.Fatalf(
			"SaleItemID() = %v, want %v",
			order.SaleItemID(),
			snapshot.SaleItemID,
		)
	}

	if order.Quantity() != snapshot.Quantity {
		t.Fatalf("Quantity() = %d, want %d", order.Quantity(), snapshot.Quantity)
	}

	if !order.CreatedAt().Equal(snapshot.CreatedAt) {
		t.Fatalf(
			"CreatedAt() = %v, want %v",
			order.CreatedAt(),
			snapshot.CreatedAt,
		)
	}
}

func TestRehydrateOrderRejectsInvalidSnapshots(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*flashsale.OrderSnapshot)
		wantErr error
	}{
		{
			name: "empty order id",
			mutate: func(snapshot *flashsale.OrderSnapshot) {
				snapshot.ID = uuid.Nil
			},
			wantErr: flashsale.ErrInvalidConfiguration,
		},
		{
			name: "empty reservation id",
			mutate: func(snapshot *flashsale.OrderSnapshot) {
				snapshot.ReservationID = uuid.Nil
			},
			wantErr: flashsale.ErrInvalidConfiguration,
		},
		{
			name: "empty user id",
			mutate: func(snapshot *flashsale.OrderSnapshot) {
				snapshot.UserID = uuid.Nil
			},
			wantErr: flashsale.ErrInvalidConfiguration,
		},
		{
			name: "empty sale item id",
			mutate: func(snapshot *flashsale.OrderSnapshot) {
				snapshot.SaleItemID = uuid.Nil
			},
			wantErr: flashsale.ErrInvalidConfiguration,
		},
		{
			name: "zero quantity",
			mutate: func(snapshot *flashsale.OrderSnapshot) {
				snapshot.Quantity = 0
			},
			wantErr: flashsale.ErrInvalidQuantity,
		},
		{
			name: "zero created at",
			mutate: func(snapshot *flashsale.OrderSnapshot) {
				snapshot.CreatedAt = time.Time{}
			},
			wantErr: flashsale.ErrInvalidConfiguration,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			snapshot := validOrderSnapshot()
			tt.mutate(&snapshot)

			_, err := flashsale.RehydrateOrder(snapshot)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf(
					"RehydrateOrder() error = %v, want %v",
					err,
					tt.wantErr,
				)
			}
		})
	}
}

func TestRehydrateSaleRejectsInvalidSaleFields(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*flashsale.SaleSnapshot)
	}{
		{
			name: "empty sale id",
			mutate: func(snapshot *flashsale.SaleSnapshot) {
				snapshot.ID = uuid.Nil
			},
		},
		{
			name: "starts at equals ends at",
			mutate: func(snapshot *flashsale.SaleSnapshot) {
				snapshot.StartsAt = snapshot.EndsAt
			},
		},
		{
			name: "starts at after ends at",
			mutate: func(snapshot *flashsale.SaleSnapshot) {
				snapshot.StartsAt = snapshot.EndsAt.Add(time.Second)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			snapshot := validSaleSnapshot()
			tt.mutate(&snapshot)

			_, err := flashsale.RehydrateSale(snapshot)
			if !errors.Is(err, flashsale.ErrInvalidConfiguration) {
				t.Fatalf(
					"RehydrateSale() error = %v, want ErrInvalidConfiguration",
					err,
				)
			}
		})
	}
}

func TestRehydrateReservationRejectsInvalidFields(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*flashsale.ReservationSnapshot)
		wantErr error
	}{
		{
			name: "empty reservation id",
			mutate: func(snapshot *flashsale.ReservationSnapshot) {
				snapshot.ID = uuid.Nil
			},
			wantErr: flashsale.ErrInvalidConfiguration,
		},
		{
			name: "empty user id",
			mutate: func(snapshot *flashsale.ReservationSnapshot) {
				snapshot.UserID = uuid.Nil
			},
			wantErr: flashsale.ErrInvalidConfiguration,
		},
		{
			name: "empty sale item id",
			mutate: func(snapshot *flashsale.ReservationSnapshot) {
				snapshot.SaleItemID = uuid.Nil
			},
			wantErr: flashsale.ErrInvalidConfiguration,
		},
		{
			name: "zero quantity",
			mutate: func(snapshot *flashsale.ReservationSnapshot) {
				snapshot.Quantity = 0
			},
			wantErr: flashsale.ErrInvalidQuantity,
		},
		{
			name: "created at equals expires at",
			mutate: func(snapshot *flashsale.ReservationSnapshot) {
				snapshot.ExpiresAt = snapshot.CreatedAt
			},
			wantErr: flashsale.ErrInvalidConfiguration,
		},
		{
			name: "created at after expires at",
			mutate: func(snapshot *flashsale.ReservationSnapshot) {
				snapshot.CreatedAt = snapshot.ExpiresAt.Add(time.Second)
			},
			wantErr: flashsale.ErrInvalidConfiguration,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			snapshot := validReservationSnapshot()
			tt.mutate(&snapshot)

			_, err := flashsale.RehydrateReservation(snapshot)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf(
					"RehydrateReservation() error = %v, want %v",
					err,
					tt.wantErr,
				)
			}
		})
	}
}

func TestPersistenceConstructorsAreUsableFromExternalPackage(t *testing.T) {
	now := time.Date(2026, time.August, 15, 12, 0, 0, 0, time.UTC)

	if _, err := flashsale.NewOrder(flashsale.NewOrderInput{
		ID:            uuid.New(),
		ReservationID: uuid.New(),
		UserID:        uuid.New(),
		SaleItemID:    uuid.New(),
		Quantity:      1,
		CreatedAt:     now,
	}); err != nil {
		t.Fatalf("NewOrder() error = %v", err)
	}

	if _, err := flashsale.RehydrateOrder(validOrderSnapshot()); err != nil {
		t.Fatalf("RehydrateOrder() error = %v", err)
	}

	if _, err := flashsale.RehydrateSale(validSaleSnapshot()); err != nil {
		t.Fatalf("RehydrateSale() error = %v", err)
	}

	if _, err := flashsale.RehydrateReservation(validReservationSnapshot()); err != nil {
		t.Fatalf("RehydrateReservation() error = %v", err)
	}
}

func validSaleSnapshot() flashsale.SaleSnapshot {
	saleID := uuid.New()
	createdAt := time.Date(2026, time.August, 15, 12, 0, 0, 0, time.UTC)

	return flashsale.SaleSnapshot{
		ID:        saleID,
		State:     flashsale.ActiveState,
		StartsAt:  createdAt.Add(time.Hour),
		EndsAt:    createdAt.Add(3 * time.Hour),
		CreatedAt: createdAt,
		Items: []flashsale.SaleItemSnapshot{
			{
				ID:          uuid.New(),
				SaleID:      saleID,
				ProductID:   uuid.New(),
				Name:        "Test product",
				PriceMinor:  10_000,
				TotalQty:    10,
				ReservedQty: 2,
				SoldQty:     3,
			},
		},
	}
}

func validReservationSnapshot() flashsale.ReservationSnapshot {
	createdAt := time.Date(2026, time.August, 15, 12, 0, 0, 0, time.UTC)

	return flashsale.ReservationSnapshot{
		ID:         uuid.New(),
		UserID:     uuid.New(),
		SaleItemID: uuid.New(),
		Quantity:   2,
		State:      flashsale.PendingState,
		CreatedAt:  createdAt,
		ExpiresAt:  createdAt.Add(5 * time.Minute),
	}
}

func validOrderSnapshot() flashsale.OrderSnapshot {
	return flashsale.OrderSnapshot{
		ID:            uuid.New(),
		ReservationID: uuid.New(),
		UserID:        uuid.New(),
		SaleItemID:    uuid.New(),
		Quantity:      2,
		CreatedAt: time.Date(
			2026,
			time.August,
			15,
			12,
			0,
			0,
			0,
			time.UTC,
		),
	}
}

func assertSaleItemMatchesSnapshot(
	t *testing.T,
	sale flashsale.Sale,
	snapshot flashsale.SaleItemSnapshot,
) {
	t.Helper()

	item, ok := sale.Item(snapshot.ID)
	if !ok {
		t.Fatalf("Item(%v) ok = false, want true", snapshot.ID)
	}

	if item.SaleID() != snapshot.SaleID {
		t.Fatalf("SaleID() = %v, want %v", item.SaleID(), snapshot.SaleID)
	}

	if item.ProductID() != snapshot.ProductID {
		t.Fatalf("ProductID() = %v, want %v", item.ProductID(), snapshot.ProductID)
	}

	if item.Name() != snapshot.Name {
		t.Fatalf("Name() = %q, want %q", item.Name(), snapshot.Name)
	}

	if item.Price().AmountMinor() != snapshot.PriceMinor {
		t.Fatalf("Price().AmountMinor() = %d, want %d", item.Price().AmountMinor(), snapshot.PriceMinor)
	}

	if item.TotalQty() != snapshot.TotalQty {
		t.Fatalf("TotalQty() = %d, want %d", item.TotalQty(), snapshot.TotalQty)
	}

	if item.ReservedQty() != snapshot.ReservedQty {
		t.Fatalf("ReservedQty() = %d, want %d", item.ReservedQty(), snapshot.ReservedQty)
	}

	if item.SoldQty() != snapshot.SoldQty {
		t.Fatalf("SoldQty() = %d, want %d", item.SoldQty(), snapshot.SoldQty)
	}
}
