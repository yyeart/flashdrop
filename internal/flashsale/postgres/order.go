package flashsale_postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/yyeart/flashdrop/internal/flashsale"
)

func (s *Store) FindOrder(
	ctx context.Context,
	userID uuid.UUID,
	orderID uuid.UUID,
) (flashsale.Order, error) {
	var orderSnapshot flashsale.OrderSnapshot
	selectOrderQuery := `
		SELECT
			id, reservation_id, user_id,
			sale_item_id, quantity, created_at
		FROM flashdrop.orders 
		WHERE id = $1 AND user_id = $2;
	`

	if err := s.pool.QueryRow(ctx, selectOrderQuery, orderID, userID).Scan(
		&orderSnapshot.ID,
		&orderSnapshot.ReservationID,
		&orderSnapshot.UserID,
		&orderSnapshot.SaleItemID,
		&orderSnapshot.Quantity,
		&orderSnapshot.CreatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return flashsale.Order{}, fmt.Errorf(
				"order with id %s not found: %w",
				orderID, flashsale.ErrOrderNotFound,
			)
		}

		return flashsale.Order{}, mapDatabaseError("scan order", err)
	}

	order, err := flashsale.RehydrateOrder(orderSnapshot)
	if err != nil {
		return flashsale.Order{}, fmt.Errorf("order validation: %w", err)
	}

	return order, nil
}
