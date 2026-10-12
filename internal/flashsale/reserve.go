package flashsale

import (
	"time"

	"github.com/google/uuid"
)

type ReserveCommand struct {
	ReservationID uuid.UUID
	UserID        uuid.UUID
	SaleItemID    uuid.UUID
	Quantity      int
	ExpiresAt     time.Time

	IdempotencyKey string
}

type ReserveResult struct {
	Reservation Reservation
	Replayed    bool
}
