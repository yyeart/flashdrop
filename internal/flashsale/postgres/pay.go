package flashsale_postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/yyeart/flashdrop/internal/flashsale"
)

func (s *Store) Pay(
	ctx context.Context,
	userID uuid.UUID,
	reservationID uuid.UUID,
	orderID uuid.UUID,
	now time.Time,
) (flashsale.Order, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return flashsale.Order{}, fmt.Errorf(
			"begin transaction: %w", err,
		)
	}
	defer func() {
		_ = tx.Rollback(ctx) //nolint:errcheck // rollback is best effort after the operation result is known
	}()

	reservation, err := selectReservationForUser(ctx, tx, reservationID, userID)
	if err != nil {
		return flashsale.Order{}, err
	}

	if err := reservation.Pay(now); err != nil {
		return flashsale.Order{}, fmt.Errorf("pay error: %w", err)
	}

	updateStockQuery := `
		UPDATE flashdrop.sale_items
		SET reserved_qty = reserved_qty - $1,
			sold_qty = sold_qty + $1
		WHERE id = $2 AND reserved_qty >= $1;
	`
	tag, err := tx.Exec(
		ctx, updateStockQuery,
		reservation.Quantity(), reservation.SaleItemID(),
	)
	if err != nil {
		return flashsale.Order{}, mapDatabaseError("update stock", err)
	}
	if tag.RowsAffected() == 0 {
		return flashsale.Order{}, fmt.Errorf(
			"sale item with id %s not found: %w",
			reservation.SaleItemID(), flashsale.ErrSaleItemNotFound,
		)
	}

	updateReservationQuery := `
		UPDATE flashdrop.reservations
		SET state = 'paid'
		WHERE id = $1;
	`
	if _, err := tx.Exec(ctx, updateReservationQuery, reservation.ID()); err != nil {
		return flashsale.Order{}, mapDatabaseError("update reservation", err)
	}

	insertOrderQuery := `
		INSERT INTO flashdrop.orders (
			id, reservation_id, user_id,
			sale_item_id, quantity, created_at
		)
		VALUES ($1, $2, $3, $4, $5, $6);
	`
	if _, err := tx.Exec(
		ctx, insertOrderQuery,
		orderID, reservation.ID(),
		reservation.UserID(),
		reservation.SaleItemID(),
		reservation.Quantity(),
		now,
	); err != nil {
		return flashsale.Order{}, mapDatabaseError("insert order", err)
	}

	order, err := flashsale.NewOrder(flashsale.NewOrderInput{
		ReservationID: reservation.ID(),
		UserID:        reservation.UserID(),
		SaleItemID:    reservation.SaleItemID(),
		Quantity:      reservation.Quantity(),
		ID:            orderID,
		CreatedAt:     now,
	})
	if err != nil {
		return flashsale.Order{}, fmt.Errorf("create order: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return flashsale.Order{}, fmt.Errorf("transaction commit: %w", err)
	}

	return order, nil
}
