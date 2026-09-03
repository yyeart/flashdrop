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

func (s *Store) Reserve(
	ctx context.Context,
	reservation flashsale.Reservation,
	now time.Time,
) error {
	reservationSnapshot := flashsale.ReservationSnapshot{
		ID:         reservation.ID(),
		UserID:     reservation.UserID(),
		SaleItemID: reservation.SaleItemID(),
		Quantity:   reservation.Quantity(),
		State:      reservation.State(),
		CreatedAt:  reservation.CreatedAt(),
		ExpiresAt:  reservation.ExpiresAt(),
	}

	if _, err := flashsale.RehydrateReservation(reservationSnapshot); err != nil {
		return fmt.Errorf("reservation validation: %w", err)
	}

	if reservation.State() != flashsale.PendingState {
		return fmt.Errorf(
			"reservation state must be pending: %w",
			flashsale.ErrForbiddenTransition,
		)
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx) //nolint:errcheck // rollback is best effort after the operation result is known
	}()

	reserveQuery := `
		UPDATE flashdrop.sale_items AS si
		SET reserved_qty = si.reserved_qty + $1
		FROM flashdrop.sales AS s
		WHERE si.id = $2
			AND s.id = si.sale_id
			AND s.state = 'active'
			AND $3 >= s.starts_at
			AND $3 < s.ends_at
			AND si.total_qty - si.reserved_qty - si.sold_qty >= $1
		RETURNING si.id;
	`

	var updatedItemID uuid.UUID
	err = tx.QueryRow(
		ctx, reserveQuery,
		reservationSnapshot.Quantity,
		reservationSnapshot.SaleItemID,
		now,
	).Scan(&updatedItemID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return flashsale.ErrReservationUnavailable
		}

		return mapDatabaseError("reserve stock", err)
	}

	insertReservationQuery := `
		INSERT INTO flashdrop.reservations 
		(id, user_id, sale_item_id, quantity, state, created_at, expires_at) 
		VALUES ($1, $2, $3, $4, $5, $6, $7);
	`
	if _, err := tx.Exec(
		ctx, insertReservationQuery,
		reservationSnapshot.ID,
		reservationSnapshot.UserID,
		reservationSnapshot.SaleItemID,
		reservationSnapshot.Quantity,
		flashsale.PendingState,
		reservationSnapshot.CreatedAt,
		reservationSnapshot.ExpiresAt,
	); err != nil {
		return mapDatabaseError("insert reservation", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("transaction commit: %w", err)
	}

	return nil
}
