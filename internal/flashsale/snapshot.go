package flashsale

import (
	"time"

	"github.com/google/uuid"
)

type OrderSnapshot struct {
	ID            uuid.UUID
	ReservationID uuid.UUID
	UserID        uuid.UUID
	SaleItemID    uuid.UUID
	Quantity      int
	CreatedAt     time.Time
}

type ReservationSnapshot struct {
	ID         uuid.UUID
	UserID     uuid.UUID
	SaleItemID uuid.UUID
	Quantity   int
	State      ReservationState
	CreatedAt  time.Time
	ExpiresAt  time.Time
}

type SaleSnapshot struct {
	ID        uuid.UUID
	State     SaleState
	StartsAt  time.Time
	EndsAt    time.Time
	CreatedAt time.Time
	Items     []SaleItemSnapshot
}

type SaleItemSnapshot struct {
	ID          uuid.UUID
	SaleID      uuid.UUID
	ProductID   uuid.UUID
	Name        string
	PriceMinor  int64
	TotalQty    int
	ReservedQty int
	SoldQty     int
}
