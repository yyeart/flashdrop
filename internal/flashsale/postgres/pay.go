package flashsale_postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/yyeart/flashdrop/internal/flashsale"
)

func (s *Store) Pay(
	ctx context.Context,
	reservationID uuid.UUID,
	orderID uuid.UUID,
	now time.Time,
) (flashsale.Order, error) {
	var reservationSnapshot flashsale.ReservationSnapshot
	selectReservationQuery := `
		SELECT
			id, user_id, sale_item_id,
			quantity, state,
			created_at, expires_at
		FROM flashdrop.reservations
		WHERE id = $1
		FOR UPDATE;
	`

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return flashsale.Order{}, fmt.Errorf(
			"begin transaction: %w", err,
		)
	}
	defer func() {
		_ = tx.Rollback(ctx) //nolint:errcheck // rollback is best effort after the operation result is known
	}()

	err = tx.QueryRow(ctx, selectReservationQuery, reservationID).Scan(
		&reservationSnapshot.ID,
		&reservationSnapshot.UserID,
		&reservationSnapshot.SaleItemID,
		&reservationSnapshot.Quantity,
		&reservationSnapshot.State,
		&reservationSnapshot.CreatedAt,
		&reservationSnapshot.ExpiresAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return flashsale.Order{}, fmt.Errorf(
				"reservation with id %s not found: %w",
				reservationID, flashsale.ErrReservationNotFound,
			)
		}

		return flashsale.Order{}, mapDatabaseError("scan reservation", err)
	}

	reservation, err := flashsale.RehydrateReservation(reservationSnapshot)
	if err != nil {
		return flashsale.Order{}, fmt.Errorf(
			"reservation validation: %w", err,
		)
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
		reservationSnapshot.Quantity, reservationSnapshot.SaleItemID,
	)
	if err != nil {
		return flashsale.Order{}, mapDatabaseError("update stock", err)
	}
	if tag.RowsAffected() == 0 {
		return flashsale.Order{}, fmt.Errorf(
			"sale item with id %s not found: %w",
			reservationSnapshot.SaleItemID, flashsale.ErrSaleItemNotFound,
		)
	}

	updateReservationQuery := `
		UPDATE flashdrop.reservations
		SET state = 'paid'
		WHERE id = $1;
	`
	if _, err := tx.Exec(ctx, updateReservationQuery, reservationSnapshot.ID); err != nil {
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
		orderID, reservationSnapshot.ID,
		reservationSnapshot.UserID,
		reservationSnapshot.SaleItemID,
		reservationSnapshot.Quantity,
		now,
	); err != nil {
		return flashsale.Order{}, mapDatabaseError("insert order", err)
	}

	order, err := flashsale.NewOrder(flashsale.NewOrderInput{
		ReservationID: reservationSnapshot.ID,
		UserID:        reservationSnapshot.UserID,
		SaleItemID:    reservationSnapshot.SaleItemID,
		Quantity:      reservationSnapshot.Quantity,
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
