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
	ReservationID uuid.UUID
	UserID        uuid.UUID
	SaleItemID    uuid.UUID
	Quantity      int
	ExpiresAt     time.Time

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
	if err := validateReserveCommand(cmd); err != nil {
		return ReserveResult{}, err
	}

	now = now.UTC()

	requestHash, err := hashReserveCommand(cmd)
	if err != nil {
		return ReserveResult{}, fmt.Errorf("hash error: %w", err)
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return ReserveResult{}, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx) //nolint:errcheck // rollback is best effort after the operation result is known
	}()

	idempotency, err := registerIdempotencyRecord(
		ctx, tx,
		cmd.UserID, cmd.IdempotencyKey,
		requestHash, now,
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

	reservationSnapshot := flashsale.ReservationSnapshot{
		ID:         cmd.ReservationID,
		UserID:     cmd.UserID,
		SaleItemID: cmd.SaleItemID,
		Quantity:   cmd.Quantity,
		State:      flashsale.PendingState,
		CreatedAt:  now,
		ExpiresAt:  cmd.ExpiresAt.UTC(),
	}

	reservation, err := flashsale.RehydrateReservation(reservationSnapshot)
	if err != nil {
		return ReserveResult{}, fmt.Errorf(
			"reservation validation: %w", err,
		)
	}

	if err := reserveStock(ctx, tx, reservationSnapshot, now); err != nil {
		return ReserveResult{}, err
	}

	if err := persistReservationAndResult(
		ctx, tx,
		reservationSnapshot, idempotency.id,
	); err != nil {
		return ReserveResult{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return ReserveResult{}, fmt.Errorf("transaction commit: %w", err)
	}

	return ReserveResult{
		Reservation: reservation,
		Replayed:    false,
	}, nil
}

func validateReserveCommand(cmd ReserveCommand) error {
	if cmd.UserID == uuid.Nil {
		return fmt.Errorf("user_id is nil: %w", flashsale.ErrInvalidConfiguration)
	}

	if cmd.ReservationID == uuid.Nil {
		return fmt.Errorf("reservation_id is nil: %w", flashsale.ErrInvalidConfiguration)
	}

	if cmd.SaleItemID == uuid.Nil {
		return fmt.Errorf("sale_item_id is nil: %w", flashsale.ErrInvalidConfiguration)
	}

	if cmd.Quantity <= 0 {
		return fmt.Errorf("quantity must be > 0: %w", flashsale.ErrInvalidQuantity)
	}

	if cmd.IdempotencyKey == "" {
		return fmt.Errorf("idempotency_key cannot be empty: %w", flashsale.ErrInvalidConfiguration)
	}

	if cmd.ExpiresAt.IsZero() {
		return fmt.Errorf("expires_at cannot be zero: %w", flashsale.ErrInvalidConfiguration)
	}

	return nil
}

func registerIdempotencyRecord(
	ctx context.Context,
	tx pgx.Tx,
	userID uuid.UUID,
	idempotencyKey string,
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
		recordID, userID,
		idempotencyKey, requestHash,
		now,
	).Scan(&recordID); err == nil {
		return idempotencyRecordResult{id: recordID}, nil
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return idempotencyRecordResult{}, mapDatabaseError("scan idempotency record id", err)
	}

	reservation, err := handleExistingRecord(
		ctx, tx, idempotencyKey, userID, requestHash,
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
	payload := reservePayload{
		UserID:     cmd.UserID,
		SaleItemID: cmd.SaleItemID,
		Quantity:   cmd.Quantity,
		ExpiresAt:  cmd.ExpiresAt.UTC(),
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
