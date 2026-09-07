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

type NewOrderInput struct {
	ReservationID uuid.UUID
	UserID        uuid.UUID
	SaleItemID    uuid.UUID
	Quantity      int
	ID            uuid.UUID
	CreatedAt     time.Time
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

func NewOrder(input NewOrderInput) (Order, error) {
	if err := validateOrder(
		input.ID, input.ReservationID, input.UserID,
		input.SaleItemID, input.Quantity, input.CreatedAt,
	); err != nil {
		return Order{}, fmt.Errorf("order input validation: %w", err)
	}

	return newOrder(
		input.ID, input.ReservationID, input.UserID, input.SaleItemID,
		input.Quantity, input.CreatedAt,
	), nil
}

func RehydrateOrder(snapshot OrderSnapshot) (Order, error) {
	if err := validateOrder(
		snapshot.ID, snapshot.ReservationID, snapshot.UserID,
		snapshot.SaleItemID, snapshot.Quantity, snapshot.CreatedAt,
	); err != nil {
		return Order{}, fmt.Errorf("order snapshot validation: %w", err)
	}

	return newOrder(
		snapshot.ID, snapshot.ReservationID, snapshot.UserID, snapshot.SaleItemID,
		snapshot.Quantity, snapshot.CreatedAt,
	), nil
}

func validateOrder(
	id, reservationID, userID, saleItemID uuid.UUID,
	qty int, createdAt time.Time,
) error {
	if id == uuid.Nil {
		return fmt.Errorf("order id is empty: %w", ErrInvalidConfiguration)
	}

	if reservationID == uuid.Nil {
		return fmt.Errorf("reservation id is empty: %w", ErrInvalidConfiguration)
	}

	if userID == uuid.Nil {
		return fmt.Errorf("user id is empty: %w", ErrInvalidConfiguration)
	}

	if saleItemID == uuid.Nil {
		return fmt.Errorf("sale item id is empty: %w", ErrInvalidConfiguration)
	}

	if qty <= 0 {
		return fmt.Errorf("qty must be > 0: %w", ErrInvalidQuantity)
	}

	if createdAt.IsZero() {
		return fmt.Errorf("created_at cannot be zero: %w", ErrInvalidConfiguration)
	}

	return nil
}

func newOrder(
	id, reservationID, userID, saleItemID uuid.UUID,
	qty int,
	createdAt time.Time,
) Order {
	return Order{
		id:            id,
		reservationID: reservationID,
		userID:        userID,
		saleItemID:    saleItemID,
		qty:           qty,
		createdAt:     createdAt,
	}
}
