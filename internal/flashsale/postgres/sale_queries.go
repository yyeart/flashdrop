package flashsale_postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/yyeart/flashdrop/internal/flashsale"
)

type selectSaleOptions struct {
	ActiveOnly bool
	ForUpdate  bool
}

func selectSaleSnapshot(
	ctx context.Context,
	tx pgx.Tx,
	saleID uuid.UUID,
	options selectSaleOptions,
) (flashsale.SaleSnapshot, error) {
	query := `
		SELECT
			id, state, starts_at, ends_at, created_at
		FROM flashdrop.sales
		WHERE id = $1
	`

	args := []any{saleID}

	if options.ActiveOnly {
		query += ` AND state = $2`
		args = append(args, string(flashsale.ActiveState))
	}

	if options.ForUpdate {
		query += ` FOR UPDATE`
	}

	query += `;`

	var saleSnapshot flashsale.SaleSnapshot
	if err := tx.QueryRow(ctx, query, args...).Scan(
		&saleSnapshot.ID, &saleSnapshot.State,
		&saleSnapshot.StartsAt, &saleSnapshot.EndsAt,
		&saleSnapshot.CreatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return flashsale.SaleSnapshot{}, fmt.Errorf(
				"sale with id %s not found: %w",
				saleID, flashsale.ErrSaleNotFound,
			)
		}

		return flashsale.SaleSnapshot{}, mapDatabaseError("scan sale", err)
	}

	saleItemsSnapshots, err := selectSaleItemsSnapshots(ctx, tx, saleID)
	if err != nil {
		return flashsale.SaleSnapshot{}, err
	}

	saleSnapshot.Items = saleItemsSnapshots

	return saleSnapshot, nil
}

func selectSaleItemsSnapshots(
	ctx context.Context,
	tx pgx.Tx,
	saleID uuid.UUID,
) ([]flashsale.SaleItemSnapshot, error) {
	saleItemsSnapshots := make([]flashsale.SaleItemSnapshot, 0)

	query := `
		SELECT
			id, sale_id, product_id, name, price_minor,
			total_qty, reserved_qty, sold_qty
		FROM flashdrop.sale_items
		WHERE sale_id = $1
		ORDER BY id ASC;
	`
	rows, err := tx.Query(ctx, query, saleID)
	if err != nil {
		return nil, mapDatabaseError("query error", err)
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
			return nil, mapDatabaseError("scan sale item", err)
		}

		saleItemsSnapshots = append(saleItemsSnapshots, saleItemSnapshot)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}

	return saleItemsSnapshots, nil
}
