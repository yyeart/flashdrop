package flashsale_postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/yyeart/flashdrop/internal/flashsale"
)

func (s *Store) ActivateSale(
	ctx context.Context,
	saleID uuid.UUID,
	now time.Time,
) (flashsale.Sale, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return flashsale.Sale{}, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx) //nolint:errcheck // rollback is best effort after the operation result is known
	}()

	saleSnapshot, err := selectSaleSnapshot(ctx, tx, saleID, selectSaleOptions{
		ForUpdate: true,
	})
	if err != nil {
		return flashsale.Sale{}, err
	}

	sale, err := flashsale.RehydrateSale(saleSnapshot)
	if err != nil {
		return flashsale.Sale{}, fmt.Errorf(
			"sale validation: %w", err,
		)
	}

	if err := sale.Activate(now); err != nil {
		return flashsale.Sale{}, fmt.Errorf("activate error: %w", err)
	}

	updateSaleQuery := `
		UPDATE flashdrop.sales
		SET state = 'active'
		WHERE id = $1;
	`

	if _, err := tx.Exec(
		ctx, updateSaleQuery, saleID,
	); err != nil {
		return flashsale.Sale{}, mapDatabaseError("update sale state", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return flashsale.Sale{}, fmt.Errorf("transaction commit: %w", err)
	}

	return sale, nil
}

func (s *Store) EndSale(
	ctx context.Context,
	saleID uuid.UUID,
) (flashsale.Sale, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return flashsale.Sale{}, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx) //nolint:errcheck // rollback is best effort after the operation result is known
	}()

	saleSnapshot, err := selectSaleSnapshot(ctx, tx, saleID, selectSaleOptions{
		ForUpdate: true,
	})
	if err != nil {
		return flashsale.Sale{}, err
	}

	sale, err := flashsale.RehydrateSale(saleSnapshot)
	if err != nil {
		return flashsale.Sale{}, fmt.Errorf("sale validation: %w", err)
	}

	if err := sale.End(); err != nil {
		return flashsale.Sale{}, fmt.Errorf(
			"end error: %w", err,
		)
	}

	updateSaleQuery := `
		UPDATE flashdrop.sales
		SET state = 'ended'
		WHERE id = $1;
	`

	if _, err := tx.Exec(ctx, updateSaleQuery, saleID); err != nil {
		return flashsale.Sale{}, fmt.Errorf("update sale state: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return flashsale.Sale{}, fmt.Errorf("transaction commit: %w", err)
	}

	return sale, nil
}
