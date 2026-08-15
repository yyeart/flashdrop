package flashsale

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

type Order struct {
	id            uuid.UUID
	reservationID uuid.UUID
	userID        uuid.UUID
	saleItemID    uuid.UUID
	qty           int
	createdAt     time.Time
}

func (o *Order) ID() uuid.UUID {
	return o.id
}

func (o *Order) ReservationID() uuid.UUID {
	return o.reservationID
}

func (o *Order) UserID() uuid.UUID {
	return o.userID
}

func (o *Order) SaleItemID() uuid.UUID {
	return o.saleItemID
}

func (o *Order) Quantity() int {
	return o.qty
}

func (o *Order) CreatedAt() time.Time {
	return o.createdAt
}

func newOrder(
	id, reservationID, userID, saleItemID uuid.UUID,
	qty int,
	createdAt time.Time,
) (Order, error) {
	if id == uuid.Nil {
		return Order{}, fmt.Errorf("order id is empty: %w", ErrInvalidConfiguration)
	}

	if reservationID == uuid.Nil {
		return Order{}, fmt.Errorf("reservation id is empty: %w", ErrInvalidConfiguration)
	}

	if userID == uuid.Nil {
		return Order{}, fmt.Errorf("user id is empty: %w", ErrInvalidConfiguration)
	}

	if saleItemID == uuid.Nil {
		return Order{}, fmt.Errorf("sale item id is empty: %w", ErrInvalidConfiguration)
	}

	if qty <= 0 {
		return Order{}, fmt.Errorf("qty must be > 0: %w", ErrInvalidQuantity)
	}

	if createdAt.IsZero() {
		return Order{}, fmt.Errorf("created_at cannot be zero: %w", ErrInvalidConfiguration)
	}

	return Order{
		id:            id,
		reservationID: reservationID,
		userID:        userID,
		saleItemID:    saleItemID,
		qty:           qty,
		createdAt:     createdAt,
	}, nil
}
