package httpapi

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/yyeart/flashdrop/internal/flashsale"
)

func TestSaleToResponse_MapsAllFields(t *testing.T) {
	t.Parallel()

	saleID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	itemID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	productID := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	startsAt := time.Date(2026, time.September, 15, 10, 0, 0, 0, time.UTC)
	endsAt := startsAt.Add(time.Hour)
	createdAt := startsAt.Add(-time.Hour)

	sale, err := flashsale.RehydrateSale(flashsale.SaleSnapshot{
		ID:        saleID,
		State:     flashsale.ActiveState,
		StartsAt:  startsAt,
		EndsAt:    endsAt,
		CreatedAt: createdAt,
		Items: []flashsale.SaleItemSnapshot{
			{
				ID:          itemID,
				SaleID:      saleID,
				ProductID:   productID,
				Name:        "Mechanical keyboard",
				PriceMinor:  1250,
				TotalQty:    10,
				ReservedQty: 3,
				SoldQty:     2,
			},
		},
	})
	if err != nil {
		t.Fatalf("RehydrateSale() error = %v, want nil", err)
	}

	got := saleToResponse(sale)
	want := saleResponse{
		ID:        saleID,
		State:     flashsale.ActiveState,
		StartsAt:  startsAt,
		EndsAt:    endsAt,
		CreatedAt: createdAt,
		Items: []saleItemResponse{
			{
				ID:                itemID,
				SaleID:            saleID,
				ProductID:         productID,
				Name:              "Mechanical keyboard",
				Price:             "12.50",
				TotalQuantity:     10,
				ReservedQuantity:  3,
				SoldQuantity:      2,
				AvailableQuantity: 5,
			},
		},
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("saleToResponse() = %#v, want %#v", got, want)
	}
}

func TestMoneyToString_UsesCanonicalRepresentation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		amountMinor int64
		want        string
	}{
		{name: "fractional rubles", amountMinor: 1250, want: "12.50"},
		{name: "whole rubles", amountMinor: 1200, want: "12.00"},
		{name: "one kopeck", amountMinor: 1, want: "0.01"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			money, err := flashsale.NewMoneyFromMinor(tt.amountMinor)
			if err != nil {
				t.Fatalf("NewMoneyFromMinor(%d) error = %v, want nil", tt.amountMinor, err)
			}

			if got := moneyToString(money); got != tt.want {
				t.Errorf("moneyToString() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSaleToResponse_EmptyItemsMarshalAsArray(t *testing.T) {
	t.Parallel()

	startsAt := time.Date(2026, time.September, 15, 10, 0, 0, 0, time.UTC)
	sale, err := flashsale.NewSale(
		uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		startsAt,
		startsAt.Add(time.Hour),
		startsAt.Add(-time.Hour),
	)
	if err != nil {
		t.Fatalf("NewSale() error = %v, want nil", err)
	}

	data, err := json.Marshal(saleToResponse(sale))
	if err != nil {
		t.Fatalf("json.Marshal() error = %v, want nil", err)
	}
	if !strings.Contains(string(data), `"items":[]`) {
		t.Errorf("marshaled Sale = %s, want items to be an empty array", data)
	}
	if strings.Contains(string(data), `"items":null`) {
		t.Errorf("marshaled Sale = %s, items must not be null", data)
	}
}

func TestSalePageResponse_EmptySalesMarshalAsArray(t *testing.T) {
	t.Parallel()

	response := salePageResponse{
		Sales:  make([]saleResponse, 0),
		Limit:  20,
		Offset: 0,
	}

	data, err := json.Marshal(response)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v, want nil", err)
	}

	want := `{"sales":[],"limit":20,"offset":0}`
	if string(data) != want {
		t.Errorf("json.Marshal() = %s, want %s", data, want)
	}
}
