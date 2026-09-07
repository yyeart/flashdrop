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

func (s *Store) Cancel(
	ctx context.Context,
	reservationID uuid.UUID,
	now time.Time,
) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx) //nolint:errcheck // rollback is best effort after the operation result is known
	}()

	reservation, err := selectReservation(ctx, tx, reservationID)
	if err != nil {
		return err
	}

	if err := reservation.Cancel(now); err != nil {
		return fmt.Errorf("cancel error: %w", err)
	}

	if err := updateStockReservedQty(ctx, tx, reservation); err != nil {
		return err
	}

	if err := updateReservationState(
		ctx, tx,
		reservationID, flashsale.CancelledState,
	); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("transaction commit: %w", err)
	}

	return nil
}

func (s *Store) Expire(
	ctx context.Context,
	reservationID uuid.UUID,
	now time.Time,
) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx) //nolint:errcheck // rollback is best effort after the operation result is known
	}()

	reservation, err := selectReservation(ctx, tx, reservationID)
	if err != nil {
		return err
	}

	if err := reservation.Expire(now); err != nil {
		return fmt.Errorf("expire error: %w", err)
	}

	if err := updateStockReservedQty(ctx, tx, reservation); err != nil {
		return err
	}

	if err := updateReservationState(
		ctx, tx,
		reservationID, flashsale.ExpiredState,
	); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("transaction commit: %w", err)
	}

	return nil
}

func selectReservation(
	ctx context.Context,
	tx pgx.Tx,
	reservationID uuid.UUID,
) (flashsale.Reservation, error) {
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
	if err := tx.QueryRow(ctx, selectReservationQuery, reservationID).Scan(
		&reservationSnapshot.ID,
		&reservationSnapshot.UserID,
		&reservationSnapshot.SaleItemID,
		&reservationSnapshot.Quantity,
		&reservationSnapshot.State,
		&reservationSnapshot.CreatedAt,
		&reservationSnapshot.ExpiresAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return flashsale.Reservation{}, fmt.Errorf(
				"reservation with id %s not found: %w",
				reservationID, flashsale.ErrReservationNotFound,
			)
		}

		return flashsale.Reservation{}, mapDatabaseError("scan reservation", err)
	}

	reservation, err := flashsale.RehydrateReservation(reservationSnapshot)
	if err != nil {
		return flashsale.Reservation{}, fmt.Errorf("reservation validation: %w", err)
	}

	return reservation, nil
}

func updateStockReservedQty(
	ctx context.Context,
	tx pgx.Tx,
	reservation flashsale.Reservation,
) error {
	updateStockQuery := `
		UPDATE flashdrop.sale_items
		SET reserved_qty = reserved_qty - $1
		WHERE id = $2 AND reserved_qty >= $1;
	`

	tag, err := tx.Exec(
		ctx, updateStockQuery,
		reservation.Quantity(), reservation.SaleItemID(),
	)
	if err != nil {
		return mapDatabaseError("update stock", err)
	}

	if tag.RowsAffected() == 0 {
		return fmt.Errorf(
			"sale item with id %s not found: %w",
			reservation.SaleItemID(), flashsale.ErrSaleItemNotFound,
		)
	}

	return nil
}

func updateReservationState(
	ctx context.Context,
	tx pgx.Tx,
	reservationID uuid.UUID,
	state flashsale.ReservationState,
) error {
	updateReservationQuery := `
		UPDATE flashdrop.reservations
		SET state = $1
		WHERE id = $2;
	`

	if _, err := tx.Exec(
		ctx, updateReservationQuery, string(state), reservationID,
	); err != nil {
		return mapDatabaseError("update reservation", err)
	}

	return nil
}
