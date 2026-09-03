package flashsale_postgres_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/yyeart/flashdrop/internal/flashsale"
)

const lifecycleCancelConcurrencyAttempts = 100

func TestStore_Cancel_RestoresStockAndMarksReservationCancelled(t *testing.T) {
	fixture := newReserveFixture(t, 5, flashsale.ActiveState)
	reservation := reserveForLifecycle(t, fixture, 2)

	ctx, cancel := context.WithTimeout(context.Background(), postgresOperationTimeout)
	defer cancel()

	if err := fixture.store.Cancel(ctx, reservation.ID(), fixture.now); err != nil {
		t.Fatalf("Cancel() error = %v", err)
	}

	if state := readReservationState(t, fixture, reservation.ID()); state != flashsale.CancelledState {
		t.Fatalf("reservation state = %q, want %q", state, flashsale.CancelledState)
	}

	assertLifecycleStock(t, fixture, 0, 0)
	if countOrders(t, fixture, reservation.ID()) != 0 {
		t.Fatal("order was persisted after Cancel")
	}
}

func TestStore_Expire_AtExpiresAtRestoresStockAndMarksReservationExpired(t *testing.T) {
	fixture := newReserveFixture(t, 5, flashsale.ActiveState)
	reservation := reserveForLifecycle(t, fixture, 2)

	ctx, cancel := context.WithTimeout(context.Background(), postgresOperationTimeout)
	defer cancel()

	if err := fixture.store.Expire(ctx, reservation.ID(), reservation.ExpiresAt()); err != nil {
		t.Fatalf("Expire() error = %v", err)
	}

	if state := readReservationState(t, fixture, reservation.ID()); state != flashsale.ExpiredState {
		t.Fatalf("reservation state = %q, want %q", state, flashsale.ExpiredState)
	}

	assertLifecycleStock(t, fixture, 0, 0)
	if countOrders(t, fixture, reservation.ID()) != 0 {
		t.Fatal("order was persisted after Expire")
	}
}

func TestStore_Cancel_AfterExpirationRejectsWithoutChanges(t *testing.T) {
	fixture := newReserveFixture(t, 2, flashsale.ActiveState)
	reservation := reserveForLifecycle(t, fixture, 1)

	ctx, cancel := context.WithTimeout(context.Background(), postgresOperationTimeout)
	defer cancel()

	err := fixture.store.Cancel(ctx, reservation.ID(), reservation.ExpiresAt())
	if !errors.Is(err, flashsale.ErrExpiredTimeWindow) {
		t.Fatalf("Cancel() error = %v, want errors.Is(..., ErrExpiredTimeWindow)", err)
	}

	if state := readReservationState(t, fixture, reservation.ID()); state != flashsale.PendingState {
		t.Fatalf("reservation state = %q, want %q", state, flashsale.PendingState)
	}
	assertLifecycleStock(t, fixture, 1, 0)
	if countOrders(t, fixture, reservation.ID()) != 0 {
		t.Fatal("order was persisted after rejected Cancel")
	}
}

func TestStore_Expire_BeforeExpirationRejectsWithoutChanges(t *testing.T) {
	fixture := newReserveFixture(t, 2, flashsale.ActiveState)
	reservation := reserveForLifecycle(t, fixture, 1)

	ctx, cancel := context.WithTimeout(context.Background(), postgresOperationTimeout)
	defer cancel()

	err := fixture.store.Expire(ctx, reservation.ID(), fixture.now)
	if !errors.Is(err, flashsale.ErrForbiddenTransition) {
		t.Fatalf("Expire() error = %v, want errors.Is(..., ErrForbiddenTransition)", err)
	}

	if state := readReservationState(t, fixture, reservation.ID()); state != flashsale.PendingState {
		t.Fatalf("reservation state = %q, want %q", state, flashsale.PendingState)
	}
	assertLifecycleStock(t, fixture, 1, 0)
	if countOrders(t, fixture, reservation.ID()) != 0 {
		t.Fatal("order was persisted after rejected Expire")
	}
}

func TestStore_Lifecycle_UnknownReservation(t *testing.T) {
	tests := []struct {
		name string
		call func(*reserveFixture, context.Context, uuid.UUID, time.Time) error
	}{
		{
			name: "cancel",
			call: func(fixture *reserveFixture, ctx context.Context, id uuid.UUID, now time.Time) error {
				return fixture.store.Cancel(ctx, id, now)
			},
		},
		{
			name: "expire",
			call: func(fixture *reserveFixture, ctx context.Context, id uuid.UUID, now time.Time) error {
				return fixture.store.Expire(ctx, id, now)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixture := newReserveFixture(t, 2, flashsale.ActiveState)
			ctx, cancel := context.WithTimeout(context.Background(), postgresOperationTimeout)
			defer cancel()

			err := tt.call(fixture, ctx, uuid.New(), fixture.now)
			if !errors.Is(err, flashsale.ErrReservationNotFound) {
				t.Fatalf("lifecycle operation error = %v, want errors.Is(..., ErrReservationNotFound)", err)
			}

			assertLifecycleStock(t, fixture, 0, 0)
		})
	}
}

func TestStore_Cancel_TerminalReservationIsNotCancelledAgain(t *testing.T) {
	fixture := newReserveFixture(t, 2, flashsale.ActiveState)
	reservation := reserveForLifecycle(t, fixture, 1)

	ctx, cancel := context.WithTimeout(context.Background(), postgresOperationTimeout)
	defer cancel()

	if err := fixture.store.Cancel(ctx, reservation.ID(), fixture.now); err != nil {
		t.Fatalf("Cancel(first) error = %v", err)
	}

	err := fixture.store.Cancel(ctx, reservation.ID(), fixture.now)
	if !errors.Is(err, flashsale.ErrForbiddenTransition) {
		t.Fatalf("Cancel(second) error = %v, want errors.Is(..., ErrForbiddenTransition)", err)
	}

	if state := readReservationState(t, fixture, reservation.ID()); state != flashsale.CancelledState {
		t.Fatalf("reservation state = %q, want %q", state, flashsale.CancelledState)
	}
	assertLifecycleStock(t, fixture, 0, 0)
}

func TestStore_Expire_TerminalReservationIsNotExpiredAgain(t *testing.T) {
	fixture := newReserveFixture(t, 2, flashsale.ActiveState)
	reservation := reserveForLifecycle(t, fixture, 1)

	ctx, cancel := context.WithTimeout(context.Background(), postgresOperationTimeout)
	defer cancel()

	if err := fixture.store.Expire(ctx, reservation.ID(), reservation.ExpiresAt()); err != nil {
		t.Fatalf("Expire(first) error = %v", err)
	}

	err := fixture.store.Expire(ctx, reservation.ID(), reservation.ExpiresAt())
	if !errors.Is(err, flashsale.ErrForbiddenTransition) {
		t.Fatalf("Expire(second) error = %v, want errors.Is(..., ErrForbiddenTransition)", err)
	}

	if state := readReservationState(t, fixture, reservation.ID()); state != flashsale.ExpiredState {
		t.Fatalf("reservation state = %q, want %q", state, flashsale.ExpiredState)
	}
	assertLifecycleStock(t, fixture, 0, 0)
}

func TestStore_Lifecycle_CanceledContextDoesNotPersist(t *testing.T) {
	tests := []struct {
		name string
		call func(*reserveFixture, context.Context, uuid.UUID, time.Time) error
	}{
		{
			name: "cancel",
			call: func(fixture *reserveFixture, ctx context.Context, id uuid.UUID, now time.Time) error {
				return fixture.store.Cancel(ctx, id, now)
			},
		},
		{
			name: "expire",
			call: func(fixture *reserveFixture, ctx context.Context, id uuid.UUID, now time.Time) error {
				return fixture.store.Expire(ctx, id, now)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixture := newReserveFixture(t, 2, flashsale.ActiveState)
			reservation := reserveForLifecycle(t, fixture, 1)

			ctx, cancel := context.WithCancel(context.Background())
			cancel()

			err := tt.call(fixture, ctx, reservation.ID(), fixture.now)
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("lifecycle operation error = %v, want errors.Is(..., context.Canceled)", err)
			}

			if state := readReservationState(t, fixture, reservation.ID()); state != flashsale.PendingState {
				t.Fatalf("reservation state = %q, want %q", state, flashsale.PendingState)
			}
			assertLifecycleStock(t, fixture, 1, 0)
			if countOrders(t, fixture, reservation.ID()) != 0 {
				t.Fatal("order was persisted with a canceled context")
			}
		})
	}
}

func TestStore_Cancel_ConcurrentCallsHaveOneWinner(t *testing.T) {
	fixture := newReserveFixture(t, 2, flashsale.ActiveState)
	reservation := reserveForLifecycle(t, fixture, 1)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	results := make(chan error, lifecycleCancelConcurrencyAttempts)
	var wg sync.WaitGroup
	for range lifecycleCancelConcurrencyAttempts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- fixture.store.Cancel(ctx, reservation.ID(), fixture.now)
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
			t.Errorf("Cancel() unexpected error = %v", err)
		}
	}

	if successes != 1 || forbidden != lifecycleCancelConcurrencyAttempts-1 {
		t.Fatalf(
			"results = (%d success, %d forbidden), want (1, %d)",
			successes, forbidden, lifecycleCancelConcurrencyAttempts-1,
		)
	}

	if state := readReservationState(t, fixture, reservation.ID()); state != flashsale.CancelledState {
		t.Fatalf("reservation state = %q, want %q", state, flashsale.CancelledState)
	}
	assertLifecycleStock(t, fixture, 0, 0)
	if countOrders(t, fixture, reservation.ID()) != 0 {
		t.Fatal("order was persisted after concurrent Cancel")
	}
}

func reserveForLifecycle(
	t *testing.T,
	fixture *reserveFixture,
	quantity int,
) flashsale.Reservation {
	t.Helper()

	reservation := newReservation(t, fixture, uuid.New(), quantity)
	ctx, cancel := context.WithTimeout(context.Background(), postgresOperationTimeout)
	defer cancel()

	if err := fixture.store.Reserve(ctx, reservation, fixture.now); err != nil {
		t.Fatalf("Reserve() error = %v", err)
	}

	return reservation
}

func assertLifecycleStock(t *testing.T, fixture *reserveFixture, wantReserved, wantSold int) {
	t.Helper()

	totalQty, reservedQty, soldQty := readStock(t, fixture)
	if totalQty != fixture.totalQty || reservedQty != wantReserved || soldQty != wantSold {
		t.Fatalf(
			"stock = (%d, %d, %d), want (%d, %d, %d)",
			totalQty, reservedQty, soldQty, fixture.totalQty, wantReserved, wantSold,
		)
	}
}
