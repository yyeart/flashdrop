package httpapi

import (
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/yyeart/flashdrop/internal/flashsale"
)

type saleItemResponse struct {
	ID                uuid.UUID `json:"id"`
	SaleID            uuid.UUID `json:"sale_id"`
	ProductID         uuid.UUID `json:"product_id"`
	Name              string    `json:"name"`
	Price             string    `json:"price"`
	TotalQuantity     int       `json:"total_quantity"`
	ReservedQuantity  int       `json:"reserved_quantity"`
	SoldQuantity      int       `json:"sold_quantity"`
	AvailableQuantity int       `json:"available_quantity"`
}

type saleResponse struct {
	ID        uuid.UUID           `json:"id"`
	State     flashsale.SaleState `json:"state"`
	StartsAt  time.Time           `json:"starts_at"`
	EndsAt    time.Time           `json:"ends_at"`
	CreatedAt time.Time           `json:"created_at"`
	Items     []saleItemResponse  `json:"items"`
}

type salePageResponse struct {
	Sales  []saleResponse `json:"sales"`
	Limit  int            `json:"limit"`
	Offset int            `json:"offset"`
}

type createSaleRequest struct {
	StartsAt *time.Time `json:"starts_at"`
	EndsAt   *time.Time `json:"ends_at"`
}

type addSaleItemRequest struct {
	ProductID     uuid.UUID `json:"product_id"`
	Name          *string   `json:"name"`
	Price         string    `json:"price"`
	TotalQuantity int       `json:"total_quantity"`
}

func saleToResponse(sale flashsale.Sale) saleResponse {
	items := make([]saleItemResponse, 0, len(sale.Items()))
	for _, item := range sale.Items() {
		items = append(items, saleItemToResponse(item))
	}

	return saleResponse{
		ID:        sale.ID(),
		State:     sale.State(),
		StartsAt:  sale.StartsAt(),
		EndsAt:    sale.EndsAt(),
		CreatedAt: sale.CreatedAt(),
		Items:     items,
	}
}

func saleItemToResponse(item flashsale.SaleItem) saleItemResponse {
	return saleItemResponse{
		ID:                item.ID(),
		SaleID:            item.SaleID(),
		ProductID:         item.ProductID(),
		Name:              item.Name(),
		Price:             moneyToString(item.Price()),
		TotalQuantity:     item.TotalQty(),
		ReservedQuantity:  item.ReservedQty(),
		SoldQuantity:      item.SoldQty(),
		AvailableQuantity: item.AvailableQty(),
	}
}

func moneyToString(money flashsale.Money) string {
	amountMinor := money.AmountMinor()
	rubles := amountMinor / flashsale.MinorUnitsPerRuble
	kopecks := amountMinor % flashsale.MinorUnitsPerRuble

	return fmt.Sprintf("%d.%02d", rubles, kopecks)
}
