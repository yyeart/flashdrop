package flashsale

import (
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type SaleState string

const (
	DraftState  SaleState = "draft"
	ActiveState SaleState = "active"
	EndedState  SaleState = "ended"
)

var (
	ErrExpiredTimeWindow = errors.New("expired time window")
	ErrDuplicateSaleItem = errors.New("duplicate sale item")
)

type Sale struct {
	id        uuid.UUID
	state     SaleState
	startsAt  time.Time
	endsAt    time.Time
	items     []SaleItem
	createdAt time.Time
}

type SaleItem struct {
	id          uuid.UUID
	saleID      uuid.UUID
	productID   uuid.UUID
	name        string
	price       Money
	totalQty    int
	reservedQty int
	soldQty     int
}

func NewSale(
	id uuid.UUID,
	startsAt, endsAt time.Time,
	createdAt time.Time,
) (Sale, error) {
	if err := validateSale(
		id,
		startsAt,
		endsAt,
	); err != nil {
		return Sale{}, fmt.Errorf(
			"sale input validation: %w",
			err,
		)
	}

	return Sale{
		id:        id,
		state:     DraftState,
		startsAt:  startsAt,
		endsAt:    endsAt,
		items:     nil,
		createdAt: createdAt,
	}, nil
}

func RehydrateSale(snapshot SaleSnapshot) (Sale, error) {
	if err := validateSale(
		snapshot.ID,
		snapshot.StartsAt,
		snapshot.EndsAt,
	); err != nil {
		return Sale{}, fmt.Errorf(
			"sale snapshot validation: %w",
			err,
		)
	}

	switch snapshot.State {
	case DraftState:
	case ActiveState:
		if len(snapshot.Items) == 0 {
			return Sale{}, fmt.Errorf(
				"active sale must have at least 1 sale item: %w",
				ErrInvalidConfiguration,
			)
		}
	case EndedState:
		if len(snapshot.Items) == 0 {
			return Sale{}, fmt.Errorf(
				"ended sale must have at least 1 sale item: %w",
				ErrInvalidConfiguration,
			)
		}
	default:
		return Sale{}, fmt.Errorf(
			"invalid sale state: %s: %w",
			snapshot.State, ErrInvalidConfiguration,
		)
	}

	items := make([]SaleItem, 0, len(snapshot.Items))
	seen := make(map[uuid.UUID]struct{}, len(snapshot.Items))

	for idx, itemSnapshot := range snapshot.Items {
		if _, exists := seen[itemSnapshot.ID]; exists {
			return Sale{}, fmt.Errorf(
				"sale item already exists: %w",
				ErrDuplicateSaleItem,
			)
		}

		item, err := rehydrateSaleItem(snapshot.ID, itemSnapshot)
		if err != nil {
			return Sale{}, fmt.Errorf(
				"item %d failed to rehydrate: %w",
				idx, err,
			)
		}

		seen[itemSnapshot.ID] = struct{}{}
		items = append(items, item)
	}

	return Sale{
		id:        snapshot.ID,
		state:     snapshot.State,
		startsAt:  snapshot.StartsAt,
		endsAt:    snapshot.EndsAt,
		items:     items,
		createdAt: snapshot.CreatedAt,
	}, nil
}

func validateSale(
	id uuid.UUID,
	startsAt, endsAt time.Time,
) error {
	if id == uuid.Nil {
		return fmt.Errorf("sale id is empty: %w", ErrInvalidConfiguration)
	}

	if !startsAt.Before(endsAt) {
		return fmt.Errorf(
			"starts_at must be before ends_at: %w",
			ErrInvalidConfiguration,
		)
	}

	return nil
}

func rehydrateSaleItem(
	saleID uuid.UUID,
	snapshot SaleItemSnapshot,
) (SaleItem, error) {
	if snapshot.ID == uuid.Nil {
		return SaleItem{}, fmt.Errorf(
			"sale item id is empty: %w",
			ErrInvalidConfiguration,
		)
	}

	if snapshot.SaleID != saleID {
		return SaleItem{}, fmt.Errorf(
			"sale ids do not match: %w",
			ErrInvalidConfiguration,
		)
	}

	if snapshot.ProductID == uuid.Nil {
		return SaleItem{}, fmt.Errorf(
			"product id is empty: %w",
			ErrInvalidConfiguration,
		)
	}

	price, err := NewMoneyFromMinor(snapshot.PriceMinor)
	if err != nil {
		return SaleItem{}, fmt.Errorf(
			"price validation: %w",
			err,
		)
	}

	item := SaleItem{
		id:          snapshot.ID,
		saleID:      snapshot.SaleID,
		productID:   snapshot.ProductID,
		name:        snapshot.Name,
		price:       price,
		totalQty:    snapshot.TotalQty,
		reservedQty: snapshot.ReservedQty,
		soldQty:     snapshot.SoldQty,
	}

	if err := validateSaleItem(item); err != nil {
		return SaleItem{}, fmt.Errorf(
			"sale item validation: %w",
			err,
		)
	}

	return item, nil
}

func (s *Sale) ID() uuid.UUID {
	return s.id
}

func (s *Sale) State() SaleState {
	return s.state
}

func (s *Sale) StartsAt() time.Time {
	return s.startsAt
}

func (s *Sale) EndsAt() time.Time {
	return s.endsAt
}

func (s *Sale) CreatedAt() time.Time {
	return s.createdAt
}

func (s *Sale) Items() []SaleItem {
	itemsCopy := make([]SaleItem, len(s.items))
	copy(itemsCopy, s.items)

	return itemsCopy
}

func (s *Sale) Item(id uuid.UUID) (SaleItem, bool) {
	for _, item := range s.items {
		if item.id == id {
			return item, true
		}
	}

	return SaleItem{}, false
}

func (s *Sale) AddItem(
	id, productID uuid.UUID,
	name string,
	price Money,
	totalQty int,
) error {
	if s.State() != DraftState {
		return fmt.Errorf("sale is not draft: %w", ErrForbiddenTransition)
	}

	if s.hasItem(id) {
		return fmt.Errorf("sale item %s already exists: %w", id, ErrDuplicateSaleItem)
	}

	item := SaleItem{
		id:          id,
		saleID:      s.ID(),
		productID:   productID,
		name:        name,
		price:       price,
		totalQty:    totalQty,
		reservedQty: 0,
		soldQty:     0,
	}

	if err := validateSaleItem(item); err != nil {
		return fmt.Errorf("sale item validation: %w", err)
	}

	s.items = append(s.items, item)

	return nil
}

func (s *Sale) hasItem(id uuid.UUID) bool {
	for _, item := range s.items {
		if item.id == id {
			return true
		}
	}

	return false
}

func (si *SaleItem) ID() uuid.UUID {
	return si.id
}

func (si *SaleItem) SaleID() uuid.UUID {
	return si.saleID
}

func (si *SaleItem) ProductID() uuid.UUID {
	return si.productID
}

func (si *SaleItem) TotalQty() int {
	return si.totalQty
}

func (si *SaleItem) ReservedQty() int {
	return si.reservedQty
}

func (si *SaleItem) SoldQty() int {
	return si.soldQty
}

func (si *SaleItem) AvailableQty() int {
	return si.totalQty - si.reservedQty - si.soldQty
}

func (si *SaleItem) Name() string {
	return si.name
}

func (si *SaleItem) Price() Money {
	return si.price
}

func (s *Sale) Activate(now time.Time) error {
	if s.state != DraftState {
		return fmt.Errorf("state is not draft: %w", ErrForbiddenTransition)
	}

	if s.startsAt.After(s.endsAt) || s.startsAt.Equal(s.endsAt) {
		return fmt.Errorf("invalid starts_at/ends_at config: %w", ErrInvalidConfiguration)
	}

	if len(s.items) == 0 {
		return fmt.Errorf("items is empty: %w", ErrInvalidConfiguration)
	}

	for _, item := range s.items {
		if err := validateSaleItem(item); err != nil {
			return err
		}
	}

	if now.After(s.endsAt) || now.Equal(s.endsAt) {
		return fmt.Errorf("invalid ends_at config: %w", ErrExpiredTimeWindow)
	}

	s.state = ActiveState

	return nil
}

func validateSaleItem(item SaleItem) error {
	if item.id == uuid.Nil {
		return fmt.Errorf("sale item id is empty: %w", ErrInvalidConfiguration)
	}

	if item.saleID == uuid.Nil {
		return fmt.Errorf("sale id is empty: %w", ErrInvalidConfiguration)
	}

	if item.productID == uuid.Nil {
		return fmt.Errorf("product id is empty: %w", ErrInvalidConfiguration)
	}

	if item.totalQty <= 0 {
		return fmt.Errorf("total_qty must be > 0: %w", ErrInvalidQuantity)
	}

	if item.reservedQty < 0 {
		return fmt.Errorf("reserved_qty must be >= 0: %w", ErrInvalidQuantity)
	}

	if item.soldQty < 0 {
		return fmt.Errorf("sold_qty must be >= 0: %w", ErrInvalidQuantity)
	}

	if item.reservedQty > item.totalQty ||
		item.soldQty > item.totalQty-item.reservedQty {
		return fmt.Errorf("invalid quantity mathematics: %w", ErrInvalidQuantity)
	}

	if item.price.AmountMinor() <= 0 {
		return fmt.Errorf("price must be > 0: %w", ErrInvalidMoney)
	}

	return nil
}

func (s *Sale) End() error {
	if s.state != ActiveState {
		return fmt.Errorf("end error: %w", ErrForbiddenTransition)
	}

	s.state = EndedState

	return nil
}
