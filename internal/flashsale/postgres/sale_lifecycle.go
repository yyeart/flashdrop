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

	var saleSnapshot flashsale.SaleSnapshot
	selectSaleQuery := `
		SELECT
			id, state, starts_at, ends_at, created_at
		FROM flashdrop.sales
		WHERE id = $1
		FOR UPDATE;
	`

	if err := tx.QueryRow(
		ctx, selectSaleQuery, saleID,
	).Scan(
		&saleSnapshot.ID,
		&saleSnapshot.State,
		&saleSnapshot.StartsAt,
		&saleSnapshot.EndsAt,
		&saleSnapshot.CreatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return flashsale.Sale{}, fmt.Errorf(
				"sale with id %s not found: %w",
				saleID, flashsale.ErrSaleNotFound,
			)
		}

		return flashsale.Sale{}, mapDatabaseError("scan sale", err)
	}

	selectSaleItemsQuery := `
		SELECT 
			id, sale_id, product_id, name, price_minor, 
			total_qty, reserved_qty, sold_qty 
		FROM flashdrop.sale_items 
		WHERE sale_id = $1 
		ORDER BY id ASC;
	`
	rows, err := tx.Query(
		ctx, selectSaleItemsQuery, saleID,
	)
	if err != nil {
		return flashsale.Sale{}, fmt.Errorf("query error: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var saleItemSnapshot flashsale.SaleItemSnapshot

		if err := rows.Scan(
			&saleItemSnapshot.ID, &saleItemSnapshot.SaleID,
			&saleItemSnapshot.ProductID, &saleItemSnapshot.Name,
			&saleItemSnapshot.PriceMinor, &saleItemSnapshot.TotalQty,
			&saleItemSnapshot.ReservedQty, &saleItemSnapshot.SoldQty,
		); err != nil {
			return flashsale.Sale{}, fmt.Errorf(
				"scan item: %w", err,
			)
		}

		saleSnapshot.Items = append(saleSnapshot.Items, saleItemSnapshot)
	}

	if err := rows.Err(); err != nil {
		return flashsale.Sale{}, fmt.Errorf("rows error: %w", err)
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
