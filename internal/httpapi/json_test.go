package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDecodeJSON_DecodesObject(t *testing.T) {
	type payload struct {
		Name string `json:"name"`
	}

	request := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		"/",
		strings.NewReader(`{"name":"flashdrop"}`),
	)
	request.Header.Set("Content-Type", "application/json")

	var got payload
	if err := decodeJSON(httptest.NewRecorder(), request, &got); err != nil {
		t.Fatalf("decodeJSON() error = %v, want nil", err)
	}
	if got.Name != "flashdrop" {
		t.Errorf("decoded name = %q, want %q", got.Name, "flashdrop")
	}
}
