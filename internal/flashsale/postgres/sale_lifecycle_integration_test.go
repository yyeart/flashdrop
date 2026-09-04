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

func TestStore_ActivateSale_ActivatesDraftSaleWithItems(t *testing.T) {
	pool := openTestPool(t)
	store := flashsale_postgres.NewStore(pool)

	want := newDraftSaleWithItem(t, uuid.New())
	cleanupSale(t, pool, want.ID())

	ctx, cancel := context.WithTimeout(context.Background(), postgresOperationTimeout)
	defer cancel()

	if err := store.CreateSale(ctx, want); err != nil {
		t.Fatalf("CreateSale() error = %v", err)
	}

	got, err := store.ActivateSale(ctx, want.ID(), want.EndsAt().Add(-time.Minute))
	if err != nil {
		t.Fatalf("ActivateSale() error = %v", err)
	}

	if got.State() != flashsale.ActiveState {
		t.Fatalf("ActivateSale() state = %q, want %q", got.State(), flashsale.ActiveState)
	}
	assertSaleFieldsEqualIgnoringState(t, want, got)
	assertSaleItemsEqual(t, want.Items(), got.Items())

	persisted, err := store.FindSale(ctx, want.ID())
	if err != nil {
		t.Fatalf("FindSale() after ActivateSale() error = %v", err)
	}
	if persisted.State() != flashsale.ActiveState {
		t.Fatalf("persisted state = %q, want %q", persisted.State(), flashsale.ActiveState)
	}
	assertSaleFieldsEqualIgnoringState(t, want, persisted)
	assertSaleItemsEqual(t, want.Items(), persisted.Items())
}

func TestStore_ActivateSale_RejectsInvalidTransitionsWithoutChanges(t *testing.T) {
	t.Run("draft without items", func(t *testing.T) {
		pool := openTestPool(t)
		store := flashsale_postgres.NewStore(pool)

		sale := newDraftSale(t)
		cleanupSale(t, pool, sale.ID())
		ctx, cancel := context.WithTimeout(context.Background(), postgresOperationTimeout)
		defer cancel()

		if err := store.CreateSale(ctx, sale); err != nil {
			t.Fatalf("CreateSale() error = %v", err)
		}

		assertActivationErrorLeavesState(t, store, ctx, sale, sale.EndsAt().Add(-time.Minute), flashsale.ErrInvalidConfiguration)
	})

	t.Run("already active", func(t *testing.T) {
		pool := openTestPool(t)
		store := flashsale_postgres.NewStore(pool)

		sale := newActiveSale(t)
		cleanupSale(t, pool, sale.ID())
		ctx, cancel := context.WithTimeout(context.Background(), postgresOperationTimeout)
		defer cancel()

		if err := store.CreateSale(ctx, sale); err != nil {
			t.Fatalf("CreateSale() error = %v", err)
		}

		assertActivationErrorLeavesState(t, store, ctx, sale, sale.EndsAt().Add(-time.Minute), flashsale.ErrForbiddenTransition)
	})

	t.Run("already ended", func(t *testing.T) {
		pool := openTestPool(t)
		store := flashsale_postgres.NewStore(pool)

		sale := newActiveSale(t)
		if err := sale.End(); err != nil {
			t.Fatalf("End() error = %v", err)
		}
		cleanupSale(t, pool, sale.ID())
		ctx, cancel := context.WithTimeout(context.Background(), postgresOperationTimeout)
		defer cancel()

		if err := store.CreateSale(ctx, sale); err != nil {
			t.Fatalf("CreateSale() error = %v", err)
		}

		assertActivationErrorLeavesState(t, store, ctx, sale, sale.EndsAt().Add(-time.Minute), flashsale.ErrForbiddenTransition)
	})

	t.Run("expired draft", func(t *testing.T) {
		pool := openTestPool(t)
		store := flashsale_postgres.NewStore(pool)

		sale := newDraftSaleWithItem(t, uuid.New())
		cleanupSale(t, pool, sale.ID())
		ctx, cancel := context.WithTimeout(context.Background(), postgresOperationTimeout)
		defer cancel()

		if err := store.CreateSale(ctx, sale); err != nil {
			t.Fatalf("CreateSale() error = %v", err)
		}

		assertActivationErrorLeavesState(t, store, ctx, sale, sale.EndsAt(), flashsale.ErrExpiredTimeWindow)
	})

	t.Run("unknown sale", func(t *testing.T) {
		pool := openTestPool(t)
		store := flashsale_postgres.NewStore(pool)
		ctx, cancel := context.WithTimeout(context.Background(), postgresOperationTimeout)
		defer cancel()

		saleID := uuid.New()
		_, err := store.ActivateSale(ctx, saleID, time.Time{})
		if !errors.Is(err, flashsale.ErrSaleNotFound) {
			t.Fatalf("ActivateSale() error = %v, want errors.Is(..., ErrSaleNotFound)", err)
		}
		if saleExists(t, pool, saleID) {
			t.Fatalf("unknown sale %s appeared after rejected ActivateSale()", saleID)
		}
	})
}

func TestStore_ActivateSale_ConcurrentCallsHaveOneWinner(t *testing.T) {
	pool := openTestPool(t)
	store := flashsale_postgres.NewStore(pool)

	sale := newDraftSaleWithItem(t, uuid.New())
	cleanupSale(t, pool, sale.ID())

	ctx, cancel := context.WithTimeout(context.Background(), postgresOperationTimeout)
	defer cancel()

	if err := store.CreateSale(ctx, sale); err != nil {
		t.Fatalf("CreateSale() error = %v", err)
	}

	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	wg.Add(2)

	for range 2 {
		go func() {
			defer wg.Done()
			<-start
			_, err := store.ActivateSale(ctx, sale.ID(), sale.EndsAt().Add(-time.Minute))
			results <- err
		}()
	}

	close(start)
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
			t.Errorf("ActivateSale() concurrent error = %v, want nil or ErrForbiddenTransition", err)
		}
	}

	if successes != 1 || forbidden != 1 {
		t.Fatalf("concurrent ActivateSale() results: successes = %d, forbidden = %d, want 1 and 1", successes, forbidden)
	}

	persisted, err := store.FindSale(ctx, sale.ID())
	if err != nil {
		t.Fatalf("FindSale() after concurrent ActivateSale() error = %v", err)
	}
	if persisted.State() != flashsale.ActiveState {
		t.Fatalf("persisted state = %q, want %q", persisted.State(), flashsale.ActiveState)
	}
}

func assertActivationErrorLeavesState(
	t *testing.T,
	store *flashsale_postgres.Store,
	ctx context.Context,
	sale flashsale.Sale,
	now time.Time,
	wantErr error,
) {
	t.Helper()

	_, err := store.ActivateSale(ctx, sale.ID(), now)
	if !errors.Is(err, wantErr) {
		t.Fatalf("ActivateSale() error = %v, want errors.Is(..., %v)", err, wantErr)
	}

	persisted, err := store.FindSale(ctx, sale.ID())
	if err != nil {
		t.Fatalf("FindSale() after rejected ActivateSale() error = %v", err)
	}
	if persisted.State() != sale.State() {
		t.Fatalf("state after rejected ActivateSale() = %q, want %q", persisted.State(), sale.State())
	}
}

func assertSaleFieldsEqualIgnoringState(t *testing.T, want, got flashsale.Sale) {
	t.Helper()

	if got.ID() != want.ID() {
		t.Errorf("ID() = %s, want %s", got.ID(), want.ID())
	}
	if !got.StartsAt().Equal(want.StartsAt()) {
		t.Errorf("StartsAt() = %s, want %s", got.StartsAt(), want.StartsAt())
	}
	if !got.EndsAt().Equal(want.EndsAt()) {
		t.Errorf("EndsAt() = %s, want %s", got.EndsAt(), want.EndsAt())
	}
	if !got.CreatedAt().Equal(want.CreatedAt()) {
		t.Errorf("CreatedAt() = %s, want %s", got.CreatedAt(), want.CreatedAt())
	}
}
