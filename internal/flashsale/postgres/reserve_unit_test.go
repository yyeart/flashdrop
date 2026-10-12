package flashsale_postgres

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/yyeart/flashdrop/internal/flashsale"
)

func TestHashReserveCommand_IgnoresServerGeneratedFields(t *testing.T) {
	base := flashsale.ReserveCommand{
		ReservationID:  uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		UserID:         uuid.MustParse("22222222-2222-2222-2222-222222222222"),
		SaleItemID:     uuid.MustParse("33333333-3333-3333-3333-333333333333"),
		Quantity:       2,
		ExpiresAt:      time.Date(2026, time.September, 19, 12, 15, 0, 0, time.UTC),
		IdempotencyKey: "first-key",
	}

	want, err := hashReserveCommand(base)
	if err != nil {
		t.Fatalf("hashReserveCommand(base) error = %v", err)
	}

	changed := base
	changed.ReservationID = uuid.MustParse("44444444-4444-4444-4444-444444444444")
	changed.ExpiresAt = base.ExpiresAt.Add(time.Hour)
	changed.IdempotencyKey = "another-key"

	got, err := hashReserveCommand(changed)
	if err != nil {
		t.Fatalf("hashReserveCommand(changed) error = %v", err)
	}
	if got != want {
		t.Fatalf("hash changed with server/idempotency fields: got %q, want %q", got, want)
	}
}

func TestHashReserveCommand_IncludesSemanticPayload(t *testing.T) {
	base := flashsale.ReserveCommand{
		ReservationID:  uuid.New(),
		UserID:         uuid.MustParse("22222222-2222-2222-2222-222222222222"),
		SaleItemID:     uuid.MustParse("33333333-3333-3333-3333-333333333333"),
		Quantity:       2,
		ExpiresAt:      time.Now().Add(time.Minute),
		IdempotencyKey: "key",
	}

	want, err := hashReserveCommand(base)
	if err != nil {
		t.Fatalf("hashReserveCommand(base) error = %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*flashsale.ReserveCommand)
	}{
		{
			name: "user ID",
			mutate: func(cmd *flashsale.ReserveCommand) {
				cmd.UserID = uuid.New()
			},
		},
		{
			name: "sale item ID",
			mutate: func(cmd *flashsale.ReserveCommand) {
				cmd.SaleItemID = uuid.New()
			},
		},
		{
			name: "quantity",
			mutate: func(cmd *flashsale.ReserveCommand) {
				cmd.Quantity++
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			changed := base
			tt.mutate(&changed)

			got, err := hashReserveCommand(changed)
			if err != nil {
				t.Fatalf("hashReserveCommand() error = %v", err)
			}
			if got == want {
				t.Fatalf("hash did not change after changing %s", tt.name)
			}
		})
	}
}
