package httpapi

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestParseQtyJSON_AcceptsIntegralNumbers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  int
	}{
		{"minimum", "1", 1},
		{"decimal", "1.0", 1},
		{"decimal trailing zeros", "12.000", 12},
		{"exponent", "1e0", 1},
		{"uppercase exponent with plus", "1E+0", 1},
		{"positive exponent", "12e2", 1200},
		{"negative exponent", "120e-1", 12},
		{"fraction becomes integer", "1.20e1", 12},
		{"leading fractional zeros", "0.01e2", 1},
		{"negative zero exponent", "1e-0", 1},
		{"JSON whitespace", " \t\r\n1.0\n\t ", 1},
		{"maximum", "2147483647", 2147483647},
		{"maximum decimal", "2147483647.0", 2147483647},
		{"maximum exponent", "2.147483647e9", 2147483647},
		{"large mantissa becomes maximum", "214748364700e-2", 2147483647},
		{"long fractional zeros", "1." + strings.Repeat("0", 128), 1},
		{"long mantissa becomes minimum", "1" + strings.Repeat("0", 128) + "e-128", 1},
		{"exponent leading zeros", "1e+" + strings.Repeat("0", 64) + "1", 10},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := parseQtyJSON([]byte(test.input))
			if err != nil {
				t.Fatalf("parseQtyJSON(%q) error = %v, want nil", test.input, err)
			}
			if got != test.want {
				t.Errorf("parseQtyJSON(%q) = %d, want %d", test.input, got, test.want)
			}
		})
	}
}

func TestParseQtyJSON_RejectsInvalidNumbersAndTypes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
	}{
		{"empty", ""},
		{"whitespace only", " \t\r\n"},
		{"zero", "0"},
		{"zero with exponent", "0.00e100"},
		{"negative zero", "-0"},
		{"negative", "-1"},
		{"fraction", "1.5"},
		{"fraction with exponent", "125e-1"},
		{"below minimum", "1e-1"},
		{"fraction beyond float precision", "1.0000000000000000001"},
		{"fraction near maximum", "2147483647.0000000001"},
		{"above maximum", "2147483648"},
		{"above maximum decimal", "2147483648.0"},
		{"above maximum exponent", "2.147483648e9"},
		{"too many integral digits", "1e10"},
		{"huge positive exponent", "1e1000000000"},
		{"huge negative exponent", "1e-1000000000"},
		{"maximum int64 exponent", "1e9223372036854775807"},
		{"minimum int64 exponent with fraction", "1.0e-9223372036854775808"},
		{"positive exponent overflow", "1e9223372036854775808"},
		{"negative exponent overflow", "1e-9223372036854775809"},
		{"quoted number", `"1"`},
		{"null", "null"},
		{"boolean", "true"},
		{"array", "[1]"},
		{"object", `{"quantity":1}`},
		{"leading zero", "01"},
		{"leading plus", "+1"},
		{"missing integral part", ".1"},
		{"missing fractional part", "1."},
		{"missing exponent", "1e"},
		{"missing exponent digits", "1e+"},
		{"multiple JSON values", "1 2"},
		{"non-JSON whitespace", "\u00a01"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := parseQtyJSON([]byte(test.input))
			if !errors.Is(err, errInvalidQtyInput) {
				t.Fatalf("parseQtyJSON(%q) error = %v, want errInvalidQtyInput", test.input, err)
			}
			if got != 0 {
				t.Errorf("parseQtyJSON(%q) = %d on error, want 0", test.input, got)
			}
		})
	}
}

func TestQtyInput_UnmarshalJSON_UpdatesValueOnSuccess(t *testing.T) {
	t.Parallel()

	var input struct {
		Quantity qtyInput `json:"quantity"`
	}
	input.Quantity = 7

	if err := json.Unmarshal([]byte(`{"quantity":1.20e1}`), &input); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	if input.Quantity != 12 {
		t.Errorf("Quantity = %d, want 12", input.Quantity)
	}
}

func TestQtyInput_UnmarshalJSON_PreservesValueOnError(t *testing.T) {
	t.Parallel()

	for _, input := range []string{"1.5", "2147483648", "null", `"1"`} {
		t.Run(input, func(t *testing.T) {
			quantity := qtyInput(7)
			err := json.Unmarshal([]byte(input), &quantity)
			if !errors.Is(err, errInvalidQtyInput) {
				t.Fatalf("json.Unmarshal(%q) error = %v, want errInvalidQtyInput", input, err)
			}
			if quantity != 7 {
				t.Errorf("quantity = %d after error, want original value 7", quantity)
			}
		})
	}
}
