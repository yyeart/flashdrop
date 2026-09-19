package httpapi

import (
	"time"

	"github.com/google/uuid"
	"github.com/yyeart/flashdrop/internal/flashsale"
)

type reserveRequest struct {
	SaleItemID uuid.UUID `json:"sale_item_id"`
	Quantity   int       `json:"quantity"`
}

type reservationResponse struct {
	ID         uuid.UUID                  `json:"id"`
	UserID     uuid.UUID                  `json:"user_id"`
	SaleItemID uuid.UUID                  `json:"sale_item_id"`
	Quantity   int                        `json:"quantity"`
	State      flashsale.ReservationState `json:"state"`
	CreatedAt  time.Time                  `json:"created_at"`
	ExpiresAt  time.Time                  `json:"expires_at"`
}

func reservationToResponse(
	reservation flashsale.Reservation,
) reservationResponse {
	return reservationResponse{
		ID:         reservation.ID(),
		UserID:     reservation.UserID(),
		SaleItemID: reservation.SaleItemID(),
		Quantity:   reservation.Quantity(),
		State:      reservation.State(),
		CreatedAt:  reservation.CreatedAt(),
		ExpiresAt:  reservation.ExpiresAt(),
	}
}
