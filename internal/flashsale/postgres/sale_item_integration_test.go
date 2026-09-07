package flashsale_postgres_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/yyeart/flashdrop/internal/flashsale"
	flashsale_postgres "github.com/yyeart/flashdrop/internal/flashsale/postgres"
)

const addSaleItemConcurrencyAttempts = 32

func TestStore_AddSaleItem_PersistsItemAndPreservesExistingStock(t *testing.T) {
	pool := openTestPool(t)
	store := flashsale_postgres.NewStore(pool)

	sale := newDraftSaleWithItemCounters(t)
	newItemID := uuid.New()
	newProductID := uuid.New()
	cleanupSale(t, pool, sale.ID())

	ctx, cancel := context.WithTimeout(context.Background(), postgresOperationTimeout)
	defer cancel()

	if err := store.CreateSale(ctx, sale); err != nil {
		t.Fatalf("CreateSale() error = %v", err)
	}

	price, err := flashsale.NewMoneyFromMinor(2599)
	if err != nil {
		t.Fatalf("NewMoneyFromMinor() error = %v", err)
	}

	if err := store.AddSaleItem(
		ctx,
		sale.ID(), newItemID, newProductID,
		"new item", price, 25,
	); err != nil {
		t.Fatalf("AddSaleItem() error = %v", err)
	}

	got, err := store.FindSale(ctx, sale.ID())
	if err != nil {
		t.Fatalf("FindSale() error = %v", err)
	}

	assertAddedSaleItemPersistence(
		t, sale, got, newItemID, newProductID, price,
	)
}

func TestStore_AddSaleItem_UnknownSaleReturnsNotFound(t *testing.T) {
	pool := openTestPool(t)
	store := flashsale_postgres.NewStore(pool)

	ctx, cancel := context.WithTimeout(context.Background(), postgresOperationTimeout)
	defer cancel()

	price, err := flashsale.NewMoneyFromMinor(100)
	if err != nil {
		t.Fatalf("NewMoneyFromMinor() error = %v", err)
	}

	saleID := uuid.New()
	err = store.AddSaleItem(
		ctx,
		saleID, uuid.New(), uuid.New(),
		"unknown sale item", price, 1,
	)
	if !errors.Is(err, flashsale.ErrSaleNotFound) {
		t.Fatalf("AddSaleItem() error = %v, want ErrSaleNotFound", err)
	}
	if saleExists(t, pool, saleID) {
		t.Fatalf("unknown sale %s appeared after rejected AddSaleItem()", saleID)
	}
}

func TestStore_AddSaleItem_RejectsNonDraftSaleWithoutChanges(t *testing.T) {
	tests := []struct {
		name      string
		makeSale  func(*testing.T) flashsale.Sale
		wantState flashsale.SaleState
	}{
		{
			name:      "active",
			makeSale:  newActiveSale,
			wantState: flashsale.ActiveState,
		},
		{
			name: "ended",
			makeSale: func(t *testing.T) flashsale.Sale {
				t.Helper()
				sale := newActiveSale(t)
				if err := sale.End(); err != nil {
					t.Fatalf("End() error = %v", err)
				}
				return sale
			},
			wantState: flashsale.EndedState,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sale := tt.makeSale(t)
			assertNonDraftSaleRejectsItem(t, sale, tt.wantState)
		})
	}
}

func TestStore_AddSaleItem_RejectsInvalidInputWithoutChanges(t *testing.T) {
	pool := openTestPool(t)
	store := flashsale_postgres.NewStore(pool)
	sale := newDraftSale(t)
	cleanupSale(t, pool, sale.ID())

	ctx, cancel := context.WithTimeout(context.Background(), postgresOperationTimeout)
	defer cancel()

	if err := store.CreateSale(ctx, sale); err != nil {
		t.Fatalf("CreateSale() error = %v", err)
	}

	validPrice, err := flashsale.NewMoneyFromMinor(100)
	if err != nil {
		t.Fatalf("NewMoneyFromMinor() error = %v", err)
	}
	zeroPrice, err := flashsale.NewMoneyFromMinor(0)
	if err != nil {
		t.Fatalf("NewMoneyFromMinor() zero price error = %v", err)
	}

	tests := []struct {
		name    string
		itemID  uuid.UUID
		product uuid.UUID
		price   flashsale.Money
		qty     int
		wantErr error
	}{
		{
			name:    "empty item id",
			itemID:  uuid.Nil,
			product: uuid.New(),
			price:   validPrice,
			qty:     1,
			wantErr: flashsale.ErrInvalidConfiguration,
		},
		{
			name:    "empty product id",
			itemID:  uuid.New(),
			product: uuid.Nil,
			price:   validPrice,
			qty:     1,
			wantErr: flashsale.ErrInvalidConfiguration,
		},
		{
			name:    "zero price",
			itemID:  uuid.New(),
			product: uuid.New(),
			price:   zeroPrice,
			qty:     1,
			wantErr: flashsale.ErrInvalidMoney,
		},
		{
			name:    "zero quantity",
			itemID:  uuid.New(),
			product: uuid.New(),
			price:   validPrice,
			qty:     0,
			wantErr: flashsale.ErrInvalidQuantity,
		},
		{
			name:    "negative quantity",
			itemID:  uuid.New(),
			product: uuid.New(),
			price:   validPrice,
			qty:     -1,
			wantErr: flashsale.ErrInvalidQuantity,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := store.AddSaleItem(
				ctx,
				sale.ID(), tt.itemID, tt.product,
				"invalid item", tt.price, tt.qty,
			)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("AddSaleItem() error = %v, want %v", err, tt.wantErr)
			}
		})
	}

	got, err := store.FindSale(ctx, sale.ID())
	if err != nil {
		t.Fatalf("FindSale() error = %v", err)
	}
	if len(got.Items()) != 0 {
		t.Fatalf("len(Items()) = %d after rejected inputs, want 0", len(got.Items()))
	}
}

func TestStore_AddSaleItem_RejectsDuplicateItemIDWithoutChanges(t *testing.T) {
	pool := openTestPool(t)
	store := flashsale_postgres.NewStore(pool)
	sale := newDraftSaleWithItem(t, uuid.New())
	cleanupSale(t, pool, sale.ID())

	ctx, cancel := context.WithTimeout(context.Background(), postgresOperationTimeout)
	defer cancel()

	if err := store.CreateSale(ctx, sale); err != nil {
		t.Fatalf("CreateSale() error = %v", err)
	}

	price, err := flashsale.NewMoneyFromMinor(100)
	if err != nil {
		t.Fatalf("NewMoneyFromMinor() error = %v", err)
	}

	existing := sale.Items()[0]
	err = store.AddSaleItem(
		ctx,
		sale.ID(), existing.ID(), uuid.New(),
		"duplicate item", price, 3,
	)
	if !errors.Is(err, flashsale.ErrDuplicateSaleItem) {
		t.Fatalf("AddSaleItem() error = %v, want ErrDuplicateSaleItem", err)
	}

	got, err := store.FindSale(ctx, sale.ID())
	if err != nil {
		t.Fatalf("FindSale() error = %v", err)
	}
	assertSaleEqual(t, sale, got)
}

func TestStore_AddSaleItem_GlobalItemIDConflictRollsBack(t *testing.T) {
	pool := openTestPool(t)
	store := flashsale_postgres.NewStore(pool)

	itemID := uuid.New()
	firstSale := newDraftSale(t)
	secondSale := newDraftSale(t)
	cleanupSale(t, pool, firstSale.ID())
	cleanupSale(t, pool, secondSale.ID())

	ctx, cancel := context.WithTimeout(context.Background(), postgresOperationTimeout)
	defer cancel()

	if err := store.CreateSale(ctx, firstSale); err != nil {
		t.Fatalf("CreateSale(first) error = %v", err)
	}
	if err := store.CreateSale(ctx, secondSale); err != nil {
		t.Fatalf("CreateSale(second) error = %v", err)
	}

	price, err := flashsale.NewMoneyFromMinor(100)
	if err != nil {
		t.Fatalf("NewMoneyFromMinor() error = %v", err)
	}
	if err := store.AddSaleItem(
		ctx,
		firstSale.ID(), itemID, uuid.New(),
		"first item", price, 1,
	); err != nil {
		t.Fatalf("AddSaleItem(first) error = %v", err)
	}

	err = store.AddSaleItem(
		ctx,
		secondSale.ID(), itemID, uuid.New(),
		"conflicting item", price, 1,
	)
	if !errors.Is(err, flashsale.ErrConflict) {
		t.Fatalf("AddSaleItem(second) error = %v, want ErrConflict", err)
	}

	got, err := store.FindSale(ctx, secondSale.ID())
	if err != nil {
		t.Fatalf("FindSale(second) error = %v", err)
	}
	if len(got.Items()) != 0 {
		t.Fatalf("second sale has %d items after conflict, want 0", len(got.Items()))
	}

	got, err = store.FindSale(ctx, firstSale.ID())
	if err != nil {
		t.Fatalf("FindSale(first) error = %v", err)
	}
	if len(got.Items()) != 1 || got.Items()[0].ID() != itemID {
		t.Fatalf("first sale item was not preserved after conflict: %+v", got.Items())
	}
}

func TestStore_AddSaleItem_CanceledContextDoesNotPersist(t *testing.T) {
	pool := openTestPool(t)
	store := flashsale_postgres.NewStore(pool)
	sale := newDraftSale(t)
	cleanupSale(t, pool, sale.ID())

	setupCtx, setupCancel := context.WithTimeout(context.Background(), postgresOperationTimeout)
	defer setupCancel()
	if err := store.CreateSale(setupCtx, sale); err != nil {
		t.Fatalf("CreateSale() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	price, err := flashsale.NewMoneyFromMinor(100)
	if err != nil {
		t.Fatalf("NewMoneyFromMinor() error = %v", err)
	}
	err = store.AddSaleItem(
		ctx,
		sale.ID(), uuid.New(), uuid.New(),
		"canceled item", price, 1,
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("AddSaleItem() error = %v, want context.Canceled", err)
	}

	checkCtx, checkCancel := context.WithTimeout(context.Background(), postgresOperationTimeout)
	defer checkCancel()
	got, err := store.FindSale(checkCtx, sale.ID())
	if err != nil {
		t.Fatalf("FindSale() error = %v", err)
	}
	if len(got.Items()) != 0 {
		t.Fatalf("len(Items()) = %d after canceled AddSaleItem(), want 0", len(got.Items()))
	}
}

func TestStore_AddSaleItem_ConcurrentRequestsPersistAllItems(t *testing.T) {
	pool := openTestPool(t)
	store := flashsale_postgres.NewStore(pool)

	sale := newDraftSale(t)
	cleanupSale(t, pool, sale.ID())

	setupCtx, setupCancel := context.WithTimeout(context.Background(), postgresOperationTimeout)
	defer setupCancel()
	if err := store.CreateSale(setupCtx, sale); err != nil {
		t.Fatalf("CreateSale() error = %v", err)
	}

	price, err := flashsale.NewMoneyFromMinor(100)
	if err != nil {
		t.Fatalf("NewMoneyFromMinor() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ready := make(chan struct{}, addSaleItemConcurrencyAttempts)
	start := make(chan struct{})
	results := make(chan error, addSaleItemConcurrencyAttempts)
	expectedItemIDs := make(map[uuid.UUID]struct{}, addSaleItemConcurrencyAttempts)
	var wg sync.WaitGroup

	for range addSaleItemConcurrencyAttempts {
		itemID := uuid.New()
		productID := uuid.New()
		expectedItemIDs[itemID] = struct{}{}

		wg.Add(1)
		go func(itemID, productID uuid.UUID) {
			defer wg.Done()
			ready <- struct{}{}
			<-start
			results <- store.AddSaleItem(
				ctx,
				sale.ID(), itemID, productID,
				"concurrent item", price, 1,
			)
		}(itemID, productID)
	}

	for range addSaleItemConcurrencyAttempts {
		<-ready
	}
	close(start)
	wg.Wait()
	close(results)

	for err := range results {
		if err != nil {
			t.Errorf("AddSaleItem() concurrent error = %v, want nil", err)
		}
	}

	got, err := store.FindSale(ctx, sale.ID())
	if err != nil {
		t.Fatalf("FindSale() after concurrent AddSaleItem() error = %v", err)
	}
	if len(got.Items()) != addSaleItemConcurrencyAttempts {
		t.Fatalf(
			"len(Items()) after concurrent AddSaleItem() = %d, want %d",
			len(got.Items()), addSaleItemConcurrencyAttempts,
		)
	}

	for itemID := range expectedItemIDs {
		if _, ok := got.Item(itemID); !ok {
			t.Errorf("concurrent item %s was not persisted", itemID)
		}
	}
}

func TestStore_AddSaleItem_ConcurrentDuplicateItemIDHasOneWinner(t *testing.T) {
	pool := openTestPool(t)
	store := flashsale_postgres.NewStore(pool)

	sale := newDraftSale(t)
	cleanupSale(t, pool, sale.ID())

	setupCtx, setupCancel := context.WithTimeout(context.Background(), postgresOperationTimeout)
	defer setupCancel()
	if err := store.CreateSale(setupCtx, sale); err != nil {
		t.Fatalf("CreateSale() error = %v", err)
	}

	price, err := flashsale.NewMoneyFromMinor(100)
	if err != nil {
		t.Fatalf("NewMoneyFromMinor() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ready := make(chan struct{}, 2)
	start := make(chan struct{})
	results := make(chan error, 2)
	itemID := uuid.New()
	var wg sync.WaitGroup

	for range 2 {
		productID := uuid.New()
		wg.Add(1)
		go func(productID uuid.UUID) {
			defer wg.Done()
			ready <- struct{}{}
			<-start
			results <- store.AddSaleItem(
				ctx,
				sale.ID(), itemID, productID,
				"duplicate item", price, 1,
			)
		}(productID)
	}

	for range 2 {
		<-ready
	}
	close(start)
	wg.Wait()
	close(results)

	var successes, duplicates int
	for err := range results {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, flashsale.ErrDuplicateSaleItem):
			duplicates++
		default:
			t.Errorf(
				"AddSaleItem() concurrent duplicate error = %v, want nil or ErrDuplicateSaleItem",
				err,
			)
		}
	}

	if successes != 1 || duplicates != 1 {
		t.Fatalf(
			"concurrent duplicate AddSaleItem() results: successes = %d, duplicates = %d, want 1 and 1",
			successes, duplicates,
		)
	}

	got, err := store.FindSale(ctx, sale.ID())
	if err != nil {
		t.Fatalf("FindSale() after concurrent duplicate AddSaleItem() error = %v", err)
	}
	if len(got.Items()) != 1 {
		t.Fatalf("len(Items()) after concurrent duplicate AddSaleItem() = %d, want 1", len(got.Items()))
	}
	if _, ok := got.Item(itemID); !ok {
		t.Fatalf("winning item %s was not persisted", itemID)
	}
}

func assertAddedSaleItemPersistence(
	t *testing.T,
	want, got flashsale.Sale,
	itemID, productID uuid.UUID,
	price flashsale.Money,
) {
	t.Helper()

	if got.State() != flashsale.DraftState {
		t.Fatalf("state = %q, want %q", got.State(), flashsale.DraftState)
	}
	if len(got.Items()) != 2 {
		t.Fatalf("len(Items()) = %d, want 2", len(got.Items()))
	}

	existing, ok := got.Item(want.Items()[0].ID())
	if !ok {
		t.Fatalf("existing item %s was not persisted", want.Items()[0].ID())
	}
	if existing.ReservedQty() != 2 || existing.SoldQty() != 3 {
		t.Fatalf(
			"existing stock = (%d reserved, %d sold), want (2, 3)",
			existing.ReservedQty(), existing.SoldQty(),
		)
	}

	added, ok := got.Item(itemID)
	if !ok {
		t.Fatalf("new item %s was not persisted", itemID)
	}
	if added.SaleID() != want.ID() ||
		added.ProductID() != productID ||
		added.Name() != "new item" ||
		added.Price().AmountMinor() != price.AmountMinor() ||
		added.TotalQty() != 25 ||
		added.ReservedQty() != 0 ||
		added.SoldQty() != 0 {
		t.Fatalf("persisted item does not match AddSaleItem() input: %+v", added)
	}
}

func assertNonDraftSaleRejectsItem(
	t *testing.T,
	sale flashsale.Sale,
	wantState flashsale.SaleState,
) {
	t.Helper()

	pool := openTestPool(t)
	store := flashsale_postgres.NewStore(pool)
	cleanupSale(t, pool, sale.ID())

	ctx, cancel := context.WithTimeout(context.Background(), postgresOperationTimeout)
	defer cancel()

	if err := store.CreateSale(ctx, sale); err != nil {
		t.Fatalf("CreateSale() error = %v", err)
	}

	price, err := flashsale.NewMoneyFromMinor(100)
	if err != nil {
		t.Fatalf("NewMoneyFromMinor() error = %v", err)
	}

	err = store.AddSaleItem(
		ctx,
		sale.ID(), uuid.New(), uuid.New(),
		"forbidden item", price, 1,
	)
	if !errors.Is(err, flashsale.ErrForbiddenTransition) {
		t.Fatalf("AddSaleItem() error = %v, want ErrForbiddenTransition", err)
	}

	got, err := store.FindSale(ctx, sale.ID())
	if err != nil {
		t.Fatalf("FindSale() error = %v", err)
	}
	if got.State() != wantState {
		t.Fatalf("state = %q, want %q", got.State(), wantState)
	}
	assertSaleEqualByItemID(t, sale, got)
}

func newDraftSaleWithItemCounters(t *testing.T) flashsale.Sale {
	t.Helper()

	sale := newDraftSale(t)
	price, err := flashsale.NewMoneyFromMinor(1250)
	if err != nil {
		t.Fatalf("NewMoneyFromMinor() error = %v", err)
	}

	itemID := uuid.New()
	got, err := flashsale.RehydrateSale(flashsale.SaleSnapshot{
		ID:        sale.ID(),
		State:     flashsale.DraftState,
		StartsAt:  sale.StartsAt(),
		EndsAt:    sale.EndsAt(),
		CreatedAt: sale.CreatedAt(),
		Items: []flashsale.SaleItemSnapshot{
			{
				ID:          itemID,
				SaleID:      sale.ID(),
				ProductID:   uuid.New(),
				Name:        "existing item",
				PriceMinor:  price.AmountMinor(),
				TotalQty:    10,
				ReservedQty: 2,
				SoldQty:     3,
			},
		},
	})
	if err != nil {
		t.Fatalf("RehydrateSale() error = %v", err)
	}

	return got
}
