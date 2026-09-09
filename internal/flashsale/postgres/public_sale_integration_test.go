package flashsale_postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/yyeart/flashdrop/internal/flashsale"
	flashsale_postgres "github.com/yyeart/flashdrop/internal/flashsale/postgres"
)

func TestStore_ListActiveSales_FiltersAndPaginatesInStableOrder(t *testing.T) {
	pool := openTestPool(t)
	store := flashsale_postgres.NewStore(pool)
	baseCreatedAt := time.Date(2400, time.January, 2, 3, 4, 5, 0, time.UTC)

	oldest := newPublicSale(t, flashsale.ActiveState, baseCreatedAt)
	middle := newPublicSale(t, flashsale.ActiveState, baseCreatedAt.Add(time.Minute))
	newestCreatedAt := baseCreatedAt.Add(2 * time.Minute)
	newestFirst := newPublicSaleWithID(
		t,
		uuid.MustParse("00000000-0000-0000-0000-000000000001"),
		flashsale.ActiveState,
		newestCreatedAt,
	)
	newestSecond := newPublicSaleWithID(
		t,
		uuid.MustParse("00000000-0000-0000-0000-000000000002"),
		flashsale.ActiveState,
		newestCreatedAt,
	)
	draft := newPublicSale(t, flashsale.DraftState, baseCreatedAt.Add(3*time.Minute))
	ended := newPublicSale(t, flashsale.EndedState, baseCreatedAt.Add(4*time.Minute))

	ctx, cancel := context.WithTimeout(context.Background(), postgresOperationTimeout)
	defer cancel()

	for _, sale := range []flashsale.Sale{
		oldest, middle, newestFirst, newestSecond, draft, ended,
	} {
		cleanupSale(t, pool, sale.ID())
		if err := store.CreateSale(ctx, sale); err != nil {
			t.Fatalf("CreateSale(%s) error = %v", sale.ID(), err)
		}
	}

	firstPage, err := store.ListActiveSales(ctx, 1, 0)
	if err != nil {
		t.Fatalf("ListActiveSales(first page) error = %v", err)
	}
	if len(firstPage) != 1 {
		t.Fatalf("len(first page) = %d, want 1", len(firstPage))
	}
	assertSaleEqualByItemID(t, newestFirst, firstPage[0])

	secondPage, err := store.ListActiveSales(ctx, 1, 1)
	if err != nil {
		t.Fatalf("ListActiveSales(second page) error = %v", err)
	}
	if len(secondPage) != 1 {
		t.Fatalf("len(second page) = %d, want 1", len(secondPage))
	}
	assertSaleEqualByItemID(t, newestSecond, secondPage[0])

	thirdPage, err := store.ListActiveSales(ctx, 2, 2)
	if err != nil {
		t.Fatalf("ListActiveSales(third page) error = %v", err)
	}
	if len(thirdPage) != 2 {
		t.Fatalf("len(third page) = %d, want 2", len(thirdPage))
	}
	assertSaleEqualByItemID(t, middle, thirdPage[0])
	assertSaleEqualByItemID(t, oldest, thirdPage[1])
}

func TestStore_ListActiveSales_EmptyPageReturnsEmptySlice(t *testing.T) {
	pool := openTestPool(t)
	store := flashsale_postgres.NewStore(pool)

	ctx, cancel := context.WithTimeout(context.Background(), postgresOperationTimeout)
	defer cancel()

	// The offset is deliberately beyond the number of rows in the integration database.
	const emptyPageOffset = 1_000_000
	sales, err := store.ListActiveSales(ctx, 20, emptyPageOffset)
	if err != nil {
		t.Fatalf("ListActiveSales() error = %v", err)
	}
	if sales == nil {
		t.Fatal("ListActiveSales() returned nil slice, want empty non-nil slice")
	}
	if len(sales) != 0 {
		t.Fatalf("len(ListActiveSales()) = %d, want 0", len(sales))
	}
}

func TestStore_ListActiveSales_RejectsInvalidPagination(t *testing.T) {
	store := flashsale_postgres.NewStore(nil)

	tests := []struct {
		name   string
		limit  int
		offset int
	}{
		{name: "zero limit", limit: 0, offset: 0},
		{name: "negative limit", limit: -1, offset: 0},
		{name: "negative offset", limit: 1, offset: -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := store.ListActiveSales(context.Background(), tt.limit, tt.offset)
			if !errors.Is(err, flashsale.ErrInvalidPagination) {
				t.Fatalf(
					"ListActiveSales() error = %v, want errors.Is(..., ErrInvalidPagination)",
					err,
				)
			}
		})
	}
}

func TestStore_FindActiveSale_ReturnsOnlyActiveSale(t *testing.T) {
	pool := openTestPool(t)
	store := flashsale_postgres.NewStore(pool)
	baseCreatedAt := time.Date(2400, time.February, 3, 4, 5, 6, 0, time.UTC)

	active := newPublicSale(t, flashsale.ActiveState, baseCreatedAt)
	draft := newPublicSale(t, flashsale.DraftState, baseCreatedAt.Add(time.Minute))
	ended := newPublicSale(t, flashsale.EndedState, baseCreatedAt.Add(2*time.Minute))

	ctx, cancel := context.WithTimeout(context.Background(), postgresOperationTimeout)
	defer cancel()

	for _, sale := range []flashsale.Sale{active, draft, ended} {
		cleanupSale(t, pool, sale.ID())
		if err := store.CreateSale(ctx, sale); err != nil {
			t.Fatalf("CreateSale(%s) error = %v", sale.ID(), err)
		}
	}

	got, err := store.FindActiveSale(ctx, active.ID())
	if err != nil {
		t.Fatalf("FindActiveSale(active) error = %v", err)
	}
	assertSaleEqualByItemID(t, active, got)

	for name, saleID := range map[string]uuid.UUID{
		"draft":   draft.ID(),
		"ended":   ended.ID(),
		"unknown": uuid.New(),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := store.FindActiveSale(ctx, saleID)
			if !errors.Is(err, flashsale.ErrSaleNotFound) {
				t.Fatalf(
					"FindActiveSale() error = %v, want errors.Is(..., ErrSaleNotFound)",
					err,
				)
			}
		})
	}
}

func newPublicSale(
	t *testing.T,
	state flashsale.SaleState,
	createdAt time.Time,
) flashsale.Sale {
	t.Helper()

	return newPublicSaleWithID(t, uuid.New(), state, createdAt)
}

func newPublicSaleWithID(
	t *testing.T,
	saleID uuid.UUID,
	state flashsale.SaleState,
	createdAt time.Time,
) flashsale.Sale {
	t.Helper()

	price, err := flashsale.NewMoneyFromMinor(1250)
	if err != nil {
		t.Fatalf("NewMoneyFromMinor() error = %v", err)
	}

	sale, err := flashsale.RehydrateSale(flashsale.SaleSnapshot{
		ID:        saleID,
		State:     state,
		StartsAt:  createdAt.Add(time.Hour),
		EndsAt:    createdAt.Add(2 * time.Hour),
		CreatedAt: createdAt,
		Items: []flashsale.SaleItemSnapshot{
			{
				ID:          uuid.New(),
				SaleID:      saleID,
				ProductID:   uuid.New(),
				Name:        "public sale item",
				PriceMinor:  price.AmountMinor(),
				TotalQty:    10,
				ReservedQty: 0,
				SoldQty:     0,
			},
		},
	})
	if err != nil {
		t.Fatalf("RehydrateSale() error = %v", err)
	}

	return sale
}
