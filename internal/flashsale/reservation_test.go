package flashsale

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestNewReservationCreatesPending(t *testing.T) {
	id := uuid.New()
	userID := uuid.New()
	saleItemID := uuid.New()
	createdAt := testReservationNow()
	expiresAt := createdAt.Add(5 * time.Minute)

	reservation, err := NewReservation(
		id,
		userID,
		saleItemID,
		2,
		createdAt,
		expiresAt,
	)
	if err != nil {
		t.Fatalf("NewReservation() error = %v", err)
	}

	if reservation.ID() != id {
		t.Fatalf("ID() = %v, want %v", reservation.ID(), id)
	}

	if reservation.UserID() != userID {
		t.Fatalf("UserID() = %v, want %v", reservation.UserID(), userID)
	}

	if reservation.SaleItemID() != saleItemID {
		t.Fatalf(
			"SaleItemID() = %v, want %v",
			reservation.SaleItemID(),
			saleItemID,
		)
	}

	if reservation.Quantity() != 2 {
		t.Fatalf("Quantity() = %d, want 2", reservation.Quantity())
	}

	if reservation.State() != PendingState {
		t.Fatalf(
			"State() = %q, want %q",
			reservation.State(),
			PendingState,
		)
	}

	if !reservation.CreatedAt().Equal(createdAt) {
		t.Fatalf(
			"CreatedAt() = %v, want %v",
			reservation.CreatedAt(),
			createdAt,
		)
	}

	if !reservation.ExpiresAt().Equal(expiresAt) {
		t.Fatalf(
			"ExpiresAt() = %v, want %v",
			reservation.ExpiresAt(),
			expiresAt,
		)
	}
}

func TestNewReservationRejectsEmptyIDs(t *testing.T) {
	tests := []struct {
		name       string
		id         uuid.UUID
		userID     uuid.UUID
		saleItemID uuid.UUID
	}{
		{
			name:       "empty reservation id",
			id:         uuid.Nil,
			userID:     uuid.New(),
			saleItemID: uuid.New(),
		},
		{
			name:       "empty user id",
			id:         uuid.New(),
			userID:     uuid.Nil,
			saleItemID: uuid.New(),
		},
		{
			name:       "empty sale item id",
			id:         uuid.New(),
			userID:     uuid.New(),
			saleItemID: uuid.Nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			createdAt := testReservationNow()

			_, err := NewReservation(
				tt.id,
				tt.userID,
				tt.saleItemID,
				1,
				createdAt,
				createdAt.Add(time.Minute),
			)

			if !errors.Is(err, ErrInvalidConfiguration) {
				t.Fatalf(
					"NewReservation() error = %v, want ErrInvalidConfiguration",
					err,
				)
			}
		})
	}
}

func TestNewReservationRejectsInvalidQuantity(t *testing.T) {
	tests := []struct {
		name string
		qty  int
	}{
		{
			name: "zero",
			qty:  0,
		},
		{
			name: "negative",
			qty:  -1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			createdAt := testReservationNow()

			_, err := NewReservation(
				uuid.New(),
				uuid.New(),
				uuid.New(),
				tt.qty,
				createdAt,
				createdAt.Add(time.Minute),
			)

			if !errors.Is(err, ErrInvalidQuantity) {
				t.Fatalf(
					"NewReservation() error = %v, want ErrInvalidQuantity",
					err,
				)
			}
		})
	}
}

func TestNewReservationRejectsInvalidExpiration(t *testing.T) {
	now := testReservationNow()

	tests := []struct {
		name      string
		createdAt time.Time
		expiresAt time.Time
	}{
		{
			name:      "expires_at equals created_at",
			createdAt: now,
			expiresAt: now,
		},
		{
			name:      "expires_at before created_at",
			createdAt: now,
			expiresAt: now.Add(-time.Second),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewReservation(
				uuid.New(),
				uuid.New(),
				uuid.New(),
				1,
				tt.createdAt,
				tt.expiresAt,
			)

			if !errors.Is(err, ErrInvalidConfiguration) {
				t.Fatalf(
					"NewReservation() error = %v, want ErrInvalidConfiguration",
					err,
				)
			}
		})
	}
}

func TestReservationPay(t *testing.T) {
	reservation := newValidReservation(t)

	now := reservation.ExpiresAt().Add(-time.Second)

	if err := reservation.Pay(now); err != nil {
		t.Fatalf("Pay() error = %v", err)
	}

	if reservation.State() != PaidState {
		t.Fatalf(
			"State() = %q, want %q",
			reservation.State(),
			PaidState,
		)
	}
}

func TestReservationCancel(t *testing.T) {
	reservation := newValidReservation(t)

	now := reservation.ExpiresAt().Add(-time.Second)

	if err := reservation.Cancel(now); err != nil {
		t.Fatalf("Cancel() error = %v", err)
	}

	if reservation.State() != CancelledState {
		t.Fatalf(
			"State() = %q, want %q",
			reservation.State(),
			CancelledState,
		)
	}
}

func TestReservationExpire(t *testing.T) {
	reservation := newValidReservation(t)

	now := reservation.ExpiresAt().Add(time.Second)

	if err := reservation.Expire(now); err != nil {
		t.Fatalf("Expire() error = %v", err)
	}

	if reservation.State() != ExpiredState {
		t.Fatalf(
			"State() = %q, want %q",
			reservation.State(),
			ExpiredState,
		)
	}
}

func TestReservationAtExpiresAt(t *testing.T) {
	t.Run("pay is forbidden and state stays pending", func(t *testing.T) {
		reservation := newValidReservation(t)

		err := reservation.Pay(reservation.ExpiresAt())

		if !errors.Is(err, ErrExpiredTimeWindow) {
			t.Fatalf(
				"Pay() error = %v, want ErrExpiredTimeWindow",
				err,
			)
		}

		if reservation.State() != PendingState {
			t.Fatalf(
				"State() = %q, want %q",
				reservation.State(),
				PendingState,
			)
		}
	})

	t.Run("cancel is forbidden and state stays pending", func(t *testing.T) {
		reservation := newValidReservation(t)

		err := reservation.Cancel(reservation.ExpiresAt())

		if !errors.Is(err, ErrExpiredTimeWindow) {
			t.Fatalf(
				"Cancel() error = %v, want ErrExpiredTimeWindow",
				err,
			)
		}

		if reservation.State() != PendingState {
			t.Fatalf(
				"State() = %q, want %q",
				reservation.State(),
				PendingState,
			)
		}
	})

	t.Run("expire is allowed", func(t *testing.T) {
		reservation := newValidReservation(t)

		err := reservation.Expire(reservation.ExpiresAt())

		if err != nil {
			t.Fatalf("Expire() error = %v", err)
		}

		if reservation.State() != ExpiredState {
			t.Fatalf(
				"State() = %q, want %q",
				reservation.State(),
				ExpiredState,
			)
		}
	})
}

func TestReservationPayAfterExpirationDoesNotChangeState(t *testing.T) {
	reservation := newValidReservation(t)

	err := reservation.Pay(
		reservation.ExpiresAt().Add(time.Second),
	)

	if !errors.Is(err, ErrExpiredTimeWindow) {
		t.Fatalf(
			"Pay() error = %v, want ErrExpiredTimeWindow",
			err,
		)
	}

	if reservation.State() != PendingState {
		t.Fatalf(
			"State() = %q, want %q",
			reservation.State(),
			PendingState,
		)
	}
}

func TestReservationCancelAfterExpirationDoesNotChangeState(t *testing.T) {
	reservation := newValidReservation(t)

	err := reservation.Cancel(
		reservation.ExpiresAt().Add(time.Second),
	)

	if !errors.Is(err, ErrExpiredTimeWindow) {
		t.Fatalf(
			"Cancel() error = %v, want ErrExpiredTimeWindow",
			err,
		)
	}

	if reservation.State() != PendingState {
		t.Fatalf(
			"State() = %q, want %q",
			reservation.State(),
			PendingState,
		)
	}
}

func TestReservationExpireBeforeExpirationDoesNotChangeState(t *testing.T) {
	reservation := newValidReservation(t)

	err := reservation.Expire(
		reservation.ExpiresAt().Add(-time.Second),
	)

	if !errors.Is(err, ErrForbiddenTransition) {
		t.Fatalf(
			"Expire() error = %v, want ErrForbiddenTransition",
			err,
		)
	}

	if reservation.State() != PendingState {
		t.Fatalf(
			"State() = %q, want %q",
			reservation.State(),
			PendingState,
		)
	}
}

func TestReservationTerminalStatesRejectAllTransitions(t *testing.T) {
	tests := []struct {
		name      string
		finalize  func(*Reservation) error
		wantState ReservationState
	}{
		{
			name: "paid",
			finalize: func(r *Reservation) error {
				return r.Pay(r.ExpiresAt().Add(-time.Second))
			},
			wantState: PaidState,
		},
		{
			name: "cancelled",
			finalize: func(r *Reservation) error {
				return r.Cancel(r.ExpiresAt().Add(-time.Second))
			},
			wantState: CancelledState,
		},
		{
			name: "expired",
			finalize: func(r *Reservation) error {
				return r.Expire(r.ExpiresAt())
			},
			wantState: ExpiredState,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertTerminalStateRejectsAllTransitions(
				t,
				tt.finalize,
				tt.wantState,
			)
		})
	}
}

func assertTerminalStateRejectsAllTransitions(
	t *testing.T,
	finalize func(*Reservation) error,
	wantState ReservationState,
) {
	t.Helper()

	operations := []struct {
		name string
		run  func(*Reservation) error
	}{
		{
			name: "pay",
			run: func(r *Reservation) error {
				return r.Pay(r.ExpiresAt().Add(-time.Second))
			},
		},
		{
			name: "cancel",
			run: func(r *Reservation) error {
				return r.Cancel(r.ExpiresAt().Add(-time.Second))
			},
		},
		{
			name: "expire",
			run: func(r *Reservation) error {
				return r.Expire(r.ExpiresAt())
			},
		},
	}

	for _, op := range operations {
		t.Run(op.name, func(t *testing.T) {
			assertForbiddenTransition(
				t,
				finalize,
				op.run,
				wantState,
			)
		})
	}
}

func assertForbiddenTransition(
	t *testing.T,
	finalize func(*Reservation) error,
	operation func(*Reservation) error,
	wantState ReservationState,
) {
	t.Helper()

	reservation := newValidReservation(t)

	if err := finalize(&reservation); err != nil {
		t.Fatalf("finalize reservation: %v", err)
	}

	err := operation(&reservation)
	if !errors.Is(err, ErrForbiddenTransition) {
		t.Fatalf(
			"transition error = %v, want ErrForbiddenTransition",
			err,
		)
	}

	if reservation.State() != wantState {
		t.Fatalf(
			"State() = %q, want %q",
			reservation.State(),
			wantState,
		)
	}
}

func newValidReservation(t *testing.T) Reservation {
	t.Helper()

	createdAt := testReservationNow()

	reservation, err := NewReservation(
		uuid.New(),
		uuid.New(),
		uuid.New(),
		2,
		createdAt,
		createdAt.Add(5*time.Minute),
	)
	if err != nil {
		t.Fatalf("NewReservation() error = %v", err)
	}

	return reservation
}

func testReservationNow() time.Time {
	return time.Date(
		2026,
		time.August,
		15,
		12,
		0,
		0,
		0,
		time.UTC,
	)
}
