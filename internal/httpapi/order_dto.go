package httpapi

import (
	"time"

	"github.com/google/uuid"
	"github.com/yyeart/flashdrop/internal/flashsale"
)

type orderResponse struct {
	ID            uuid.UUID `json:"id"`
	ReservationID uuid.UUID `json:"reservation_id"`
	UserID        uuid.UUID `json:"user_id"`
	SaleItemID    uuid.UUID `json:"sale_item_id"`
	Quantity      int       `json:"quantity"`
	CreatedAt     time.Time `json:"created_at"`
}

func orderToResponse(order flashsale.Order) orderResponse {
	return orderResponse{
		ID:            order.ID(),
		ReservationID: order.ReservationID(),
		UserID:        order.UserID(),
		SaleItemID:    order.SaleItemID(),
		Quantity:      order.Quantity(),
		CreatedAt:     order.CreatedAt(),
	}
}
