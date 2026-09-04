package flashsale_postgres_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/yyeart/flashdrop/internal/flashsale"
	flashsale_postgres "github.com/yyeart/flashdrop/internal/flashsale/postgres"
)

const payConcurrencyAttempts = 100

func TestStore_Pay_CreatesOrderAndMovesStock(t *testing.T) {
	fixture := newReserveFixture(t, 5, flashsale.ActiveState)
	reservation := reserveForPay(t, fixture, uuid.New(), 2)
	orderID := uuid.New()

	ctx, cancel := context.WithTimeout(context.Background(), postgresOperationTimeout)
	defer cancel()

	order, err := fixture.store.Pay(ctx, reservation.ID(), orderID, fixture.now)
	if err != nil {
		t.Fatalf("Pay() error = %v", err)
	}

	if order.ID() != orderID ||
		order.ReservationID() != reservation.ID() ||
		order.UserID() != reservation.UserID() ||
		order.SaleItemID() != reservation.SaleItemID() ||
		order.Quantity() != reservation.Quantity() ||
		!order.CreatedAt().Equal(fixture.now) {
		t.Fatalf("order does not match Pay input: %+v", order)
	}

	totalQty, reservedQty, soldQty := readStock(t, fixture)
	if totalQty != fixture.totalQty || reservedQty != 0 || soldQty != 2 {
		t.Fatalf("stock = (%d, %d, %d), want (%d, 0, 2)", totalQty, reservedQty, soldQty, fixture.totalQty)
	}
	if state := readReservationState(t, fixture, reservation.ID()); state != flashsale.PaidState {
		t.Fatalf("reservation state = %q, want %q", state, flashsale.PaidState)
	}
	if countOrders(t, fixture, reservation.ID()) != 1 {
		t.Fatalf("order count = %d, want 1", countOrders(t, fixture, reservation.ID()))
	}
}

func TestStore_Pay_UnknownReservation(t *testing.T) {
	fixture := newReserveFixture(t, 2, flashsale.ActiveState)
	unknownID := uuid.New()

	ctx, cancel := context.WithTimeout(context.Background(), postgresOperationTimeout)
	defer cancel()

	_, err := fixture.store.Pay(ctx, unknownID, uuid.New(), fixture.now)
	if !errors.Is(err, flashsale.ErrReservationNotFound) {
		t.Fatalf("Pay() error = %v, want errors.Is(..., ErrReservationNotFound)", err)
	}

	totalQty, reservedQty, soldQty := readStock(t, fixture)
	if totalQty != fixture.totalQty || reservedQty != 0 || soldQty != 0 {
		t.Fatalf("stock = (%d, %d, %d), want (%d, 0, 0)", totalQty, reservedQty, soldQty, fixture.totalQty)
	}
}

func TestStore_Pay_ExpiredReservationDoesNotChangeStateOrStock(t *testing.T) {
	fixture := newReserveFixture(t, 2, flashsale.ActiveState)
	reservation, err := flashsale.NewReservation(
		uuid.New(),
		fixture.userID,
		fixture.itemID,
		1,
		fixture.now.Add(-2*time.Minute),
		fixture.now.Add(-time.Minute),
	)
	if err != nil {
		t.Fatalf("NewReservation() error = %v", err)
	}

	reserveContext, reserveCancel := context.WithTimeout(context.Background(), postgresOperationTimeout)
	defer reserveCancel()
	if _, err := fixture.store.Reserve(
		reserveContext,
		flashsale_postgres.ReserveCommand{
			Reservation:    reservation,
			IdempotencyKey: uuid.NewString(),
		},
		fixture.now,
	); err != nil {
		t.Fatalf("Reserve() error = %v", err)
	}

	payContext, payCancel := context.WithTimeout(context.Background(), postgresOperationTimeout)
	defer payCancel()
	_, err = fixture.store.Pay(payContext, reservation.ID(), uuid.New(), fixture.now)
	if !errors.Is(err, flashsale.ErrExpiredTimeWindow) {
		t.Fatalf("Pay() error = %v, want errors.Is(..., ErrExpiredTimeWindow)", err)
	}

	if state := readReservationState(t, fixture, reservation.ID()); state != flashsale.PendingState {
		t.Fatalf("reservation state = %q, want %q", state, flashsale.PendingState)
	}
	totalQty, reservedQty, soldQty := readStock(t, fixture)
	if totalQty != fixture.totalQty || reservedQty != 1 || soldQty != 0 {
		t.Fatalf("stock = (%d, %d, %d), want (%d, 1, 0)", totalQty, reservedQty, soldQty, fixture.totalQty)
	}
	if countOrders(t, fixture, reservation.ID()) != 0 {
		t.Fatal("order was persisted for an expired reservation")
	}
}

func TestStore_Pay_TerminalReservationIsNotPaidAgain(t *testing.T) {
	fixture := newReserveFixture(t, 2, flashsale.ActiveState)
	reservation := reserveForPay(t, fixture, uuid.New(), 1)

	ctx, cancel := context.WithTimeout(context.Background(), postgresOperationTimeout)
	defer cancel()

	if _, err := fixture.store.Pay(ctx, reservation.ID(), uuid.New(), fixture.now); err != nil {
		t.Fatalf("Pay(first) error = %v", err)
	}

	_, err := fixture.store.Pay(ctx, reservation.ID(), uuid.New(), fixture.now)
	if !errors.Is(err, flashsale.ErrForbiddenTransition) {
		t.Fatalf("Pay(second) error = %v, want errors.Is(..., ErrForbiddenTransition)", err)
	}

	if state := readReservationState(t, fixture, reservation.ID()); state != flashsale.PaidState {
		t.Fatalf("reservation state = %q, want %q", state, flashsale.PaidState)
	}
	totalQty, reservedQty, soldQty := readStock(t, fixture)
	if totalQty != fixture.totalQty || reservedQty != 0 || soldQty != 1 {
		t.Fatalf("stock = (%d, %d, %d), want (%d, 0, 1)", totalQty, reservedQty, soldQty, fixture.totalQty)
	}
	if countOrders(t, fixture, reservation.ID()) != 1 {
		t.Fatalf("order count = %d, want 1", countOrders(t, fixture, reservation.ID()))
	}
}

func TestStore_Pay_OrderConflictRollsBackAllChanges(t *testing.T) {
	fixture := newReserveFixture(t, 3, flashsale.ActiveState)
	first := reserveForPay(t, fixture, uuid.New(), 1)
	second := reserveForPay(t, fixture, uuid.New(), 1)
	conflictingOrderID := uuid.New()

	ctx, cancel := context.WithTimeout(context.Background(), postgresOperationTimeout)
	defer cancel()

	if _, err := fixture.store.Pay(ctx, first.ID(), conflictingOrderID, fixture.now); err != nil {
		t.Fatalf("Pay(first) error = %v", err)
	}

	_, err := fixture.store.Pay(ctx, second.ID(), conflictingOrderID, fixture.now)
	if !errors.Is(err, flashsale.ErrConflict) {
		t.Fatalf("Pay(second) error = %v, want errors.Is(..., ErrConflict)", err)
	}

	if state := readReservationState(t, fixture, second.ID()); state != flashsale.PendingState {
		t.Fatalf("second reservation state = %q, want %q", state, flashsale.PendingState)
	}
	totalQty, reservedQty, soldQty := readStock(t, fixture)
	if totalQty != fixture.totalQty || reservedQty != 1 || soldQty != 1 {
		t.Fatalf("stock = (%d, %d, %d), want (%d, 1, 1)", totalQty, reservedQty, soldQty, fixture.totalQty)
	}
	if countOrdersForItem(t, fixture) != 1 {
		t.Fatalf("order count = %d, want 1", countOrdersForItem(t, fixture))
	}
}

func TestStore_Pay_CanceledContextDoesNotPersist(t *testing.T) {
	fixture := newReserveFixture(t, 2, flashsale.ActiveState)
	reservation := reserveForPay(t, fixture, uuid.New(), 1)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := fixture.store.Pay(ctx, reservation.ID(), uuid.New(), fixture.now)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Pay() error = %v, want errors.Is(..., context.Canceled)", err)
	}

	if state := readReservationState(t, fixture, reservation.ID()); state != flashsale.PendingState {
		t.Fatalf("reservation state = %q, want %q", state, flashsale.PendingState)
	}
	totalQty, reservedQty, soldQty := readStock(t, fixture)
	if totalQty != fixture.totalQty || reservedQty != 1 || soldQty != 0 {
		t.Fatalf("stock = (%d, %d, %d), want (%d, 1, 0)", totalQty, reservedQty, soldQty, fixture.totalQty)
	}
	if countOrders(t, fixture, reservation.ID()) != 0 {
		t.Fatal("order was persisted with a canceled context")
	}
}

func TestStore_Pay_ConcurrentCallsHaveOneWinner(t *testing.T) {
	fixture := newReserveFixture(t, 2, flashsale.ActiveState)
	reservation := reserveForPay(t, fixture, uuid.New(), 1)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	results := make(chan error, payConcurrencyAttempts)
	var wg sync.WaitGroup
	for range payConcurrencyAttempts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := fixture.store.Pay(ctx, reservation.ID(), uuid.New(), fixture.now)
			results <- err
		}()
	}
	wg.Wait()
	close(results)

	var successes, forbidden int
	for err := range results {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, flashsale.ErrForbiddenTransition):
			forbidden++
		default:
			t.Errorf("Pay() unexpected error = %v", err)
		}
	}

	if successes != 1 || forbidden != payConcurrencyAttempts-1 {
		t.Fatalf("results = (%d success, %d forbidden), want (1, %d)", successes, forbidden, payConcurrencyAttempts-1)
	}
	if state := readReservationState(t, fixture, reservation.ID()); state != flashsale.PaidState {
		t.Fatalf("reservation state = %q, want %q", state, flashsale.PaidState)
	}
	totalQty, reservedQty, soldQty := readStock(t, fixture)
	if totalQty != fixture.totalQty || reservedQty != 0 || soldQty != 1 {
		t.Fatalf("stock = (%d, %d, %d), want (%d, 0, 1)", totalQty, reservedQty, soldQty, fixture.totalQty)
	}
	if countOrders(t, fixture, reservation.ID()) != 1 {
		t.Fatalf("order count = %d, want 1", countOrders(t, fixture, reservation.ID()))
	}
}

func reserveForPay(
	t *testing.T,
	fixture *reserveFixture,
	id uuid.UUID,
	quantity int,
) flashsale.Reservation {
	t.Helper()

	reservation := newReservation(t, fixture, id, quantity)
	ctx, cancel := context.WithTimeout(context.Background(), postgresOperationTimeout)
	defer cancel()

	if _, err := fixture.store.Reserve(
		ctx,
		flashsale_postgres.ReserveCommand{
			Reservation:    reservation,
			IdempotencyKey: uuid.NewString(),
		},
		fixture.now,
	); err != nil {
		t.Fatalf("Reserve() error = %v", err)
	}

	return reservation
}

func readReservationState(
	t *testing.T,
	fixture *reserveFixture,
	reservationID uuid.UUID,
) flashsale.ReservationState {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), postgresOperationTimeout)
	defer cancel()

	var state string
	if err := fixture.pool.QueryRow(
		ctx,
		`SELECT state FROM flashdrop.reservations WHERE id = $1`,
		reservationID,
	).Scan(&state); err != nil {
		t.Fatalf("query reservation state: %v", err)
	}

	return flashsale.ReservationState(state)
}

func countOrders(t *testing.T, fixture *reserveFixture, reservationID uuid.UUID) int {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), postgresOperationTimeout)
	defer cancel()

	var count int
	if err := fixture.pool.QueryRow(
		ctx,
		`SELECT count(*) FROM flashdrop.orders WHERE reservation_id = $1`,
		reservationID,
	).Scan(&count); err != nil {
		t.Fatalf("count orders: %v", err)
	}

	return count
}

func countOrdersForItem(t *testing.T, fixture *reserveFixture) int {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), postgresOperationTimeout)
	defer cancel()

	var count int
	if err := fixture.pool.QueryRow(
		ctx,
		`SELECT count(*) FROM flashdrop.orders WHERE sale_item_id = $1`,
		fixture.itemID,
	).Scan(&count); err != nil {
		t.Fatalf("count orders for item: %v", err)
	}

	return count
}
