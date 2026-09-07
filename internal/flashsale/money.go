package flashsale

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

var (
	ErrInvalidMoney = errors.New("invalid money")
)

const (
	MinorUnitsPerRuble int64 = 100
)

type Money struct {
	amountMinor int64
}

func (m Money) AmountMinor() int64 {
	return m.amountMinor
}

func NewMoneyFromMinor(amount int64) (Money, error) {
	if amount < 0 {
		return Money{}, fmt.Errorf(
			"money amount must be non-negative: %w",
			ErrInvalidMoney,
		)
	}

	return Money{amountMinor: amount}, nil
}

func ParseMoney(val string) (Money, error) {
	if val == "" {
		return Money{}, fmt.Errorf(
			"money is empty: %w",
			ErrInvalidMoney,
		)
	}

	if strings.HasPrefix(val, "-") || strings.HasPrefix(val, "+") {
		return Money{}, fmt.Errorf(
			"money must be unsigned: %w",
			ErrInvalidMoney,
		)
	}

	parts := strings.Split(val, ".")
	if len(parts) > 2 {
		return Money{}, fmt.Errorf(
			"invalid money format: %w",
			ErrInvalidMoney,
		)
	}

	integerPart := parts[0]
	if err := validatePart(integerPart); err != nil {
		return Money{}, fmt.Errorf(
			"validate integer part: %w",
			err,
		)
	}

	var fractionPart string

	if len(parts) == 2 {
		fractionPart = parts[1]

		if err := validatePart(fractionPart); err != nil {
			return Money{}, fmt.Errorf(
				"validate fraction part: %w",
				err,
			)
		}
	}

	integer, err := strconv.ParseUint(integerPart, 10, 64)
	if err != nil {
		return Money{}, fmt.Errorf(
			"parse integer part: %w: %w",
			err,
			ErrInvalidMoney,
		)
	}

	fraction := uint64(0)

	switch len(fractionPart) {
	case 0:
	case 1:
		fraction = uint64(fractionPart[0]-'0') * 10
	case 2:
		fraction = uint64(fractionPart[0]-'0')*10 +
			uint64(fractionPart[1]-'0')
	default:
		return Money{}, fmt.Errorf(
			"invalid fraction part format: %w",
			ErrInvalidMoney,
		)
	}

	const maxInt64 = int64(1<<63 - 1)

	maxInt := uint64(maxInt64 / MinorUnitsPerRuble)
	maxFract := uint64(maxInt64 % MinorUnitsPerRuble)

	if integer > maxInt ||
		(integer == maxInt && fraction > maxFract) {
		return Money{}, fmt.Errorf("money overflow: %w", ErrInvalidMoney)
	}

	return Money{
		amountMinor: int64(integer*uint64(MinorUnitsPerRuble)) + int64(fraction),
	}, nil

}

func validatePart(part string) error {
	if part == "" || !isDigits(part) {
		return fmt.Errorf(
			"invalid part (%s): %w",
			part, ErrInvalidMoney,
		)
	}

	return nil
}

func isDigits(val string) bool {
	for _, r := range val {
		if r < '0' || r > '9' {
			return false
		}
	}

	return true
}
