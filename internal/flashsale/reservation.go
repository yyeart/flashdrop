package flashsale

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

type ReservationState string

const (
	PendingState   ReservationState = "pending"
	PaidState      ReservationState = "paid"
	CancelledState ReservationState = "cancelled"
	ExpiredState   ReservationState = "expired"
)

type Reservation struct {
	id         uuid.UUID
	userID     uuid.UUID
	saleItemID uuid.UUID
	qty        int
	state      ReservationState
	createdAt  time.Time
	expiresAt  time.Time
}

func NewReservation(
	id, userID, saleItemID uuid.UUID,
	qty int,
	createdAt, expiresAt time.Time,
) (Reservation, error) {
	if id == uuid.Nil {
		return Reservation{}, fmt.Errorf(
			"reservation id is empty: %w",
			ErrInvalidConfiguration,
		)
	}

	if userID == uuid.Nil {
		return Reservation{}, fmt.Errorf(
			"user id is empty: %w",
			ErrInvalidConfiguration,
		)
	}

	if saleItemID == uuid.Nil {
		return Reservation{}, fmt.Errorf(
			"sale_item id is empty: %w",
			ErrInvalidConfiguration,
		)
	}

	if qty <= 0 {
		return Reservation{}, fmt.Errorf(
			"qty must be > 0: %w",
			ErrInvalidQuantity,
		)
	}

	if !createdAt.Before(expiresAt) {
		return Reservation{}, fmt.Errorf(
			"created_at must be before expires_at: %w",
			ErrInvalidConfiguration,
		)
	}

	return Reservation{
		id:         id,
		userID:     userID,
		saleItemID: saleItemID,
		qty:        qty,
		state:      PendingState,
		createdAt:  createdAt,
		expiresAt:  expiresAt,
	}, nil
}

func (r *Reservation) ID() uuid.UUID {
	return r.id
}

func (r *Reservation) UserID() uuid.UUID {
	return r.userID
}

func (r *Reservation) SaleItemID() uuid.UUID {
	return r.saleItemID
}

func (r *Reservation) Quantity() int {
	return r.qty
}

func (r *Reservation) State() ReservationState {
	return r.state
}

func (r *Reservation) CreatedAt() time.Time {
	return r.createdAt
}

func (r *Reservation) ExpiresAt() time.Time {
	return r.expiresAt
}

func (r *Reservation) Pay(now time.Time) error {
	if r.state != PendingState {
		return fmt.Errorf("state is not pending: %w", ErrForbiddenTransition)
	}

	if !now.Before(r.expiresAt) {
		return fmt.Errorf("reservation is expired: %w", ErrExpiredTimeWindow)
	}

	r.state = PaidState

	return nil
}

func (r *Reservation) Cancel(now time.Time) error {
	if r.state != PendingState {
		return fmt.Errorf("state is not pending: %w", ErrForbiddenTransition)
	}

	if !now.Before(r.expiresAt) {
		return fmt.Errorf("reservation is expired: %w", ErrExpiredTimeWindow)
	}

	r.state = CancelledState

	return nil
}

func (r *Reservation) Expire(now time.Time) error {
	if r.state != PendingState {
		return fmt.Errorf("state is not pending: %w", ErrForbiddenTransition)
	}

	if now.Before(r.expiresAt) {
		return fmt.Errorf("reservation is not expired: %w", ErrForbiddenTransition)
	}

	r.state = ExpiredState

	return nil
}
