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
	if id == uuid.Nil {
		return Sale{}, fmt.Errorf("sale id is empty: %w", ErrInvalidConfiguration)
	}

	if !startsAt.Before(endsAt) {
		return Sale{}, fmt.Errorf(
			"starts_at must be before ends_at: %w",
			ErrInvalidConfiguration,
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
	id uuid.UUID,
	productID uuid.UUID,
	name string,
	price Money,
	totalQty int,
) error {
	if s.State() != DraftState {
		return fmt.Errorf("sale is not draft: %w", ErrForbiddenTransition)
	}

	if id == uuid.Nil {
		return fmt.Errorf("sale item id is empty: %w", ErrInvalidConfiguration)
	}

	if productID == uuid.Nil {
		return fmt.Errorf("product id is empty: %w", ErrInvalidConfiguration)
	}

	if totalQty <= 0 {
		return fmt.Errorf("total_qty must be > 0: %w", ErrInvalidQuantity)
	}

	if s.hasItem(id) {
		return fmt.Errorf("sale item %s already exists: %w", id, ErrDuplicateSaleItem)
	}

	if price.amountMinor <= 0 {
		return fmt.Errorf("price must be > 0: %w", ErrInvalidMoney)
	}

	s.items = append(s.items, SaleItem{
		id:          id,
		saleID:      s.ID(),
		productID:   productID,
		name:        name,
		price:       price,
		totalQty:    totalQty,
		reservedQty: 0,
		soldQty:     0,
	})

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
	if item.totalQty <= 0 {
		return fmt.Errorf("total_qty must be > 0: %w", ErrInvalidQuantity)
	}

	if item.reservedQty < 0 {
		return fmt.Errorf("reserved_qty must be >= 0: %w", ErrInvalidQuantity)
	}

	if item.soldQty < 0 {
		return fmt.Errorf("sold_qty must be >= 0: %w", ErrInvalidQuantity)
	}

	if item.soldQty+item.reservedQty > item.totalQty {
		return fmt.Errorf("invalid quantity mathematics: %w", ErrInvalidQuantity)
	}

	if item.price.amountMinor <= 0 {
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
