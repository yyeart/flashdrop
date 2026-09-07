package flashsale_postgres

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/yyeart/flashdrop/internal/flashsale"
)

func (s *Store) AddSaleItem(
	ctx context.Context,
	saleID, itemID, productID uuid.UUID,
	name string,
	price flashsale.Money,
	totalQty int,
) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx) //nolint:errcheck // rollback is best effort after the operation result is known
	}()

	saleSnapshot, err := selectSaleSnapshot(ctx, tx, saleID, true)
	if err != nil {
		return err
	}

	sale, err := flashsale.RehydrateSale(saleSnapshot)
	if err != nil {
		return fmt.Errorf(
			"sale validation: %w", err,
		)
	}

	if err := sale.AddItem(
		itemID, productID,
		name, price, totalQty,
	); err != nil {
		return fmt.Errorf("add item error: %w", err)
	}

	insertSaleItemQuery := `
		INSERT INTO flashdrop.sale_items (
			id, sale_id, product_id, name,
			price_minor, total_qty, reserved_qty,
			sold_qty
		)
		VALUES ($1, $2, $3, $4, $5, $6, 0, 0);
	`
	if _, err := tx.Exec(
		ctx, insertSaleItemQuery,
		itemID, saleID, productID,
		name, price.AmountMinor(), totalQty,
	); err != nil {
		return mapDatabaseError("insert sale item", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("transaction commit: %w", err)
	}

	return nil
}
