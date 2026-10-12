package flashsale_postgres_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/yyeart/flashdrop/internal/flashsale"
)

func TestStore_FindOrder_ReturnsPersistedOrderAfterPay(t *testing.T) {
	fixture := newReserveFixture(t, 5, flashsale.ActiveState)
	reservation := reserveForPay(t, fixture, uuid.New(), 2)
	orderID := uuid.New()

	ctx, cancel := context.WithTimeout(context.Background(), postgresOperationTimeout)
	defer cancel()

	want, err := fixture.store.Pay(
		ctx, fixture.userID,
		reservation.ID(), orderID, fixture.now,
	)
	if err != nil {
		t.Fatalf("Pay() error = %v", err)
	}

	got, err := fixture.store.FindOrder(ctx, fixture.userID, orderID)
	if err != nil {
		t.Fatalf("FindOrder() error = %v", err)
	}

	if got.ID() != want.ID() ||
		got.ReservationID() != want.ReservationID() ||
		got.UserID() != want.UserID() ||
		got.SaleItemID() != want.SaleItemID() ||
		got.Quantity() != want.Quantity() ||
		!got.CreatedAt().Equal(want.CreatedAt()) {
		t.Fatalf("FindOrder() = %+v, want %+v", got, want)
	}
}

func TestStore_FindOrder_UnknownIDReturnsNotFound(t *testing.T) {
	fixture := newReserveFixture(t, 2, flashsale.ActiveState)

	ctx, cancel := context.WithTimeout(context.Background(), postgresOperationTimeout)
	defer cancel()

	_, err := fixture.store.FindOrder(ctx, fixture.userID, uuid.New())
	if !errors.Is(err, flashsale.ErrOrderNotFound) {
		t.Fatalf("FindOrder() error = %v, want errors.Is(..., ErrOrderNotFound)", err)
	}
}

func TestStore_FindOrder_ForeignOrderReturnsNotFound(t *testing.T) {
	fixture := newReserveFixture(t, 2, flashsale.ActiveState)
	reservation := reserveForPay(t, fixture, uuid.New(), 1)
	foreignUserID := createFixtureUser(t, fixture)

	ctx, cancel := context.WithTimeout(context.Background(), postgresOperationTimeout)
	defer cancel()

	order, err := fixture.store.Pay(
		ctx, fixture.userID,
		reservation.ID(), uuid.New(), fixture.now,
	)
	if err != nil {
		t.Fatalf("Pay() error = %v", err)
	}

	_, err = fixture.store.FindOrder(ctx, foreignUserID, order.ID())
	if !errors.Is(err, flashsale.ErrOrderNotFound) {
		t.Fatalf("FindOrder() error = %v, want errors.Is(..., ErrOrderNotFound)", err)
	}
}

func TestStore_FindOrder_CanceledContextReturnsCanceled(t *testing.T) {
	fixture := newReserveFixture(t, 2, flashsale.ActiveState)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := fixture.store.FindOrder(ctx, fixture.userID, uuid.New())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("FindOrder() error = %v, want errors.Is(..., context.Canceled)", err)
	}
}

func TestStore_FindOrder_PendingReservationHasNoOrderAndDoesNotMutateState(t *testing.T) {
	fixture := newReserveFixture(t, 2, flashsale.ActiveState)
	reservation := reserveForLifecycle(t, fixture, 1)

	ctx, cancel := context.WithTimeout(context.Background(), postgresOperationTimeout)
	defer cancel()

	_, err := fixture.store.FindOrder(ctx, fixture.userID, uuid.New())
	if !errors.Is(err, flashsale.ErrOrderNotFound) {
		t.Fatalf("FindOrder() error = %v, want errors.Is(..., ErrOrderNotFound)", err)
	}

	if state := readReservationState(t, fixture, reservation.ID()); state != flashsale.PendingState {
		t.Fatalf("reservation state = %q, want %q", state, flashsale.PendingState)
	}
	assertLifecycleStock(t, fixture, 1, 0)
	if countOrders(t, fixture, reservation.ID()) != 0 {
		t.Fatal("order was persisted for a pending reservation")
	}
}
