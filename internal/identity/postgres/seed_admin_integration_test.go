package identity_postgres_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/yyeart/flashdrop/internal/identity"
	identity_postgres "github.com/yyeart/flashdrop/internal/identity/postgres"
)

func TestAdminSeeder_Integration_NormalizedAndIdempotent(t *testing.T) {
	pool := openTestPool(t)
	store := identity_postgres.NewStore(pool)
	seeder := identity.NewAdminSeeder(store)
	email := uniqueEmail("seed-admin")

	ctx, cancel := context.WithTimeout(context.Background(), postgresOperationTimeout)
	defer cancel()

	err := seeder.SeedAdmin(ctx, identity.SeedAdminInput{
		Email:    "  " + strings.ToUpper(email) + "  ",
		Password: integrationPassword,
	})
	if err != nil {
		t.Fatalf("SeedAdmin(first) error = %v", err)
	}

	credentials, found, err := store.FindLoginCreds(ctx, email)
	if err != nil {
		t.Fatalf("FindLoginCreds() error = %v", err)
	}
	if !found {
		t.Fatal("FindLoginCreds() found = false after SeedAdmin()")
	}
	cleanupUsers(t, pool, credentials.UserID)
	if credentials.Role != identity.RoleAdmin {
		t.Fatalf("stored role = %q, want %q", credentials.Role, identity.RoleAdmin)
	}
	encodedHash, err := credentials.PasswordHash.Encoded()
	if err != nil {
		t.Fatalf("stored password hash is invalid: %v", err)
	}
	if encodedHash == integrationPassword {
		t.Fatal("stored password hash contains the plaintext password")
	}

	if err := seeder.SeedAdmin(ctx, identity.SeedAdminInput{
		Email:    email,
		Password: integrationPassword,
	}); err != nil {
		t.Fatalf("SeedAdmin(repeated) error = %v", err)
	}
	if got := countCredentialsByEmail(t, pool, email); got != 1 {
		t.Fatalf("credentials with email %q = %d, want 1", email, got)
	}

	err = seeder.SeedAdmin(ctx, identity.SeedAdminInput{
		Email:    email,
		Password: "another valid password",
	})
	if !errors.Is(err, identity.ErrSeedAdminConflict) {
		t.Fatalf("SeedAdmin(other password) error = %v, want ErrSeedAdminConflict", err)
	}
}

func TestAdminSeeder_Integration_ExistingUserConflicts(t *testing.T) {
	pool := openTestPool(t)
	store := identity_postgres.NewStore(pool)
	email := uniqueEmail("seed-existing-user")
	user := newTestUser(t, email)
	cleanupUsers(t, pool, user.ID())

	ctx, cancel := context.WithTimeout(context.Background(), postgresOperationTimeout)
	defer cancel()
	if err := store.CreateUser(ctx, user, newTestPasswordHash(t)); err != nil {
		t.Fatalf("CreateUser() error = %v", err)
	}

	err := identity.NewAdminSeeder(store).SeedAdmin(ctx, identity.SeedAdminInput{
		Email:    email,
		Password: integrationPassword,
	})
	if !errors.Is(err, identity.ErrSeedAdminConflict) {
		t.Fatalf("SeedAdmin() error = %v, want ErrSeedAdminConflict", err)
	}
	if got := countCredentialsByEmail(t, pool, email); got != 1 {
		t.Fatalf("credentials with email %q = %d, want 1", email, got)
	}
}

func TestAdminSeeder_Integration_ConcurrentSeedIsIdempotent(t *testing.T) {
	pool := openTestPool(t)
	store := identity_postgres.NewStore(pool)
	seeder := identity.NewAdminSeeder(store)
	email := uniqueEmail("seed-concurrent")

	ctx, cancel := context.WithTimeout(context.Background(), 2*postgresOperationTimeout)
	defer cancel()

	const goroutines = 2
	results := make(chan error, goroutines)
	var waitGroup sync.WaitGroup
	for range goroutines {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			results <- seeder.SeedAdmin(ctx, identity.SeedAdminInput{
				Email:    email,
				Password: integrationPassword,
			})
		}()
	}
	waitGroup.Wait()
	close(results)

	for err := range results {
		if err != nil {
			t.Fatalf("concurrent SeedAdmin() error = %v", err)
		}
	}

	credentials, found, err := store.FindLoginCreds(ctx, email)
	if err != nil {
		t.Fatalf("FindLoginCreds() error = %v", err)
	}
	if !found {
		t.Fatal("FindLoginCreds() found = false after concurrent SeedAdmin()")
	}
	cleanupUsers(t, pool, credentials.UserID)
	if credentials.Role != identity.RoleAdmin {
		t.Fatalf("stored role = %q, want %q", credentials.Role, identity.RoleAdmin)
	}
	if got := countCredentialsByEmail(t, pool, email); got != 1 {
		t.Fatalf("credentials with email %q = %d, want 1", email, got)
	}
}

func TestAdminSeeder_Integration_CanceledContextDoesNotPersist(t *testing.T) {
	pool := openTestPool(t)
	store := identity_postgres.NewStore(pool)
	email := uniqueEmail("seed-canceled")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := identity.NewAdminSeeder(store).SeedAdmin(ctx, identity.SeedAdminInput{
		Email:    email,
		Password: integrationPassword,
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("SeedAdmin() error = %v, want context.Canceled", err)
	}

	verificationCtx, verificationCancel := context.WithTimeout(context.Background(), postgresOperationTimeout)
	defer verificationCancel()
	_, found, err := store.FindLoginCreds(verificationCtx, email)
	if err != nil {
		t.Fatalf("FindLoginCreds() error = %v", err)
	}
	if found {
		t.Fatal("canceled SeedAdmin() persisted credentials")
	}
}
