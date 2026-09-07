package flashsale_postgres_test

import (
	"context"
	"errors"
	"os"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yyeart/flashdrop/internal/flashsale"
	flashsale_postgres "github.com/yyeart/flashdrop/internal/flashsale/postgres"
)

const postgresOperationTimeout = 5 * time.Second

func TestStore_CreateFindSale_DraftWithoutItems(t *testing.T) {
	pool := openTestPool(t)
	store := flashsale_postgres.NewStore(pool)

	want := newDraftSale(t)
	cleanupSale(t, pool, want.ID())

	ctx, cancel := context.WithTimeout(context.Background(), postgresOperationTimeout)
	defer cancel()

	if err := store.CreateSale(ctx, want); err != nil {
		t.Fatalf("CreateSale() error = %v", err)
	}

	got, err := store.FindSale(ctx, want.ID())
	if err != nil {
		t.Fatalf("FindSale() error = %v", err)
	}

	assertSaleEqual(t, want, got)
}

func TestStore_CreateFindSale_ActivePreservesFieldsAndItemOrderByID(t *testing.T) {
	pool := openTestPool(t)
	store := flashsale_postgres.NewStore(pool)

	want := newActiveSale(t)
	cleanupSale(t, pool, want.ID())

	ctx, cancel := context.WithTimeout(context.Background(), postgresOperationTimeout)
	defer cancel()

	if err := store.CreateSale(ctx, want); err != nil {
		t.Fatalf("CreateSale() error = %v", err)
	}

	got, err := store.FindSale(ctx, want.ID())
	if err != nil {
		t.Fatalf("FindSale() error = %v", err)
	}

	assertSaleEqualByItemID(t, want, got)
}

func TestStore_FindSale_ReturnsErrSaleNotFound(t *testing.T) {
	pool := openTestPool(t)
	store := flashsale_postgres.NewStore(pool)

	ctx, cancel := context.WithTimeout(context.Background(), postgresOperationTimeout)
	defer cancel()

	_, err := store.FindSale(ctx, uuid.New())
	if !errors.Is(err, flashsale.ErrSaleNotFound) {
		t.Fatalf("FindSale() error = %v, want errors.Is(..., ErrSaleNotFound)", err)
	}
}

func TestStore_CreateSale_IsAtomicWhenItemInsertConflicts(t *testing.T) {
	pool := openTestPool(t)
	store := flashsale_postgres.NewStore(pool)

	conflictingItemID := uuid.New()
	first := newDraftSaleWithItem(t, conflictingItemID)
	second := newDraftSaleWithItem(t, conflictingItemID)
	cleanupSale(t, pool, first.ID())
	cleanupSale(t, pool, second.ID())

	ctx, cancel := context.WithTimeout(context.Background(), postgresOperationTimeout)
	defer cancel()

	if err := store.CreateSale(ctx, first); err != nil {
		t.Fatalf("CreateSale(first) error = %v", err)
	}

	err := store.CreateSale(ctx, second)
	if !errors.Is(err, flashsale.ErrConflict) {
		t.Fatalf(
			"CreateSale(second) error = %v, want errors.Is(..., ErrConflict)",
			err,
		)
	}

	if err := store.CreateSale(ctx, second); err == nil {
		t.Fatal("CreateSale(second) error = nil, want conflict error")
	}

	if exists := saleExists(t, pool, second.ID()); exists {
		t.Fatalf("sale %s still exists after an item insert conflict", second.ID())
	}
}

func TestStore_CreateSale_CanceledContextDoesNotSucceed(t *testing.T) {
	pool := openTestPool(t)
	store := flashsale_postgres.NewStore(pool)

	want := newDraftSale(t)
	cleanupSale(t, pool, want.ID())

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := store.CreateSale(ctx, want); err == nil {
		t.Fatal("CreateSale() error = nil with a canceled context")
	}

	if exists := saleExists(t, pool, want.ID()); exists {
		t.Fatalf("sale %s was persisted with a canceled context", want.ID())
	}
}

func openTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	dsn := os.Getenv("FLASHDROP_TEST_DSN")
	if dsn == "" {
		t.Skip("FLASHDROP_TEST_DSN is not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), postgresOperationTimeout)
	defer cancel()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pgxpool.New() error = %v", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Fatalf("PostgreSQL ping error = %v", err)
	}

	t.Cleanup(pool.Close)
	return pool
}

func newDraftSale(t *testing.T) flashsale.Sale {
	t.Helper()

	createdAt := time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC)
	sale, err := flashsale.NewSale(
		uuid.New(),
		createdAt.Add(time.Hour),
		createdAt.Add(2*time.Hour),
		createdAt,
	)
	if err != nil {
		t.Fatalf("NewSale() error = %v", err)
	}

	return sale
}

func newDraftSaleWithItem(t *testing.T, itemID uuid.UUID) flashsale.Sale {
	t.Helper()

	sale := newDraftSale(t)
	price, err := flashsale.NewMoneyFromMinor(1250)
	if err != nil {
		t.Fatalf("NewMoneyFromMinor() error = %v", err)
	}

	if err := sale.AddItem(itemID, uuid.New(), "conflict item", price, 10); err != nil {
		t.Fatalf("AddItem() error = %v", err)
	}

	return sale
}

func newActiveSale(t *testing.T) flashsale.Sale {
	t.Helper()

	createdAt := time.Date(2026, time.February, 3, 4, 5, 6, 0, time.UTC)
	saleID := uuid.New()
	firstItemID := uuid.New()
	secondItemID := uuid.New()

	firstPrice, err := flashsale.NewMoneyFromMinor(1999)
	if err != nil {
		t.Fatalf("NewMoneyFromMinor(first) error = %v", err)
	}
	secondPrice, err := flashsale.NewMoneyFromMinor(875)
	if err != nil {
		t.Fatalf("NewMoneyFromMinor(second) error = %v", err)
	}

	want, err := flashsale.RehydrateSale(flashsale.SaleSnapshot{
		ID:        saleID,
		State:     flashsale.ActiveState,
		StartsAt:  createdAt.Add(time.Hour),
		EndsAt:    createdAt.Add(3 * time.Hour),
		CreatedAt: createdAt,
		Items: []flashsale.SaleItemSnapshot{
			{
				ID:          firstItemID,
				SaleID:      saleID,
				ProductID:   uuid.New(),
				Name:        "first item",
				PriceMinor:  firstPrice.AmountMinor(),
				TotalQty:    11,
				ReservedQty: 2,
				SoldQty:     3,
			},
			{
				ID:          secondItemID,
				SaleID:      saleID,
				ProductID:   uuid.New(),
				Name:        "second item",
				PriceMinor:  secondPrice.AmountMinor(),
				TotalQty:    20,
				ReservedQty: 0,
				SoldQty:     4,
			},
		},
	})
	if err != nil {
		t.Fatalf("RehydrateSale() error = %v", err)
	}

	return want
}

func cleanupSale(t *testing.T, pool *pgxpool.Pool, saleID uuid.UUID) {
	t.Helper()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), postgresOperationTimeout)
		defer cancel()

		if _, err := pool.Exec(ctx, `DELETE FROM flashdrop.sale_items WHERE sale_id = $1`, saleID); err != nil {
			t.Errorf("cleanup sale items: %v", err)
		}
		if _, err := pool.Exec(ctx, `DELETE FROM flashdrop.sales WHERE id = $1`, saleID); err != nil {
			t.Errorf("cleanup sale: %v", err)
		}
	})
}

func saleExists(t *testing.T, pool *pgxpool.Pool, saleID uuid.UUID) bool {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), postgresOperationTimeout)
	defer cancel()

	var exists bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM flashdrop.sales WHERE id = $1)`, saleID).Scan(&exists); err != nil {
		t.Fatalf("check sale existence: %v", err)
	}

	return exists
}

func assertSaleEqual(t *testing.T, want, got flashsale.Sale) {
	t.Helper()

	assertSaleFieldsEqual(t, want, got)
	assertSaleItemsEqual(t, want.Items(), got.Items())
}

func assertSaleEqualByItemID(t *testing.T, want, got flashsale.Sale) {
	t.Helper()

	assertSaleFieldsEqual(t, want, got)
	wantItems := want.Items()
	sort.Slice(wantItems, func(i, j int) bool {
		return wantItems[i].ID().String() < wantItems[j].ID().String()
	})
	assertSaleItemsEqual(t, wantItems, got.Items())
}

func assertSaleItemsEqual(t *testing.T, wantItems, gotItems []flashsale.SaleItem) {
	t.Helper()

	if len(gotItems) != len(wantItems) {
		t.Fatalf("len(Items()) = %d, want %d", len(gotItems), len(wantItems))
	}

	for idx := range wantItems {
		assertSaleItemEqual(t, idx, wantItems[idx], gotItems[idx])
	}
}

func assertSaleFieldsEqual(t *testing.T, want, got flashsale.Sale) {
	t.Helper()

	if got.ID() != want.ID() {
		t.Errorf("ID() = %s, want %s", got.ID(), want.ID())
	}
	if got.State() != want.State() {
		t.Errorf("State() = %q, want %q", got.State(), want.State())
	}
	if !got.StartsAt().Equal(want.StartsAt()) {
		t.Errorf("StartsAt() = %s, want %s", got.StartsAt(), want.StartsAt())
	}
	if !got.EndsAt().Equal(want.EndsAt()) {
		t.Errorf("EndsAt() = %s, want %s", got.EndsAt(), want.EndsAt())
	}
	if !got.CreatedAt().Equal(want.CreatedAt()) {
		t.Errorf("CreatedAt() = %s, want %s", got.CreatedAt(), want.CreatedAt())
	}
}

func assertSaleItemEqual(t *testing.T, index int, want, got flashsale.SaleItem) {
	t.Helper()

	if got.ID() != want.ID() {
		t.Errorf("Items()[%d].ID() = %s, want %s", index, got.ID(), want.ID())
	}
	if got.SaleID() != want.SaleID() {
		t.Errorf("Items()[%d].SaleID() = %s, want %s", index, got.SaleID(), want.SaleID())
	}
	if got.ProductID() != want.ProductID() {
		t.Errorf("Items()[%d].ProductID() = %s, want %s", index, got.ProductID(), want.ProductID())
	}
	if got.Name() != want.Name() {
		t.Errorf("Items()[%d].Name() = %q, want %q", index, got.Name(), want.Name())
	}
	if got.Price().AmountMinor() != want.Price().AmountMinor() {
		t.Errorf("Items()[%d].Price() = %d, want %d", index, got.Price().AmountMinor(), want.Price().AmountMinor())
	}
	if got.TotalQty() != want.TotalQty() {
		t.Errorf("Items()[%d].TotalQty() = %d, want %d", index, got.TotalQty(), want.TotalQty())
	}
	if got.ReservedQty() != want.ReservedQty() {
		t.Errorf("Items()[%d].ReservedQty() = %d, want %d", index, got.ReservedQty(), want.ReservedQty())
	}
	if got.SoldQty() != want.SoldQty() {
		t.Errorf("Items()[%d].SoldQty() = %d, want %d", index, got.SoldQty(), want.SoldQty())
	}
}
