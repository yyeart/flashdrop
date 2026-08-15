package flashsale

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestNewSaleCreatesDraft(t *testing.T) {
	id := uuid.New()
	startsAt := testNow().Add(time.Hour)
	endsAt := startsAt.Add(2 * time.Hour)
	createdAt := testNow()

	sale, err := NewSale(id, startsAt, endsAt, createdAt)
	if err != nil {
		t.Fatalf("NewSale() error = %v", err)
	}

	if sale.ID() != id {
		t.Fatalf("ID() = %v, want %v", sale.ID(), id)
	}

	if sale.State() != DraftState {
		t.Fatalf("State() = %q, want %q", sale.State(), DraftState)
	}

	if !sale.StartsAt().Equal(startsAt) {
		t.Fatalf("StartsAt() = %v, want %v", sale.StartsAt(), startsAt)
	}

	if !sale.EndsAt().Equal(endsAt) {
		t.Fatalf("EndsAt() = %v, want %v", sale.EndsAt(), endsAt)
	}

	if !sale.CreatedAt().Equal(createdAt) {
		t.Fatalf("CreatedAt() = %v, want %v", sale.CreatedAt(), createdAt)
	}

	if len(sale.Items()) != 0 {
		t.Fatalf("len(Items()) = %d, want 0", len(sale.Items()))
	}
}

func TestNewSaleRejectsEmptyID(t *testing.T) {
	_, err := NewSale(
		uuid.Nil,
		testNow().Add(time.Hour),
		testNow().Add(2*time.Hour),
		testNow(),
	)

	if !errors.Is(err, ErrInvalidConfiguration) {
		t.Fatalf("NewSale() error = %v, want ErrInvalidConfiguration", err)
	}
}

func TestNewSaleRejectsInvalidTimeWindow(t *testing.T) {
	base := testNow()

	tests := []struct {
		name     string
		startsAt time.Time
		endsAt   time.Time
	}{
		{
			name:     "starts_at equals ends_at",
			startsAt: base,
			endsAt:   base,
		},
		{
			name:     "starts_at after ends_at",
			startsAt: base.Add(time.Second),
			endsAt:   base,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewSale(
				uuid.New(),
				tt.startsAt,
				tt.endsAt,
				base,
			)

			if !errors.Is(err, ErrInvalidConfiguration) {
				t.Fatalf(
					"NewSale() error = %v, want ErrInvalidConfiguration",
					err,
				)
			}
		})
	}
}

func TestSaleAddItem(t *testing.T) {
	sale := newValidSale(t)

	itemID := uuid.New()
	productID := uuid.New()
	name := "Test product"
	price := 99.99

	err := sale.AddItem(
		itemID,
		productID,
		name,
		price,
		10,
	)
	if err != nil {
		t.Fatalf("AddItem() error = %v", err)
	}

	item, ok := sale.Item(itemID)
	if !ok {
		t.Fatal("Item() ok = false, want true")
	}

	if item.ID() != itemID {
		t.Fatalf("item.ID() = %v, want %v", item.ID(), itemID)
	}

	if item.SaleID() != sale.ID() {
		t.Fatalf(
			"item.SaleID() = %v, want %v",
			item.SaleID(),
			sale.ID(),
		)
	}

	if item.ProductID() != productID {
		t.Fatalf(
			"item.ProductID() = %v, want %v",
			item.ProductID(),
			productID,
		)
	}

	if item.Name() != name {
		t.Fatalf(
			"item.Name() = %v, want %v",
			item.Name(),
			name,
		)
	}

	if item.Price() != price {
		t.Fatalf(
			"item.Price() = %v, want %v",
			item.Price(),
			price,
		)
	}

	if item.TotalQty() != 10 {
		t.Fatalf("item.TotalQty() = %d, want 10", item.TotalQty())
	}

	if item.ReservedQty() != 0 {
		t.Fatalf(
			"item.ReservedQty() = %d, want 0",
			item.ReservedQty(),
		)
	}

	if item.SoldQty() != 0 {
		t.Fatalf("item.SoldQty() = %d, want 0", item.SoldQty())
	}

	if item.AvailableQty() != 10 {
		t.Fatalf(
			"item.AvailableQty() = %d, want 10",
			item.AvailableQty(),
		)
	}
}

func TestSaleAddItemRejectsInvalidIDs(t *testing.T) {
	tests := []struct {
		name      string
		itemID    uuid.UUID
		productID uuid.UUID
	}{
		{
			name:      "empty item id",
			itemID:    uuid.Nil,
			productID: uuid.New(),
		},
		{
			name:      "empty product id",
			itemID:    uuid.New(),
			productID: uuid.Nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sale := newValidSale(t)

			err := sale.AddItem(
				tt.itemID,
				tt.productID,
				"Test product",
				100,
				10,
			)

			if !errors.Is(err, ErrInvalidConfiguration) {
				t.Fatalf(
					"AddItem() error = %v, want ErrInvalidConfiguration",
					err,
				)
			}

			if len(sale.Items()) != 0 {
				t.Fatalf(
					"len(Items()) = %d, want 0",
					len(sale.Items()),
				)
			}
		})
	}
}

func TestSaleAddItemRejectsInvalidTotalQty(t *testing.T) {
	tests := []struct {
		name     string
		totalQty int
	}{
		{
			name:     "zero",
			totalQty: 0,
		},
		{
			name:     "negative",
			totalQty: -1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sale := newValidSale(t)

			err := sale.AddItem(
				uuid.New(),
				uuid.New(),
				"Test product",
				100,
				tt.totalQty,
			)

			if !errors.Is(err, ErrInvalidStock) {
				t.Fatalf(
					"AddItem() error = %v, want ErrInvalidStock",
					err,
				)
			}

			if len(sale.Items()) != 0 {
				t.Fatalf(
					"len(Items()) = %d, want 0",
					len(sale.Items()),
				)
			}
		})
	}
}

func TestSaleAddItemRejectsDuplicateID(t *testing.T) {
	sale := newValidSale(t)
	itemID := uuid.New()

	err := sale.AddItem(
		itemID,
		uuid.New(),
		"First product",
		100,
		10,
	)
	if err != nil {
		t.Fatalf("first AddItem() error = %v", err)
	}

	err = sale.AddItem(
		itemID,
		uuid.New(),
		"Second product",
		200,
		20,
	)

	if !errors.Is(err, ErrDuplicateSaleItem) {
		t.Fatalf(
			"second AddItem() error = %v, want ErrDuplicateSaleItem",
			err,
		)
	}

	if len(sale.Items()) != 1 {
		t.Fatalf(
			"len(Items()) = %d, want 1",
			len(sale.Items()),
		)
	}
}

func TestSaleAddItemFromActiveForbidden(t *testing.T) {
	sale := newSaleWithItem(t)

	if err := sale.Activate(sale.StartsAt().Add(-time.Hour)); err != nil {
		t.Fatalf("Activate() error = %v", err)
	}

	err := sale.AddItem(
		uuid.New(),
		uuid.New(),
		"Another product",
		100,
		10,
	)

	if !errors.Is(err, ErrForbiddenTransition) {
		t.Fatalf(
			"AddItem() error = %v, want ErrForbiddenTransition",
			err,
		)
	}

	if len(sale.Items()) != 1 {
		t.Fatalf(
			"len(Items()) = %d, want 1",
			len(sale.Items()),
		)
	}
}

func TestSaleAddItemFromEndedForbidden(t *testing.T) {
	sale := newSaleWithItem(t)

	if err := sale.Activate(sale.StartsAt().Add(-time.Hour)); err != nil {
		t.Fatalf("Activate() error = %v", err)
	}

	if err := sale.End(); err != nil {
		t.Fatalf("End() error = %v", err)
	}

	err := sale.AddItem(
		uuid.New(),
		uuid.New(),
		"Another product",
		100,
		10,
	)

	if !errors.Is(err, ErrForbiddenTransition) {
		t.Fatalf(
			"AddItem() error = %v, want ErrForbiddenTransition",
			err,
		)
	}

	if len(sale.Items()) != 1 {
		t.Fatalf(
			"len(Items()) = %d, want 1",
			len(sale.Items()),
		)
	}
}

func TestSaleItemsReturnsCopy(t *testing.T) {
	sale := newSaleWithItem(t)
	originalID := sale.items[0].id

	items := sale.Items()
	items[0].id = uuid.New()

	if sale.items[0].id != originalID {
		t.Fatalf(
			"Items() exposes internal slice: id = %v, want %v",
			sale.items[0].id,
			originalID,
		)
	}
}

func TestSaleItemReturnsCopy(t *testing.T) {
	sale := newSaleWithItem(t)

	itemID := sale.items[0].id
	originalQty := sale.items[0].totalQty

	item, ok := sale.Item(itemID)
	if !ok {
		t.Fatal("Item() ok = false, want true")
	}

	item.totalQty = 999

	stored, ok := sale.Item(itemID)
	if !ok {
		t.Fatal("Item() after mutation ok = false, want true")
	}

	if stored.TotalQty() != originalQty {
		t.Fatalf(
			"Item() exposes mutable internal item: TotalQty() = %d, want %d",
			stored.TotalQty(),
			originalQty,
		)
	}
}

func TestSaleItemNotFound(t *testing.T) {
	sale := newSaleWithItem(t)

	_, ok := sale.Item(uuid.New())

	if ok {
		t.Fatal("Item() ok = true, want false")
	}
}

func TestSaleItemAvailableQty(t *testing.T) {
	item := SaleItem{
		totalQty:    10,
		reservedQty: 3,
		soldQty:     2,
	}

	if item.AvailableQty() != 5 {
		t.Fatalf(
			"AvailableQty() = %d, want 5",
			item.AvailableQty(),
		)
	}
}

func TestSaleActivateDraftToActive(t *testing.T) {
	sale := newSaleWithItem(t)

	err := sale.Activate(sale.StartsAt().Add(-time.Hour))
	if err != nil {
		t.Fatalf("Activate() error = %v", err)
	}

	if sale.State() != ActiveState {
		t.Fatalf(
			"State() = %q, want %q",
			sale.State(),
			ActiveState,
		)
	}
}

func TestSaleActivateBeforeStartsAtAllowed(t *testing.T) {
	sale := newSaleWithItem(t)

	err := sale.Activate(sale.StartsAt().Add(-time.Second))

	if err != nil {
		t.Fatalf(
			"Activate() before starts_at error = %v",
			err,
		)
	}

	if sale.State() != ActiveState {
		t.Fatalf(
			"State() = %q, want %q",
			sale.State(),
			ActiveState,
		)
	}
}

func TestSaleActivateAtStartsAtAllowed(t *testing.T) {
	sale := newSaleWithItem(t)

	err := sale.Activate(sale.StartsAt())

	if err != nil {
		t.Fatalf(
			"Activate() at starts_at error = %v",
			err,
		)
	}

	if sale.State() != ActiveState {
		t.Fatalf(
			"State() = %q, want %q",
			sale.State(),
			ActiveState,
		)
	}
}

func TestSaleActivateExpiredTimeWindowForbidden(t *testing.T) {
	tests := []struct {
		name string
		now  func(Sale) time.Time
	}{
		{
			name: "at ends_at",
			now: func(s Sale) time.Time {
				return s.EndsAt()
			},
		},
		{
			name: "after ends_at",
			now: func(s Sale) time.Time {
				return s.EndsAt().Add(time.Second)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sale := newSaleWithItem(t)

			err := sale.Activate(tt.now(sale))

			if !errors.Is(err, ErrExpiredTimeWindow) {
				t.Fatalf(
					"Activate() error = %v, want ErrExpiredTimeWindow",
					err,
				)
			}

			if sale.State() != DraftState {
				t.Fatalf(
					"State() = %q, want %q",
					sale.State(),
					DraftState,
				)
			}
		})
	}
}

func TestSaleWithoutItemsCannotBeActivated(t *testing.T) {
	sale := newValidSale(t)

	err := sale.Activate(sale.StartsAt().Add(-time.Hour))

	if !errors.Is(err, ErrInvalidConfiguration) {
		t.Fatalf(
			"Activate() error = %v, want ErrInvalidConfiguration",
			err,
		)
	}

	if sale.State() != DraftState {
		t.Fatalf(
			"State() = %q, want %q",
			sale.State(),
			DraftState,
		)
	}
}

func TestSaleActivateFromActiveForbidden(t *testing.T) {
	sale := newSaleWithItem(t)
	now := sale.StartsAt().Add(-time.Hour)

	if err := sale.Activate(now); err != nil {
		t.Fatalf("first Activate() error = %v", err)
	}

	err := sale.Activate(now)

	if !errors.Is(err, ErrForbiddenTransition) {
		t.Fatalf(
			"second Activate() error = %v, want ErrForbiddenTransition",
			err,
		)
	}
}

func TestSaleActivateFromEndedForbidden(t *testing.T) {
	sale := newSaleWithItem(t)
	now := sale.StartsAt().Add(-time.Hour)

	if err := sale.Activate(now); err != nil {
		t.Fatalf("Activate() error = %v", err)
	}

	if err := sale.End(); err != nil {
		t.Fatalf("End() error = %v", err)
	}

	err := sale.Activate(now)

	if !errors.Is(err, ErrForbiddenTransition) {
		t.Fatalf(
			"Activate() from ended error = %v, want ErrForbiddenTransition",
			err,
		)
	}
}

func TestSaleActivateRejectsInvalidTimeWindowSnapshot(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Sale)
	}{
		{
			name: "starts_at equals ends_at",
			mutate: func(s *Sale) {
				s.startsAt = s.endsAt
			},
		},
		{
			name: "starts_at after ends_at",
			mutate: func(s *Sale) {
				s.startsAt = s.endsAt.Add(time.Second)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sale := newSaleWithItem(t)
			tt.mutate(&sale)

			err := sale.Activate(testNow())

			if !errors.Is(err, ErrInvalidConfiguration) {
				t.Fatalf(
					"Activate() error = %v, want ErrInvalidConfiguration",
					err,
				)
			}

			if sale.State() != DraftState {
				t.Fatalf(
					"State() = %q, want %q",
					sale.State(),
					DraftState,
				)
			}
		})
	}
}

func TestSaleActivateRejectsInvalidStockSnapshot(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*SaleItem)
	}{
		{
			name: "zero total quantity",
			mutate: func(item *SaleItem) {
				item.totalQty = 0
			},
		},
		{
			name: "negative total quantity",
			mutate: func(item *SaleItem) {
				item.totalQty = -1
			},
		},
		{
			name: "negative reserved quantity",
			mutate: func(item *SaleItem) {
				item.reservedQty = -1
			},
		},
		{
			name: "negative sold quantity",
			mutate: func(item *SaleItem) {
				item.soldQty = -1
			},
		},
		{
			name: "reserved plus sold exceeds total",
			mutate: func(item *SaleItem) {
				item.totalQty = 10
				item.reservedQty = 6
				item.soldQty = 5
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sale := newSaleWithItem(t)
			tt.mutate(&sale.items[0])

			err := sale.Activate(
				sale.StartsAt().Add(-time.Hour),
			)

			if !errors.Is(err, ErrInvalidStock) {
				t.Fatalf(
					"Activate() error = %v, want ErrInvalidStock",
					err,
				)
			}

			if sale.State() != DraftState {
				t.Fatalf(
					"State() = %q, want %q",
					sale.State(),
					DraftState,
				)
			}
		})
	}
}

func TestSaleEndActiveToEnded(t *testing.T) {
	sale := newSaleWithItem(t)

	if err := sale.Activate(sale.StartsAt().Add(-time.Hour)); err != nil {
		t.Fatalf("Activate() error = %v", err)
	}

	if err := sale.End(); err != nil {
		t.Fatalf("End() error = %v", err)
	}

	if sale.State() != EndedState {
		t.Fatalf(
			"State() = %q, want %q",
			sale.State(),
			EndedState,
		)
	}
}

func TestSaleEndFromDraftForbidden(t *testing.T) {
	sale := newValidSale(t)

	err := sale.End()

	if !errors.Is(err, ErrForbiddenTransition) {
		t.Fatalf(
			"End() error = %v, want ErrForbiddenTransition",
			err,
		)
	}

	if sale.State() != DraftState {
		t.Fatalf(
			"State() = %q, want %q",
			sale.State(),
			DraftState,
		)
	}
}

func TestSaleEndFromEndedForbidden(t *testing.T) {
	sale := newSaleWithItem(t)

	if err := sale.Activate(sale.StartsAt().Add(-time.Hour)); err != nil {
		t.Fatalf("Activate() error = %v", err)
	}

	if err := sale.End(); err != nil {
		t.Fatalf("first End() error = %v", err)
	}

	err := sale.End()

	if !errors.Is(err, ErrForbiddenTransition) {
		t.Fatalf(
			"second End() error = %v, want ErrForbiddenTransition",
			err,
		)
	}

	if sale.State() != EndedState {
		t.Fatalf(
			"State() = %q, want %q",
			sale.State(),
			EndedState,
		)
	}
}

func newValidSale(t *testing.T) Sale {
	t.Helper()

	now := testNow()

	sale, err := NewSale(
		uuid.New(),
		now.Add(time.Hour),
		now.Add(3*time.Hour),
		now,
	)
	if err != nil {
		t.Fatalf("NewSale() error = %v", err)
	}

	return sale
}

func newSaleWithItem(t *testing.T) Sale {
	t.Helper()

	sale := newValidSale(t)

	err := sale.AddItem(
		uuid.New(),
		uuid.New(),
		"Test product",
		100,
		10,
	)
	if err != nil {
		t.Fatalf("AddItem() error = %v", err)
	}

	return sale
}

func testNow() time.Time {
	return time.Date(
		2026,
		time.August,
		15,
		12,
		0,
		0,
		0,
		time.UTC,
	)
}
