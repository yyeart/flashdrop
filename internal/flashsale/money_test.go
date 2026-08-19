package flashsale

import (
	"errors"
	"math"
	"testing"
)

func TestParseMoney(t *testing.T) {
	tests := []struct {
		name            string
		value           string
		wantAmountMinor int64
	}{
		{
			name:            "zero",
			value:           "0",
			wantAmountMinor: 0,
		},
		{
			name:            "one minor unit",
			value:           "0.01",
			wantAmountMinor: 1,
		},
		{
			name:            "whole amount",
			value:           "12",
			wantAmountMinor: 1_200,
		},
		{
			name:            "one fractional digit",
			value:           "12.5",
			wantAmountMinor: 1_250,
		},
		{
			name:            "two fractional digits",
			value:           "12.50",
			wantAmountMinor: 1_250,
		},
		{
			name:            "ten minor units",
			value:           "0.10",
			wantAmountMinor: 10,
		},
		{
			name:            "maximum int64 amount",
			value:           "92233720368547758.07",
			wantAmountMinor: 9_223_372_036_854_775_807,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			money, err := ParseMoney(tt.value)
			if err != nil {
				t.Fatalf("ParseMoney(%q) error = %v", tt.value, err)
			}

			if money.AmountMinor() != tt.wantAmountMinor {
				t.Fatalf(
					"ParseMoney(%q).AmountMinor() = %d, want %d",
					tt.value,
					money.AmountMinor(),
					tt.wantAmountMinor,
				)
			}
		})
	}
}

func TestParseMoneyRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name  string
		value string
	}{
		{name: "empty", value: ""},
		{name: "negative", value: "-1.00"},
		{name: "explicit plus", value: "+1.00"},
		{name: "more than two fractional digits", value: "12.505"},
		{name: "missing fraction", value: "12."},
		{name: "missing whole part", value: ".50"},
		{name: "multiple separators", value: "12.5.0"},
		{name: "comma separator", value: "12,50"},
		{name: "scientific notation", value: "1e3"},
		{name: "letters", value: "abc"},
		{name: "overflow by one minor unit", value: "92233720368547758.08"},
		{name: "whole part over uint64", value: "18446744073709551616.00"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseMoney(tt.value)

			if !errors.Is(err, ErrInvalidMoney) {
				t.Fatalf(
					"ParseMoney(%q) error = %v, want ErrInvalidMoney",
					tt.value,
					err,
				)
			}
		})
	}
}

func TestParseMoneyDoesNotRoundExtraPrecision(t *testing.T) {
	_, err := ParseMoney("12.505")

	if !errors.Is(err, ErrInvalidMoney) {
		t.Fatalf(
			"ParseMoney(%q) error = %v, want ErrInvalidMoney",
			"12.505",
			err,
		)
	}
}

func TestParseMoneyUsesExactMinorUnits(t *testing.T) {
	ten, err := ParseMoney("0.10")
	if err != nil {
		t.Fatalf("ParseMoney(%q) error = %v", "0.10", err)
	}

	twenty, err := ParseMoney("0.20")
	if err != nil {
		t.Fatalf("ParseMoney(%q) error = %v", "0.20", err)
	}

	if got := ten.AmountMinor() + twenty.AmountMinor(); got != 30 {
		t.Fatalf("0.10 + 0.20 = %d minor units, want 30", got)
	}
}

func TestNewMoneyFromMinor(t *testing.T) {
	tests := []struct {
		name        string
		amountMinor int64
		want        int64
	}{
		{
			name:        "zero",
			amountMinor: 0,
			want:        0,
		},
		{
			name:        "one minor unit",
			amountMinor: 1,
			want:        1,
		},
		{
			name:        "one ruble",
			amountMinor: MinorUnitsPerRuble,
			want:        100,
		},
		{
			name:        "regular amount",
			amountMinor: 1250,
			want:        1250,
		},
		{
			name:        "max int64",
			amountMinor: math.MaxInt64,
			want:        math.MaxInt64,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			money, err := NewMoneyFromMinor(tt.amountMinor)
			if err != nil {
				t.Fatalf("NewMoneyFromMinor() error = %v", err)
			}

			if money.AmountMinor() != tt.want {
				t.Fatalf(
					"AmountMinor() = %d, want %d",
					money.AmountMinor(),
					tt.want,
				)
			}
		})
	}
}

func TestNewMoneyFromMinorRejectsNegativeAmount(t *testing.T) {
	tests := []struct {
		name        string
		amountMinor int64
	}{
		{
			name:        "minus one",
			amountMinor: -1,
		},
		{
			name:        "negative regular amount",
			amountMinor: -1250,
		},
		{
			name:        "min int64",
			amountMinor: math.MinInt64,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewMoneyFromMinor(tt.amountMinor)

			if !errors.Is(err, ErrInvalidMoney) {
				t.Fatalf(
					"NewMoneyFromMinor() error = %v, want ErrInvalidMoney",
					err,
				)
			}
		})
	}
}
