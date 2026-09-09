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
	userID uuid.UUID,
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

	reservation, err := selectReservationForUser(ctx, tx, reservationID, userID)
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
	return selectReservationForUpdate(ctx, tx, reservationID, nil)
}

func selectReservationForUser(
	ctx context.Context,
	tx pgx.Tx,
	reservationID uuid.UUID,
	userID uuid.UUID,
) (flashsale.Reservation, error) {
	return selectReservationForUpdate(ctx, tx, reservationID, &userID)
}

func selectReservationForUpdate(
	ctx context.Context,
	tx pgx.Tx,
	reservationID uuid.UUID,
	userID *uuid.UUID,
) (flashsale.Reservation, error) {
	query := `
		SELECT
			id, user_id, sale_item_id,
			quantity, state,
			created_at, expires_at
		FROM flashdrop.reservations
		WHERE id = $1
	`

	args := []any{reservationID}

	if userID != nil {
		query += ` AND user_id = $2`
		args = append(args, *userID)
	}

	query += ` FOR UPDATE;`

	var snapshot flashsale.ReservationSnapshot

	err := tx.QueryRow(ctx, query, args...).Scan(
		&snapshot.ID,
		&snapshot.UserID,
		&snapshot.SaleItemID,
		&snapshot.Quantity,
		&snapshot.State,
		&snapshot.CreatedAt,
		&snapshot.ExpiresAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return flashsale.Reservation{}, fmt.Errorf(
				"reservation with id %s not found: %w",
				reservationID,
				flashsale.ErrReservationNotFound,
			)
		}

		return flashsale.Reservation{},
			mapDatabaseError("scan reservation", err)
	}

	reservation, err := flashsale.RehydrateReservation(snapshot)
	if err != nil {
		return flashsale.Reservation{}, fmt.Errorf(
			"rehydrate reservation: %w",
			err,
		)
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
