package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
)

type qtyInput int

var (
	errInvalidQtyInput = errors.New("invalid quantity input")
)

func (q *qtyInput) UnmarshalJSON(data []byte) error {
	value, err := parseQtyJSON(data)
	if err != nil {
		return err
	}

	*q = qtyInput(value)

	return nil
}

func parseQtyJSON(data []byte) (int, error) {
	raw := bytes.Trim(data, " \t\r\n")

	if len(raw) == 0 ||
		raw[0] < '0' || raw[0] > '9' ||
		!json.Valid(raw) {
		return 0, errInvalidQtyInput
	}

	mantissa, exp, err := splitQtyExponent(string(raw))
	if err != nil {
		return 0, err
	}

	digits := mantissa
	var fractLength int64

	if index := strings.IndexByte(mantissa, '.'); index >= 0 {
		fractLength = int64(len(mantissa) - index - 1)
		digits = mantissa[:index] + mantissa[index+1:]
	}

	digits = strings.TrimLeft(digits, "0")
	if digits == "" {
		return 0, errInvalidQtyInput
	}

	const maxQtyDigits int64 = 10
	digitsLength := int64(len(digits))

	minExp := fractLength - digitsLength + 1
	maxExp := fractLength - digitsLength + maxQtyDigits

	if exp < minExp || exp > maxExp {
		return 0, errInvalidQtyInput
	}

	shift := exp - fractLength

	if shift >= 0 {
		digits += strings.Repeat("0", int(shift))
	} else {
		cut := int(digitsLength + shift)

		for index := cut; index < len(digits); index++ {
			if digits[index] != '0' {
				return 0, errInvalidQtyInput
			}
		}

		digits = digits[:cut]
	}

	value, err := strconv.ParseInt(digits, 10, 32)
	if err != nil || value < 1 {
		return 0, errInvalidQtyInput
	}

	return int(value), nil
}

func splitQtyExponent(number string) (string, int64, error) {
	index := strings.IndexAny(number, "eE")
	if index < 0 {
		return number, 0, nil
	}

	exponent, err := strconv.ParseInt(number[index+1:], 10, 64)
	if err != nil {
		return "", 0, errInvalidQtyInput
	}

	return number[:index], exponent, nil
}
