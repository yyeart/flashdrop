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

const idempotencyConcurrencyAttempts = 100

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

	result, err := fixture.store.Reserve(
		ctx,
		newReserveCommand(reservation, uuid.NewString()),
		fixture.now,
	)
	if err != nil {
		t.Fatalf("Reserve() error = %v", err)
	}
	reservation = result.Reservation

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
	idempotencyKey := uuid.NewString()

	ctx, cancel := context.WithTimeout(context.Background(), postgresOperationTimeout)
	defer cancel()

	_, err := fixture.store.Reserve(
		ctx,
		newReserveCommand(reservation, idempotencyKey),
		fixture.now,
	)
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
	if countIdempotencyRecords(t, fixture, idempotencyKey) != 0 {
		t.Fatal("idempotency record was persisted after unavailable stock")
	}
}

func TestStore_Reserve_RejectsInvalidCommandWithoutChanges(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*flashsale_postgres.ReserveCommand, time.Time)
		wantErr error
	}{
		{
			name: "empty reservation ID",
			mutate: func(cmd *flashsale_postgres.ReserveCommand, _ time.Time) {
				cmd.ReservationID = uuid.Nil
			},
			wantErr: flashsale.ErrInvalidConfiguration,
		},
		{
			name: "empty user ID",
			mutate: func(cmd *flashsale_postgres.ReserveCommand, _ time.Time) {
				cmd.UserID = uuid.Nil
			},
			wantErr: flashsale.ErrInvalidConfiguration,
		},
		{
			name: "empty sale item ID",
			mutate: func(cmd *flashsale_postgres.ReserveCommand, _ time.Time) {
				cmd.SaleItemID = uuid.Nil
			},
			wantErr: flashsale.ErrInvalidConfiguration,
		},
		{
			name: "zero quantity",
			mutate: func(cmd *flashsale_postgres.ReserveCommand, _ time.Time) {
				cmd.Quantity = 0
			},
			wantErr: flashsale.ErrInvalidQuantity,
		},
		{
			name: "negative quantity",
			mutate: func(cmd *flashsale_postgres.ReserveCommand, _ time.Time) {
				cmd.Quantity = -1
			},
			wantErr: flashsale.ErrInvalidQuantity,
		},
		{
			name: "zero expiration time",
			mutate: func(cmd *flashsale_postgres.ReserveCommand, _ time.Time) {
				cmd.ExpiresAt = time.Time{}
			},
			wantErr: flashsale.ErrInvalidConfiguration,
		},
		{
			name: "expiration equals creation time",
			mutate: func(cmd *flashsale_postgres.ReserveCommand, now time.Time) {
				cmd.ExpiresAt = now
			},
			wantErr: flashsale.ErrInvalidConfiguration,
		},
		{
			name: "expiration precedes creation time",
			mutate: func(cmd *flashsale_postgres.ReserveCommand, now time.Time) {
				cmd.ExpiresAt = now.Add(-time.Second)
			},
			wantErr: flashsale.ErrInvalidConfiguration,
		},
		{
			name: "empty idempotency key",
			mutate: func(cmd *flashsale_postgres.ReserveCommand, _ time.Time) {
				cmd.IdempotencyKey = ""
			},
			wantErr: flashsale.ErrInvalidConfiguration,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixture := newReserveFixture(t, 5, flashsale.ActiveState)
			cmd := flashsale_postgres.ReserveCommand{
				ReservationID:  uuid.New(),
				UserID:         fixture.userID,
				SaleItemID:     fixture.itemID,
				Quantity:       1,
				ExpiresAt:      fixture.now.Add(5 * time.Minute),
				IdempotencyKey: uuid.NewString(),
			}
			tt.mutate(&cmd, fixture.now)

			ctx, cancel := context.WithTimeout(context.Background(), postgresOperationTimeout)
			defer cancel()

			_, err := fixture.store.Reserve(ctx, cmd, fixture.now)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Reserve() error = %v, want errors.Is(..., %v)", err, tt.wantErr)
			}

			totalQty, reservedQty, soldQty := readStock(t, fixture)
			if totalQty != fixture.totalQty || reservedQty != 0 || soldQty != 0 {
				t.Fatalf(
					"stock = (%d, %d, %d), want (%d, 0, 0)",
					totalQty, reservedQty, soldQty, fixture.totalQty,
				)
			}
			if countReservations(t, fixture) != 0 {
				t.Fatal("reservation was persisted after invalid command")
			}
			if countIdempotencyRecords(t, fixture, cmd.IdempotencyKey) != 0 {
				t.Fatal("idempotency record was persisted after invalid command")
			}
		})
	}
}

func TestStore_Reserve_ReplaysSameIdempotentRequest(t *testing.T) {
	fixture := newReserveFixture(t, 5, flashsale.ActiveState)
	idempotencyKey := uuid.NewString()
	firstReservation := newReservation(t, fixture, uuid.New(), 2)
	secondReservation := newReservation(t, fixture, uuid.New(), 2)

	ctx, cancel := context.WithTimeout(context.Background(), postgresOperationTimeout)
	defer cancel()

	firstResult, err := fixture.store.Reserve(
		ctx,
		newReserveCommand(firstReservation, idempotencyKey),
		fixture.now,
	)
	if err != nil {
		t.Fatalf("first Reserve() error = %v", err)
	}
	if firstResult.Replayed {
		t.Fatal("first Reserve() result is marked as replayed")
	}

	replayNow := firstReservation.ExpiresAt().Add(time.Minute)
	secondResult, err := fixture.store.Reserve(
		ctx,
		newReserveCommand(secondReservation, idempotencyKey),
		replayNow,
	)
	if err != nil {
		t.Fatalf("second Reserve() error = %v", err)
	}
	if !secondResult.Replayed {
		t.Fatal("second Reserve() result is not marked as replayed")
	}
	if secondResult.Reservation.ID() != firstResult.Reservation.ID() {
		t.Fatalf(
			"replayed reservation ID = %s, want %s",
			secondResult.Reservation.ID(), firstResult.Reservation.ID(),
		)
	}
	if !firstResult.Reservation.CreatedAt().Equal(fixture.now) {
		t.Fatalf(
			"first reservation CreatedAt() = %v, want %v",
			firstResult.Reservation.CreatedAt(), fixture.now,
		)
	}
	if !secondResult.Reservation.CreatedAt().Equal(firstResult.Reservation.CreatedAt()) {
		t.Fatalf(
			"replayed reservation CreatedAt() = %v, want %v",
			secondResult.Reservation.CreatedAt(), firstResult.Reservation.CreatedAt(),
		)
	}

	_, reservedQty, soldQty := readStock(t, fixture)
	if reservedQty != 2 || soldQty != 0 {
		t.Fatalf("stock = (reserved %d, sold %d), want (2, 0)", reservedQty, soldQty)
	}
	if countReservations(t, fixture) != 1 {
		t.Fatalf("reservation count = %d, want 1", countReservations(t, fixture))
	}
	if countIdempotencyRecords(t, fixture, idempotencyKey) != 1 {
		t.Fatalf("idempotency record count = %d, want 1", countIdempotencyRecords(t, fixture, idempotencyKey))
	}
}

func TestStore_Reserve_RejectsIdempotencyPayloadConflict(t *testing.T) {
	fixture := newReserveFixture(t, 5, flashsale.ActiveState)
	idempotencyKey := uuid.NewString()
	firstReservation := newReservation(t, fixture, uuid.New(), 1)
	secondReservation := newReservation(t, fixture, uuid.New(), 2)

	ctx, cancel := context.WithTimeout(context.Background(), postgresOperationTimeout)
	defer cancel()

	if _, err := fixture.store.Reserve(
		ctx,
		newReserveCommand(firstReservation, idempotencyKey),
		fixture.now,
	); err != nil {
		t.Fatalf("first Reserve() error = %v", err)
	}

	_, err := fixture.store.Reserve(
		ctx,
		newReserveCommand(secondReservation, idempotencyKey),
		fixture.now,
	)
	if !errors.Is(err, flashsale.ErrConflict) {
		t.Fatalf("second Reserve() error = %v, want errors.Is(..., ErrConflict)", err)
	}

	_, reservedQty, soldQty := readStock(t, fixture)
	if reservedQty != 1 || soldQty != 0 {
		t.Fatalf("stock = (reserved %d, sold %d), want (1, 0)", reservedQty, soldQty)
	}
	if countReservations(t, fixture) != 1 {
		t.Fatalf("reservation count = %d, want 1", countReservations(t, fixture))
	}
	if countIdempotencyRecords(t, fixture, idempotencyKey) != 1 {
		t.Fatalf("idempotency record count = %d, want 1", countIdempotencyRecords(t, fixture, idempotencyKey))
	}
}

func TestStore_Reserve_ConcurrentSameIdempotencyKeyCreatesOneReservation(t *testing.T) {
	fixture := newReserveFixture(t, 1, flashsale.ActiveState)
	idempotencyKey := uuid.NewString()
	reservations := make([]flashsale.Reservation, idempotencyConcurrencyAttempts)
	for idx := range reservations {
		reservations[idx] = newReservation(t, fixture, uuid.New(), 1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	type reserveOutcome struct {
		result flashsale_postgres.ReserveResult
		err    error
	}
	results := make(chan reserveOutcome, len(reservations))
	var wg sync.WaitGroup
	for _, reservation := range reservations {
		wg.Add(1)
		go func(reservation flashsale.Reservation) {
			defer wg.Done()
			result, err := fixture.store.Reserve(
				ctx,
				newReserveCommand(reservation, idempotencyKey),
				fixture.now,
			)
			results <- reserveOutcome{result: result, err: err}
		}(reservation)
	}
	wg.Wait()
	close(results)

	var (
		successes     int
		replays       int
		originalID    uuid.UUID
		originalFound bool
	)
	for outcome := range results {
		if outcome.err != nil {
			t.Errorf("Reserve() unexpected error = %v", outcome.err)
			continue
		}

		successes++
		if outcome.result.Replayed {
			replays++
		}
		if !originalFound {
			originalID = outcome.result.Reservation.ID()
			originalFound = true
		} else if outcome.result.Reservation.ID() != originalID {
			t.Errorf(
				"reservation ID = %s, want %s",
				outcome.result.Reservation.ID(), originalID,
			)
		}
	}

	if successes != idempotencyConcurrencyAttempts {
		t.Fatalf("successful results = %d, want %d", successes, idempotencyConcurrencyAttempts)
	}
	if replays != idempotencyConcurrencyAttempts-1 {
		t.Fatalf("replayed results = %d, want %d", replays, idempotencyConcurrencyAttempts-1)
	}

	_, reservedQty, soldQty := readStock(t, fixture)
	if reservedQty != 1 || soldQty != 0 {
		t.Fatalf("stock = (reserved %d, sold %d), want (1, 0)", reservedQty, soldQty)
	}
	if countReservations(t, fixture) != 1 {
		t.Fatalf("reservation count = %d, want 1", countReservations(t, fixture))
	}
}

func TestStore_Reserve_AllowsSameIdempotencyKeyForDifferentUsers(t *testing.T) {
	fixture := newReserveFixture(t, 2, flashsale.ActiveState)
	secondUserID := uuid.New()

	ctx, cancel := context.WithTimeout(context.Background(), postgresOperationTimeout)
	defer cancel()
	if _, err := fixture.pool.Exec(
		ctx,
		`INSERT INTO flashdrop.users (id, role, created_at) VALUES ($1, $2, $3)`,
		secondUserID, "user", fixture.now.Add(-time.Hour),
	); err != nil {
		t.Fatalf("insert second test user: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), postgresOperationTimeout)
		defer cleanupCancel()
		if _, err := fixture.pool.Exec(
			cleanupCtx,
			`DELETE FROM flashdrop.orders WHERE user_id = $1`,
			secondUserID,
		); err != nil {
			t.Errorf("cleanup second user orders: %v", err)
		}
		if _, err := fixture.pool.Exec(
			cleanupCtx,
			`DELETE FROM flashdrop.reservations WHERE user_id = $1`,
			secondUserID,
		); err != nil {
			t.Errorf("cleanup second user reservations: %v", err)
		}
		if _, err := fixture.pool.Exec(
			cleanupCtx,
			`DELETE FROM flashdrop.idempotency_records WHERE user_id = $1`,
			secondUserID,
		); err != nil {
			t.Errorf("cleanup second user idempotency records: %v", err)
		}
		if _, err := fixture.pool.Exec(
			cleanupCtx,
			`DELETE FROM flashdrop.users WHERE id = $1`,
			secondUserID,
		); err != nil {
			t.Errorf("cleanup second user: %v", err)
		}
	})

	idempotencyKey := uuid.NewString()
	firstReservation := newReservation(t, fixture, uuid.New(), 1)
	secondReservation, err := flashsale.NewReservation(
		uuid.New(), secondUserID, fixture.itemID, 1,
		fixture.now, fixture.now.Add(5*time.Minute),
	)
	if err != nil {
		t.Fatalf("NewReservation() for second user error = %v", err)
	}

	if _, err := fixture.store.Reserve(
		ctx,
		newReserveCommand(firstReservation, idempotencyKey),
		fixture.now,
	); err != nil {
		t.Fatalf("first Reserve() error = %v", err)
	}
	if _, err := fixture.store.Reserve(
		ctx,
		newReserveCommand(secondReservation, idempotencyKey),
		fixture.now,
	); err != nil {
		t.Fatalf("second Reserve() error = %v", err)
	}

	_, reservedQty, soldQty := readStock(t, fixture)
	if reservedQty != 2 || soldQty != 0 {
		t.Fatalf("stock = (reserved %d, sold %d), want (2, 0)", reservedQty, soldQty)
	}
	if countReservations(t, fixture) != 2 {
		t.Fatalf("reservation count = %d, want 2", countReservations(t, fixture))
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

	_, err := fixture.store.Reserve(
		ctx,
		newReserveCommand(reservation, uuid.NewString()),
		now,
	)
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

	if _, err := fixture.store.Reserve(
		ctx,
		newReserveCommand(first, uuid.NewString()),
		fixture.now,
	); err != nil {
		t.Fatalf("Reserve(first) error = %v", err)
	}

	_, err := fixture.store.Reserve(
		ctx,
		newReserveCommand(second, uuid.NewString()),
		fixture.now,
	)
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

	_, err := fixture.store.Reserve(
		ctx,
		newReserveCommand(reservation, uuid.NewString()),
		fixture.now,
	)
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
			_, err := fixture.store.Reserve(
				ctx,
				newReserveCommand(reservation, uuid.NewString()),
				fixture.now,
			)
			results <- err
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
		cleanupReserveFixture(t, fixture)
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

func cleanupReserveFixture(t *testing.T, fixture *reserveFixture) {
	t.Helper()

	cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), postgresOperationTimeout)
	defer cleanupCancel()

	cleanupSteps := []struct {
		name  string
		query string
		arg   uuid.UUID
	}{
		{
			name:  "orders",
			query: `DELETE FROM flashdrop.orders WHERE sale_item_id = $1`,
			arg:   fixture.itemID,
		},
		{
			name:  "reservations",
			query: `DELETE FROM flashdrop.reservations WHERE sale_item_id = $1`,
			arg:   fixture.itemID,
		},
		{
			name:  "idempotency records",
			query: `DELETE FROM flashdrop.idempotency_records WHERE user_id = $1`,
			arg:   fixture.userID,
		},
		{
			name:  "sale item",
			query: `DELETE FROM flashdrop.sale_items WHERE id = $1`,
			arg:   fixture.itemID,
		},
		{
			name:  "sale",
			query: `DELETE FROM flashdrop.sales WHERE id = $1`,
			arg:   fixture.saleID,
		},
		{
			name:  "user",
			query: `DELETE FROM flashdrop.users WHERE id = $1`,
			arg:   fixture.userID,
		},
	}

	for _, step := range cleanupSteps {
		if _, err := fixture.pool.Exec(cleanupCtx, step.query, step.arg); err != nil {
			t.Errorf("cleanup %s: %v", step.name, err)
		}
	}
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

func newReserveCommand(
	reservation flashsale.Reservation,
	idempotencyKey string,
) flashsale_postgres.ReserveCommand {
	return flashsale_postgres.ReserveCommand{
		ReservationID:  reservation.ID(),
		UserID:         reservation.UserID(),
		SaleItemID:     reservation.SaleItemID(),
		Quantity:       reservation.Quantity(),
		ExpiresAt:      reservation.ExpiresAt(),
		IdempotencyKey: idempotencyKey,
	}
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

func countIdempotencyRecords(t *testing.T, fixture *reserveFixture, idempotencyKey string) int {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), postgresOperationTimeout)
	defer cancel()

	var count int
	if err := fixture.pool.QueryRow(
		ctx,
		`SELECT count(*)
		 FROM flashdrop.idempotency_records
		 WHERE user_id = $1 AND idempotency_key = $2`,
		fixture.userID, idempotencyKey,
	).Scan(&count); err != nil {
		t.Fatalf("count idempotency records: %v", err)
	}

	return count
}
