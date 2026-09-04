package flashsale_postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/yyeart/flashdrop/internal/flashsale"
)

type ReserveCommand struct {
	Reservation    flashsale.Reservation
	IdempotencyKey string
}

type ReserveResult struct {
	Reservation flashsale.Reservation
	Replayed    bool
}

type reservePayload struct {
	UserID     uuid.UUID `json:"user_id"`
	SaleItemID uuid.UUID `json:"sale_item_id"`
	Quantity   int       `json:"quantity"`
	ExpiresAt  time.Time `json:"expires_at"`
}

type storedRequestResult struct {
	Reservation flashsale.ReservationSnapshot `json:"reservation"`
}

type idempotencyRecordResult struct {
	id          uuid.UUID
	reservation flashsale.Reservation
	replayed    bool
}

func (s *Store) Reserve(
	ctx context.Context,
	cmd ReserveCommand,
	now time.Time,
) (ReserveResult, error) {
	reservationSnapshot, err := validateReserveReservation(cmd.Reservation)
	if err != nil {
		return ReserveResult{}, err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return ReserveResult{}, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx) //nolint:errcheck // rollback is best effort after the operation result is known
	}()

	requestHash, err := hashReserveCommand(cmd)
	if err != nil {
		return ReserveResult{}, fmt.Errorf("hash error: %w", err)
	}

	idempotency, err := registerIdempotencyRecord(
		ctx, tx, cmd, reservationSnapshot, requestHash, now,
	)
	if err != nil {
		return ReserveResult{}, err
	}

	if idempotency.replayed {
		if err := tx.Commit(ctx); err != nil {
			return ReserveResult{}, fmt.Errorf("transaction commit: %w", err)
		}

		return ReserveResult{
			Reservation: idempotency.reservation,
			Replayed:    true,
		}, nil
	}

	if err := reserveStock(ctx, tx, reservationSnapshot, now); err != nil {
		return ReserveResult{}, err
	}

	if err := persistReservationAndResult(ctx, tx, reservationSnapshot, idempotency.id); err != nil {
		return ReserveResult{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return ReserveResult{}, fmt.Errorf("transaction commit: %w", err)
	}

	return ReserveResult{
		Reservation: cmd.Reservation,
		Replayed:    false,
	}, nil
}

func validateReserveReservation(
	reservation flashsale.Reservation,
) (flashsale.ReservationSnapshot, error) {
	snapshot := flashsale.ReservationSnapshot{
		ID:         reservation.ID(),
		UserID:     reservation.UserID(),
		SaleItemID: reservation.SaleItemID(),
		Quantity:   reservation.Quantity(),
		State:      reservation.State(),
		CreatedAt:  reservation.CreatedAt(),
		ExpiresAt:  reservation.ExpiresAt(),
	}

	if _, err := flashsale.RehydrateReservation(snapshot); err != nil {
		return flashsale.ReservationSnapshot{}, fmt.Errorf("reservation validation: %w", err)
	}

	if reservation.State() != flashsale.PendingState {
		return flashsale.ReservationSnapshot{}, fmt.Errorf(
			"reservation state must be pending: %w",
			flashsale.ErrForbiddenTransition,
		)
	}

	return snapshot, nil
}

func registerIdempotencyRecord(
	ctx context.Context,
	tx pgx.Tx,
	cmd ReserveCommand,
	reservationSnapshot flashsale.ReservationSnapshot,
	requestHash string,
	now time.Time,
) (idempotencyRecordResult, error) {
	insertIdempotencyRecordQuery := `
		INSERT INTO flashdrop.idempotency_records
			(id, user_id, idempotency_key, request_hash, response_result, created_at)
		VALUES
			($1, $2, $3, $4, '{}'::jsonb, $5)
		ON CONFLICT (user_id, idempotency_key) DO NOTHING
		RETURNING id;
	`

	recordID := uuid.New()
	if err := tx.QueryRow(
		ctx, insertIdempotencyRecordQuery,
		recordID, reservationSnapshot.UserID,
		cmd.IdempotencyKey, requestHash,
		now,
	).Scan(&recordID); err == nil {
		return idempotencyRecordResult{id: recordID}, nil
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return idempotencyRecordResult{}, mapDatabaseError("scan idempotency record id", err)
	}

	reservation, err := handleExistingRecord(
		ctx, tx, cmd.IdempotencyKey, reservationSnapshot.UserID, requestHash,
	)
	if err != nil {
		return idempotencyRecordResult{}, err
	}

	return idempotencyRecordResult{
		reservation: reservation,
		replayed:    true,
	}, nil
}

func reserveStock(
	ctx context.Context,
	tx pgx.Tx,
	reservationSnapshot flashsale.ReservationSnapshot,
	now time.Time,
) error {
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
	if err := tx.QueryRow(
		ctx, reserveQuery,
		reservationSnapshot.Quantity,
		reservationSnapshot.SaleItemID,
		now,
	).Scan(&updatedItemID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return flashsale.ErrReservationUnavailable
		}

		return mapDatabaseError("reserve stock", err)
	}

	return nil
}

func persistReservationAndResult(
	ctx context.Context,
	tx pgx.Tx,
	reservationSnapshot flashsale.ReservationSnapshot,
	idempotencyRecordID uuid.UUID,
) error {
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

	storedResult := storedRequestResult{Reservation: reservationSnapshot}
	rawResult, err := json.Marshal(storedResult)
	if err != nil {
		return fmt.Errorf("marshal idempotency result: %w", err)
	}

	updateIdempotencyRecordQuery := `
		UPDATE flashdrop.idempotency_records
		SET response_result = $1
		WHERE id = $2;
	`
	tag, err := tx.Exec(ctx, updateIdempotencyRecordQuery, rawResult, idempotencyRecordID)
	if err != nil {
		return fmt.Errorf("update idempotency result: %w", err)
	}

	if tag.RowsAffected() != 1 {
		return fmt.Errorf(
			"update idempotency result: expected one row, affected %d",
			tag.RowsAffected(),
		)
	}

	return nil
}

func hashReserveCommand(cmd ReserveCommand) (string, error) {
	if cmd.IdempotencyKey == "" {
		return "", fmt.Errorf(
			"idempotency key cant be empty: %w",
			flashsale.ErrInvalidConfiguration,
		)
	}

	payload := reservePayload{
		UserID:     cmd.Reservation.UserID(),
		SaleItemID: cmd.Reservation.SaleItemID(),
		Quantity:   cmd.Reservation.Quantity(),
		ExpiresAt:  cmd.Reservation.ExpiresAt().UTC(),
	}

	raw, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal reserve payload: %w", err)
	}

	sum := sha256.Sum256(raw)

	return hex.EncodeToString(sum[:]), nil
}

func handleExistingRecord(
	ctx context.Context,
	tx pgx.Tx,
	idempotencyKey string,
	userID uuid.UUID,
	requestHash string,
) (flashsale.Reservation, error) {
	var (
		recordRequestHash string
		rawResult         []byte
	)
	selectIdempotencyRecordQuery := `
		SELECT request_hash, response_result
		FROM flashdrop.idempotency_records
		WHERE user_id = $1 AND idempotency_key = $2
		FOR UPDATE;
	`

	if err := tx.QueryRow(
		ctx, selectIdempotencyRecordQuery,
		userID, idempotencyKey,
	).Scan(&recordRequestHash, &rawResult); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return flashsale.Reservation{}, fmt.Errorf(
				"record with user_id %s not found: %w",
				userID, flashsale.ErrIdempotencyRecordNotFound,
			)
		}

		return flashsale.Reservation{}, mapDatabaseError("scan idempotency record", err)
	}

	if recordRequestHash != requestHash {
		return flashsale.Reservation{}, fmt.Errorf(
			"request hash mismatch: %w", flashsale.ErrConflict,
		)
	}

	var stored storedRequestResult
	if err := json.Unmarshal(rawResult, &stored); err != nil {
		return flashsale.Reservation{}, fmt.Errorf(
			"decode idempotency record: %w",
			err,
		)
	}

	reservation, err := flashsale.RehydrateReservation(stored.Reservation)
	if err != nil {
		return flashsale.Reservation{}, fmt.Errorf("rehydrate idempotency result: %w", err)
	}

	return reservation, nil
}
