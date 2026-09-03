package flashsale_postgres_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yyeart/flashdrop/internal/flashsale"
	flashsale_postgres "github.com/yyeart/flashdrop/internal/flashsale/postgres"
)

const reserveConcurrencyAttempts = 1000

type reserveFixture struct {
	pool     *pgxpool.Pool
	store    *flashsale_postgres.Store
	userID   uuid.UUID
	saleID   uuid.UUID
	itemID   uuid.UUID
	now      time.Time
	startsAt time.Time
	endsAt   time.Time
	totalQty int
}

func TestStore_Reserve_CreatesPendingReservationAndIncrementsStock(t *testing.T) {
	fixture := newReserveFixture(t, 5, flashsale.ActiveState)
	reservation := newReservation(t, fixture, uuid.New(), 2)

	ctx, cancel := context.WithTimeout(context.Background(), postgresOperationTimeout)
	defer cancel()

	if err := fixture.store.Reserve(ctx, reservation, fixture.now); err != nil {
		t.Fatalf("Reserve() error = %v", err)
	}

	totalQty, reservedQty, soldQty := readStock(t, fixture)
	if totalQty != fixture.totalQty || reservedQty != 2 || soldQty != 0 {
		t.Fatalf(
			"stock = (%d, %d, %d), want (%d, 2, 0)",
			totalQty, reservedQty, soldQty, fixture.totalQty,
		)
	}

	var (
		gotID, gotUserID, gotItemID uuid.UUID
		gotQuantity                 int
		gotState                    string
		gotCreatedAt                time.Time
		gotExpiresAt                time.Time
	)
	if err := fixture.pool.QueryRow(
		ctx,
		`SELECT id, user_id, sale_item_id, quantity, state, created_at, expires_at
		 FROM flashdrop.reservations WHERE id = $1`,
		reservation.ID(),
	).Scan(
		&gotID, &gotUserID, &gotItemID, &gotQuantity, &gotState,
		&gotCreatedAt, &gotExpiresAt,
	); err != nil {
		t.Fatalf("query reservation: %v", err)
	}

	if gotID != reservation.ID() ||
		gotUserID != reservation.UserID() ||
		gotItemID != reservation.SaleItemID() ||
		gotQuantity != reservation.Quantity() ||
		gotState != string(flashsale.PendingState) ||
		!gotCreatedAt.Equal(reservation.CreatedAt()) ||
		!gotExpiresAt.Equal(reservation.ExpiresAt()) {
		t.Fatalf(
			"reservation = (%s, %s, %s, %d, %s, %v, %v), want (%s, %s, %s, %d, %s, %v, %v)",
			gotID, gotUserID, gotItemID, gotQuantity, gotState, gotCreatedAt, gotExpiresAt,
			reservation.ID(), reservation.UserID(), reservation.SaleItemID(), reservation.Quantity(),
			reservation.State(), reservation.CreatedAt(), reservation.ExpiresAt(),
		)
	}
}

func TestStore_Reserve_RejectsUnavailableStockWithoutChanges(t *testing.T) {
	fixture := newReserveFixture(t, 2, flashsale.ActiveState)
	reservation := newReservation(t, fixture, uuid.New(), 3)

	ctx, cancel := context.WithTimeout(context.Background(), postgresOperationTimeout)
	defer cancel()

	err := fixture.store.Reserve(ctx, reservation, fixture.now)
	if !errors.Is(err, flashsale.ErrReservationUnavailable) {
		t.Fatalf(
			"Reserve() error = %v, want errors.Is(..., ErrReservationUnavailable)",
			err,
		)
	}

	totalQty, reservedQty, soldQty := readStock(t, fixture)
	if totalQty != fixture.totalQty || reservedQty != 0 || soldQty != 0 {
		t.Fatalf("stock = (%d, %d, %d), want (%d, 0, 0)", totalQty, reservedQty, soldQty, fixture.totalQty)
	}
	if countReservations(t, fixture) != 0 {
		t.Fatal("reservation was persisted after unavailable stock")
	}
}

func TestStore_Reserve_RejectsUnavailableSaleWithoutChanges(t *testing.T) {
	tests := []struct {
		name  string
		state flashsale.SaleState
		atEnd bool
	}{
		{name: "draft", state: flashsale.DraftState},
		{name: "ended", state: flashsale.EndedState},
		{name: "at ends_at", state: flashsale.ActiveState, atEnd: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixture, reserveNow := newUnavailableReserveFixture(t, tt.state, tt.atEnd)
			assertReserveUnavailable(t, fixture, reserveNow)
		})
	}
}

func newUnavailableReserveFixture(
	t *testing.T,
	state flashsale.SaleState,
	atEnd bool,
) (*reserveFixture, time.Time) {
	t.Helper()

	if atEnd {
		fixture := newReserveFixtureAtEnd(t, 2)
		return fixture, fixture.endsAt
	}

	fixture := newReserveFixture(t, 2, state)
	return fixture, fixture.now
}

func assertReserveUnavailable(t *testing.T, fixture *reserveFixture, now time.Time) {
	t.Helper()

	reservation := newReservation(t, fixture, uuid.New(), 1)
	ctx, cancel := context.WithTimeout(context.Background(), postgresOperationTimeout)
	defer cancel()

	err := fixture.store.Reserve(ctx, reservation, now)
	if !errors.Is(err, flashsale.ErrReservationUnavailable) {
		t.Fatalf(
			"Reserve() error = %v, want errors.Is(..., ErrReservationUnavailable)",
			err,
		)
	}

	totalQty, reservedQty, soldQty := readStock(t, fixture)
	if totalQty != fixture.totalQty || reservedQty != 0 || soldQty != 0 {
		t.Fatalf("stock = (%d, %d, %d), want (%d, 0, 0)", totalQty, reservedQty, soldQty, fixture.totalQty)
	}
	if countReservations(t, fixture) != 0 {
		t.Fatal("reservation was persisted for an unavailable sale")
	}
}

func TestStore_Reserve_ConflictingReservationIDRollsBackStock(t *testing.T) {
	fixture := newReserveFixture(t, 2, flashsale.ActiveState)
	reservationID := uuid.New()
	first := newReservation(t, fixture, reservationID, 1)
	second := newReservation(t, fixture, reservationID, 1)

	ctx, cancel := context.WithTimeout(context.Background(), postgresOperationTimeout)
	defer cancel()

	if err := fixture.store.Reserve(ctx, first, fixture.now); err != nil {
		t.Fatalf("Reserve(first) error = %v", err)
	}

	err := fixture.store.Reserve(ctx, second, fixture.now)
	if !errors.Is(err, flashsale.ErrConflict) {
		t.Fatalf("Reserve(second) error = %v, want errors.Is(..., ErrConflict)", err)
	}

	totalQty, reservedQty, soldQty := readStock(t, fixture)
	if totalQty != fixture.totalQty || reservedQty != 1 || soldQty != 0 {
		t.Fatalf("stock = (%d, %d, %d), want (%d, 1, 0)", totalQty, reservedQty, soldQty, fixture.totalQty)
	}
	if countReservations(t, fixture) != 1 {
		t.Fatalf("reservation count = %d, want 1", countReservations(t, fixture))
	}
}

func TestStore_Reserve_CanceledContextDoesNotPersist(t *testing.T) {
	fixture := newReserveFixture(t, 2, flashsale.ActiveState)
	reservation := newReservation(t, fixture, uuid.New(), 1)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := fixture.store.Reserve(ctx, reservation, fixture.now)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Reserve() error = %v, want errors.Is(..., context.Canceled)", err)
	}

	totalQty, reservedQty, soldQty := readStock(t, fixture)
	if totalQty != fixture.totalQty || reservedQty != 0 || soldQty != 0 {
		t.Fatalf("stock = (%d, %d, %d), want (%d, 0, 0)", totalQty, reservedQty, soldQty, fixture.totalQty)
	}
	if countReservations(t, fixture) != 0 {
		t.Fatal("reservation was persisted with a canceled context")
	}
}

func TestStore_Reserve_ConcurrentRequestsDoNotOversell(t *testing.T) {
	fixture := newReserveFixture(t, 100, flashsale.ActiveState)
	reservations := make([]flashsale.Reservation, reserveConcurrencyAttempts)
	for idx := range reservations {
		reservations[idx] = newReservation(t, fixture, uuid.New(), 1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	results := make(chan error, reserveConcurrencyAttempts)
	var wg sync.WaitGroup
	for _, reservation := range reservations {
		wg.Add(1)
		go func(reservation flashsale.Reservation) {
			defer wg.Done()
			results <- fixture.store.Reserve(ctx, reservation, fixture.now)
		}(reservation)
	}
	wg.Wait()
	close(results)

	var successes, unavailable int
	for err := range results {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, flashsale.ErrReservationUnavailable):
			unavailable++
		default:
			t.Errorf("Reserve() unexpected error = %v", err)
		}
	}

	if successes != 100 || unavailable != 900 {
		t.Fatalf("results = (%d success, %d unavailable), want (100, 900)", successes, unavailable)
	}

	totalQty, reservedQty, soldQty := readStock(t, fixture)
	if totalQty != 100 || reservedQty != 100 || soldQty != 0 {
		t.Fatalf("stock = (%d, %d, %d), want (100, 100, 0)", totalQty, reservedQty, soldQty)
	}
	if countReservations(t, fixture) != 100 {
		t.Fatalf("reservation count = %d, want 100", countReservations(t, fixture))
	}
}

func newReserveFixture(t *testing.T, totalQty int, state flashsale.SaleState) *reserveFixture {
	t.Helper()

	now := time.Now().UTC().Truncate(time.Microsecond)
	startsAt := now.Add(-time.Minute)
	endsAt := now.Add(time.Hour)
	if state == flashsale.EndedState {
		startsAt = now.Add(-2 * time.Hour)
		endsAt = now.Add(-time.Hour)
	}

	return createReserveFixture(t, totalQty, state, now, startsAt, endsAt)
}

func newReserveFixtureAtEnd(t *testing.T, totalQty int) *reserveFixture {
	t.Helper()

	now := time.Now().UTC().Truncate(time.Microsecond)
	return createReserveFixture(
		t, totalQty, flashsale.ActiveState, now,
		now.Add(-time.Minute), now,
	)
}

func createReserveFixture(
	t *testing.T,
	totalQty int,
	state flashsale.SaleState,
	now, startsAt, endsAt time.Time,
) *reserveFixture {
	t.Helper()

	pool := openTestPool(t)
	store := flashsale_postgres.NewStore(pool)
	fixture := &reserveFixture{
		pool:     pool,
		store:    store,
		userID:   uuid.New(),
		saleID:   uuid.New(),
		itemID:   uuid.New(),
		now:      now,
		startsAt: startsAt,
		endsAt:   endsAt,
		totalQty: totalQty,
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), postgresOperationTimeout)
		defer cleanupCancel()

		if _, err := pool.Exec(cleanupCtx, `DELETE FROM flashdrop.orders WHERE sale_item_id = $1`, fixture.itemID); err != nil {
			t.Errorf("cleanup orders: %v", err)
		}
		if _, err := pool.Exec(cleanupCtx, `DELETE FROM flashdrop.reservations WHERE sale_item_id = $1`, fixture.itemID); err != nil {
			t.Errorf("cleanup reservations: %v", err)
		}
		if _, err := pool.Exec(cleanupCtx, `DELETE FROM flashdrop.sale_items WHERE id = $1`, fixture.itemID); err != nil {
			t.Errorf("cleanup sale item: %v", err)
		}
		if _, err := pool.Exec(cleanupCtx, `DELETE FROM flashdrop.sales WHERE id = $1`, fixture.saleID); err != nil {
			t.Errorf("cleanup sale: %v", err)
		}
		if _, err := pool.Exec(cleanupCtx, `DELETE FROM flashdrop.users WHERE id = $1`, fixture.userID); err != nil {
			t.Errorf("cleanup user: %v", err)
		}
	})

	ctx, cancel := context.WithTimeout(context.Background(), postgresOperationTimeout)
	defer cancel()

	if _, err := pool.Exec(
		ctx,
		`INSERT INTO flashdrop.users (id, role, created_at) VALUES ($1, $2, $3)`,
		fixture.userID, "user", now.Add(-time.Hour),
	); err != nil {
		t.Fatalf("insert test user: %v", err)
	}

	price, err := flashsale.NewMoneyFromMinor(100)
	if err != nil {
		t.Fatalf("NewMoneyFromMinor() error = %v", err)
	}
	sale, err := flashsale.RehydrateSale(flashsale.SaleSnapshot{
		ID:        fixture.saleID,
		State:     state,
		StartsAt:  startsAt,
		EndsAt:    endsAt,
		CreatedAt: now.Add(-time.Hour),
		Items: []flashsale.SaleItemSnapshot{
			{
				ID:          fixture.itemID,
				SaleID:      fixture.saleID,
				ProductID:   uuid.New(),
				Name:        "reserve fixture",
				PriceMinor:  price.AmountMinor(),
				TotalQty:    totalQty,
				ReservedQty: 0,
				SoldQty:     0,
			},
		},
	})
	if err != nil {
		t.Fatalf("RehydrateSale() error = %v", err)
	}
	if err := store.CreateSale(ctx, sale); err != nil {
		t.Fatalf("CreateSale() error = %v", err)
	}

	return fixture
}

func newReservation(
	t *testing.T,
	fixture *reserveFixture,
	id uuid.UUID,
	quantity int,
) flashsale.Reservation {
	t.Helper()

	reservation, err := flashsale.NewReservation(
		id,
		fixture.userID,
		fixture.itemID,
		quantity,
		fixture.now,
		fixture.now.Add(5*time.Minute),
	)
	if err != nil {
		t.Fatalf("NewReservation() error = %v", err)
	}

	return reservation
}

func readStock(t *testing.T, fixture *reserveFixture) (totalQty, reservedQty, soldQty int) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), postgresOperationTimeout)
	defer cancel()

	if err := fixture.pool.QueryRow(
		ctx,
		`SELECT total_qty, reserved_qty, sold_qty
		 FROM flashdrop.sale_items WHERE id = $1`,
		fixture.itemID,
	).Scan(&totalQty, &reservedQty, &soldQty); err != nil {
		t.Fatalf("query stock: %v", err)
	}

	return totalQty, reservedQty, soldQty
}

func countReservations(t *testing.T, fixture *reserveFixture) int {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), postgresOperationTimeout)
	defer cancel()

	var count int
	if err := fixture.pool.QueryRow(
		ctx,
		`SELECT count(*) FROM flashdrop.reservations WHERE sale_item_id = $1`,
		fixture.itemID,
	).Scan(&count); err != nil {
		t.Fatalf("count reservations: %v", err)
	}

	return count
}
