package identity_postgres_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/yyeart/flashdrop/internal/identity"
	identity_postgres "github.com/yyeart/flashdrop/internal/identity/postgres"
)

func TestStore_FindLoginCreds_ExistingUser(t *testing.T) {
	pool := openTestPool(t)
	store := identity_postgres.NewStore(pool)
	service, err := identity.NewService(store, testTokenConfig())
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), postgresOperationTimeout)
	defer cancel()

	email := uniqueEmail("find-existing")
	user, err := service.Register(ctx, identity.RegisterInput{
		Email:    "  " + strings.ToUpper(email) + "  ",
		Password: integrationPassword,
	})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	cleanupUsers(t, pool, user.ID())

	credentials, found, err := store.FindLoginCreds(ctx, email)
	if err != nil || !found {
		t.Fatalf("FindLoginCreds() found = %v, error = %v; want true, nil", found, err)
	}
	if credentials.UserID != user.ID() || credentials.Role != user.Role() {
		t.Fatalf("credentials ID/role = %s/%s, want %s/%s",
			credentials.UserID, credentials.Role, user.ID(), user.Role())
	}
	encoded, err := credentials.PasswordHash.Encoded()
	if err != nil {
		t.Fatalf("PasswordHash.Encoded() error = %v", err)
	}
	var storedHash string
	if err := pool.QueryRow(ctx,
		`SELECT password_hash FROM flashdrop.user_credentials WHERE user_id = $1`,
		user.ID(),
	).Scan(&storedHash); err != nil {
		t.Fatalf("read stored hash: %v", err)
	}
	if encoded != storedHash {
		t.Fatal("returned password hash differs from stored hash")
	}
}

func TestStore_FindLoginCreds_MissingUser(t *testing.T) {
	pool := openTestPool(t)
	store := identity_postgres.NewStore(pool)
	ctx, cancel := context.WithTimeout(context.Background(), postgresOperationTimeout)
	defer cancel()

	_, found, err := store.FindLoginCreds(ctx, uniqueEmail("find-missing"))
	if err != nil || found {
		t.Fatalf("FindLoginCreds() found = %v, error = %v; want false, nil", found, err)
	}
}

func TestStore_FindLoginCreds_RejectsCorruptHash(t *testing.T) {
	pool := openTestPool(t)
	store := identity_postgres.NewStore(pool)
	service, err := identity.NewService(store, testTokenConfig())
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), postgresOperationTimeout)
	defer cancel()

	email := uniqueEmail("find-corrupt")
	user, err := service.Register(ctx, identity.RegisterInput{
		Email:    email,
		Password: integrationPassword,
	})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	cleanupUsers(t, pool, user.ID())
	tag, err := pool.Exec(ctx,
		`UPDATE flashdrop.user_credentials SET password_hash = $1 WHERE user_id = $2`,
		"invalid-phc", user.ID())
	if err != nil {
		t.Fatalf("corrupt stored hash: %v", err)
	}
	if tag.RowsAffected() != 1 {
		t.Fatalf("updated rows = %d, want 1", tag.RowsAffected())
	}

	_, found, err := store.FindLoginCreds(ctx, email)
	if err == nil || found {
		t.Fatalf("FindLoginCreds() found = %v, error = %v; want false and error", found, err)
	}
}

func TestStore_FindLoginCreds_CanceledContext(t *testing.T) {
	pool := openTestPool(t)
	store := identity_postgres.NewStore(pool)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, found, err := store.FindLoginCreds(ctx, uniqueEmail("find-canceled"))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("FindLoginCreds() error = %v, want context.Canceled", err)
	}
	if found {
		t.Fatal("FindLoginCreds() found = true with canceled context")
	}
}
