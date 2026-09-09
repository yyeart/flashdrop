package flashsale_postgres

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/yyeart/flashdrop/internal/flashsale"
)

func (s *Store) ListActiveSales(
	ctx context.Context,
	limit int,
	offset int,
) ([]flashsale.Sale, error) {
	if limit <= 0 || offset < 0 {
		return nil, fmt.Errorf(
			"invalid pagination: limit=%d, offset=%d: %w",
			limit, offset, flashsale.ErrInvalidPagination,
		)
	}

	query := `
		WITH paged_sales AS (
			SELECT
				id, state, starts_at, ends_at, created_at
			FROM flashdrop.sales
			WHERE state = 'active'
			ORDER BY created_at DESC, id ASC
			LIMIT $1
			OFFSET $2
		)
		SELECT
			s.id, s.state, s.starts_at, s.ends_at, s.created_at,
			si.id, si.sale_id, si.product_id, si.name, si.price_minor,
			si.total_qty, si.reserved_qty, si.sold_qty
		FROM paged_sales AS s
		LEFT JOIN flashdrop.sale_items AS si
			ON si.sale_id = s.id
		ORDER BY
			s.created_at DESC,
			s.id ASC,
			si.id ASC;
	`

	rows, err := s.pool.Query(ctx, query, limit, offset)
	if err != nil {
		return nil, mapDatabaseError("select sales", err)
	}
	defer rows.Close()

	saleSnapshots := make([]flashsale.SaleSnapshot, 0)

	for rows.Next() {
		saleSnapshots, err = scanSnapshot(rows, saleSnapshots)
		if err != nil {
			return nil, err
		}
	}

	if err := rows.Err(); err != nil {
		return nil, mapDatabaseError("iterate active sales", err)
	}

	sales := make([]flashsale.Sale, 0, len(saleSnapshots))
	for _, saleSnapshot := range saleSnapshots {
		sale, err := flashsale.RehydrateSale(saleSnapshot)
		if err != nil {
			return nil, fmt.Errorf(
				"rehydrate sale %s: %w",
				saleSnapshot.ID, err,
			)
		}

		sales = append(sales, sale)
	}

	return sales, nil
}

func (s *Store) FindActiveSale(
	ctx context.Context,
	saleID uuid.UUID,
) (flashsale.Sale, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return flashsale.Sale{}, fmt.Errorf(
			"begin transaction: %w", err,
		)
	}
	defer func() {
		tx.Rollback(ctx) //nolint:errcheck // rollback is best effort after the operation result is known
	}()

	saleSnapshot, err := selectSaleSnapshot(
		ctx, tx, saleID, selectSaleOptions{
			ActiveOnly: true,
			ForUpdate:  false,
		},
	)
	if err != nil {
		return flashsale.Sale{}, err
	}

	sale, err := flashsale.RehydrateSale(saleSnapshot)
	if err != nil {
		return flashsale.Sale{}, fmt.Errorf("rehydrate sale: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return flashsale.Sale{}, fmt.Errorf("transaction commit: %w", err)
	}

	return sale, nil
}

func scanSnapshot(
	rows pgx.Rows,
	saleSnapshots []flashsale.SaleSnapshot,
) ([]flashsale.SaleSnapshot, error) {
	var saleSnapshot flashsale.SaleSnapshot

	var itemID pgtype.UUID
	var itemSaleID pgtype.UUID
	var productID pgtype.UUID
	var itemName pgtype.Text
	var priceMinor pgtype.Int8
	var totalQty pgtype.Int4
	var reservedQty pgtype.Int4
	var soldQty pgtype.Int4

	err := rows.Scan(
		&saleSnapshot.ID, &saleSnapshot.State,
		&saleSnapshot.StartsAt, &saleSnapshot.EndsAt, &saleSnapshot.CreatedAt,

		&itemID, &itemSaleID, &productID, &itemName,
		&priceMinor, &totalQty, &reservedQty, &soldQty,
	)
	if err != nil {
		return nil, mapDatabaseError("scan active sales", err)
	}

	if len(saleSnapshots) == 0 ||
		saleSnapshots[len(saleSnapshots)-1].ID != saleSnapshot.ID {
		saleSnapshot.Items = make([]flashsale.SaleItemSnapshot, 0)
		saleSnapshots = append(saleSnapshots, saleSnapshot)
	}

	if itemID.Valid {
		if !itemSaleID.Valid ||
			!productID.Valid ||
			!itemName.Valid ||
			!priceMinor.Valid ||
			!totalQty.Valid ||
			!reservedQty.Valid ||
			!soldQty.Valid {
			return nil, fmt.Errorf(
				"sale item %s contains unexpected NULL columns",
				uuid.UUID(itemID.Bytes),
			)
		}

		itemSnapshot := flashsale.SaleItemSnapshot{
			ID:          uuid.UUID(itemID.Bytes),
			SaleID:      uuid.UUID(itemSaleID.Bytes),
			ProductID:   uuid.UUID(productID.Bytes),
			Name:        itemName.String,
			PriceMinor:  priceMinor.Int64,
			TotalQty:    int(totalQty.Int32),
			ReservedQty: int(reservedQty.Int32),
			SoldQty:     int(soldQty.Int32),
		}

		lastIndex := len(saleSnapshots) - 1
		saleSnapshots[lastIndex].Items = append(
			saleSnapshots[lastIndex].Items,
			itemSnapshot,
		)
	}

	return saleSnapshots, nil
}
