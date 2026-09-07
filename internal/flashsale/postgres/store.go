package flashsale_postgres

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yyeart/flashdrop/internal/flashsale"
)

const postgresUniqueViolation = "23505"

type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

func (s *Store) CreateSale(
	ctx context.Context,
	sale flashsale.Sale,
) error {
	items := sale.Items()
	itemsSnapshots := make([]flashsale.SaleItemSnapshot, 0, len(items))
	for _, item := range items {
		itemsSnapshots = append(itemsSnapshots, flashsale.SaleItemSnapshot{
			ID:          item.ID(),
			SaleID:      item.SaleID(),
			ProductID:   item.ProductID(),
			Name:        item.Name(),
			PriceMinor:  item.Price().AmountMinor(),
			TotalQty:    item.TotalQty(),
			ReservedQty: item.ReservedQty(),
			SoldQty:     item.SoldQty(),
		})
	}

	saleSnapshot := flashsale.SaleSnapshot{
		ID:        sale.ID(),
		State:     sale.State(),
		StartsAt:  sale.StartsAt(),
		EndsAt:    sale.EndsAt(),
		CreatedAt: sale.CreatedAt(),
		Items:     itemsSnapshots,
	}

	if _, err := flashsale.RehydrateSale(saleSnapshot); err != nil {
		return fmt.Errorf("sale snapshot validation: %w", err)
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx) //nolint:errcheck // rollback is best effort after the operation result is known
	}()

	sales_query := `
		INSERT INTO flashdrop.sales 
		(id, state, starts_at, ends_at, created_at) 
		VALUES ($1, $2, $3, $4, $5);
	`
	if _, err = tx.Exec(
		ctx, sales_query,
		saleSnapshot.ID, saleSnapshot.State,
		saleSnapshot.StartsAt, saleSnapshot.EndsAt,
		saleSnapshot.CreatedAt,
	); err != nil {
		return mapDatabaseError("insert sale", err)
	}

	sale_items_query := `
		INSERT INTO flashdrop.sale_items 
		(id, sale_id, product_id, name, price_minor, total_qty, reserved_qty, sold_qty) 
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8);
	`
	for _, itemSnapshot := range itemsSnapshots {
		if _, err := tx.Exec(
			ctx, sale_items_query,
			itemSnapshot.ID, itemSnapshot.SaleID, itemSnapshot.ProductID,
			itemSnapshot.Name, itemSnapshot.PriceMinor, itemSnapshot.TotalQty,
			itemSnapshot.ReservedQty, itemSnapshot.SoldQty,
		); err != nil {
			return mapDatabaseError("insert sale item", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("transaction commit: %w", err)
	}

	return nil
}

func (s *Store) FindSale(
	ctx context.Context,
	id uuid.UUID,
) (flashsale.Sale, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return flashsale.Sale{}, fmt.Errorf(
			"begin transaction: %w",
			err,
		)
	}
	defer func() {
		_ = tx.Rollback(ctx) //nolint:errcheck // rollback is best effort after the operation result is known
	}()

	saleSnapshot, err := selectSaleSnapshot(ctx, tx, id, false)
	if err != nil {
		return flashsale.Sale{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return flashsale.Sale{}, fmt.Errorf("commit transaction: %w", err)
	}

	sale, err := flashsale.RehydrateSale(saleSnapshot)
	if err != nil {
		return flashsale.Sale{}, fmt.Errorf("rehydrate sale: %w", err)
	}

	return sale, nil
}
