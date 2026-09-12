package identity_postgres_test

import (
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yyeart/flashdrop/internal/identity"
	identity_postgres "github.com/yyeart/flashdrop/internal/identity/postgres"
)

const (
	postgresOperationTimeout = 5 * time.Second
	integrationPassword      = "correct horse battery staple"
)

type capturedUserRepository struct {
	user         identity.User
	passwordHash identity.PasswordHash
}

func (r *capturedUserRepository) CreateUser(
	_ context.Context,
	user identity.User,
	passwordHash identity.PasswordHash,
) error {
	r.user = user
	r.passwordHash = passwordHash
	return nil
}

func (r *capturedUserRepository) FindLoginCreds(
	context.Context,
	string,
) (identity.LoginCredentials, bool, error) {
	return identity.LoginCredentials{}, false, nil
}

func testTokenConfig() identity.TokenConfig {
	privateKey := ed25519.NewKeyFromSeed(make([]byte, ed25519.SeedSize))
	publicKey, ok := privateKey.Public().(ed25519.PublicKey)
	if !ok {
		panic("ed25519 private key returned a non-ed25519 public key")
	}

	return identity.TokenConfig{
		Issuer:     "flashdrop",
		Audience:   "flashdrop-api",
		TTL:        15 * time.Minute,
		PrivateKey: privateKey,
		PublicKey:  publicKey,
	}
}

func TestRegister_PersistsNormalizedUserAndPasswordHash(t *testing.T) {
	pool := openTestPool(t)
	store := identity_postgres.NewStore(pool)
	service, err := identity.NewService(store, testTokenConfig())
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), postgresOperationTimeout)
	defer cancel()

	email := uniqueEmail("persist")
	inputEmail := "  " + strings.ToUpper(email) + "  "
	registered, err := service.Register(ctx, identity.RegisterInput{
		Email:    inputEmail,
		Password: integrationPassword,
	})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	cleanupUsers(t, pool, registered.ID())

	var storedEmail string
	var storedHash string
	var storedRole identity.Role
	var storedCreatedAt time.Time
	err = pool.QueryRow(
		ctx,
		`SELECT c.email, c.password_hash, u.role, u.created_at
		 FROM flashdrop.users AS u
		 JOIN flashdrop.user_credentials AS c ON c.user_id = u.id
		 WHERE u.id = $1`,
		registered.ID(),
	).Scan(&storedEmail, &storedHash, &storedRole, &storedCreatedAt)
	if err != nil {
		t.Fatalf("read registered user: %v", err)
	}

	if registered.ID() == uuid.Nil {
		t.Fatal("Register() ID is nil, want a server-generated UUID")
	}
	if registered.Email() != email || storedEmail != email {
		t.Fatalf(
			"emails = returned %q, stored %q; want %q",
			registered.Email(), storedEmail, email,
		)
	}
	if registered.Role() != identity.RoleUser || storedRole != identity.RoleUser {
		t.Fatalf(
			"roles = returned %q, stored %q; want %q",
			registered.Role(), storedRole, identity.RoleUser,
		)
	}
	if !registered.CreatedAt().Equal(storedCreatedAt) {
		t.Fatalf(
			"created_at = returned %s, stored %s",
			registered.CreatedAt(), storedCreatedAt,
		)
	}
	if storedHash == integrationPassword {
		t.Fatal("password_hash contains the plaintext password")
	}
	if !strings.HasPrefix(storedHash, "$argon2id$") {
		t.Fatalf("password_hash = %q, want Argon2id PHC string", storedHash)
	}
}

func TestStore_CreateUser_DuplicateEmailRollsBackUser(t *testing.T) {
	pool := openTestPool(t)
	store := identity_postgres.NewStore(pool)
	ctx, cancel := context.WithTimeout(context.Background(), postgresOperationTimeout)
	defer cancel()

	email := uniqueEmail("duplicate")
	first := newTestUser(t, email)
	second := newTestUser(t, email)
	passwordHash := newTestPasswordHash(t)
	cleanupUsers(t, pool, first.ID(), second.ID())

	if err := store.CreateUser(ctx, first, passwordHash); err != nil {
		t.Fatalf("CreateUser(first) error = %v", err)
	}

	err := store.CreateUser(ctx, second, passwordHash)
	if !errors.Is(err, identity.ErrEmailAlreadyExists) {
		t.Fatalf("CreateUser(second) error = %v, want ErrEmailAlreadyExists", err)
	}
	for _, leaked := range []string{"duplicate key", "uq_user_credentials_email", "flashdrop.user_credentials"} {
		if strings.Contains(strings.ToLower(err.Error()), strings.ToLower(leaked)) {
			t.Fatalf("duplicate email error %q leaks PostgreSQL detail %q", err, leaked)
		}
	}

	if userExists(t, pool, second.ID()) {
		t.Fatalf("user %s persisted after credentials conflict", second.ID())
	}
	if got := countCredentialsByEmail(t, pool, email); got != 1 {
		t.Fatalf("credentials with email %q = %d, want 1", email, got)
	}
}

func TestRegister_RejectsDisplayName(t *testing.T) {
	pool := openTestPool(t)
	service, err := identity.NewService(identity_postgres.NewStore(pool), testTokenConfig())
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), postgresOperationTimeout)
	defer cancel()

	mailbox := uniqueEmail("display-name")
	_, err = service.Register(ctx, identity.RegisterInput{
		Email:    "Alice <" + strings.ToUpper(mailbox) + ">",
		Password: integrationPassword,
	})
	if !errors.Is(err, identity.ErrInvalidEmail) {
		t.Fatalf("Register(display name) error = %v, want ErrInvalidEmail", err)
	}
	if got := countCredentialsByEmail(t, pool, mailbox); got != 0 {
		t.Fatalf("credentials with email %q = %d, want 0", mailbox, got)
	}
}

func TestStore_CreateUser_ConcurrentDuplicateEmailHasOneWinner(t *testing.T) {
	pool := openTestPool(t)
	store := identity_postgres.NewStore(pool)

	email := uniqueEmail("concurrent")
	users := []identity.User{
		newTestUser(t, email),
		newTestUser(t, email),
	}
	passwordHash := newTestPasswordHash(t)
	cleanupUsers(t, pool, users[0].ID(), users[1].ID())
	ctx, cancel := context.WithTimeout(context.Background(), postgresOperationTimeout)
	defer cancel()

	results := make(chan error, len(users))
	var waitGroup sync.WaitGroup
	for _, user := range users {
		waitGroup.Add(1)
		go func(candidate identity.User) {
			defer waitGroup.Done()
			results <- store.CreateUser(ctx, candidate, passwordHash)
		}(user)
	}
	waitGroup.Wait()
	close(results)

	successes := 0
	conflicts := 0
	for err := range results {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, identity.ErrEmailAlreadyExists):
			conflicts++
		default:
			t.Fatalf("CreateUser() unexpected error = %v", err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("results = %d successes, %d conflicts; want 1 and 1", successes, conflicts)
	}
	if got := countExistingUsers(t, pool, users); got != 1 {
		t.Fatalf("persisted candidate users = %d, want 1", got)
	}
	if got := countCredentialsByEmail(t, pool, email); got != 1 {
		t.Fatalf("credentials with email %q = %d, want 1", email, got)
	}
}

func TestStore_CreateUser_CanceledContextDoesNotPersist(t *testing.T) {
	pool := openTestPool(t)
	store := identity_postgres.NewStore(pool)
	user := newTestUser(t, uniqueEmail("cancelled"))
	passwordHash := newTestPasswordHash(t)
	cleanupUsers(t, pool, user.ID())

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := store.CreateUser(ctx, user, passwordHash)
	if err == nil {
		t.Fatal("CreateUser() error = nil with canceled context")
	}
	if userExists(t, pool, user.ID()) {
		t.Fatalf("user %s persisted with canceled context", user.ID())
	}
}

func TestStore_CreateUser_RejectsEmptyPasswordHashWithoutPersisting(t *testing.T) {
	pool := openTestPool(t)
	store := identity_postgres.NewStore(pool)
	user := newTestUser(t, uniqueEmail("empty-password-hash"))
	cleanupUsers(t, pool, user.ID())

	ctx, cancel := context.WithTimeout(context.Background(), postgresOperationTimeout)
	defer cancel()

	err := store.CreateUser(ctx, user, identity.PasswordHash{})
	if err == nil {
		t.Fatal("CreateUser() error = nil with empty PasswordHash")
	}
	if userExists(t, pool, user.ID()) {
		t.Fatalf("user %s persisted with empty PasswordHash", user.ID())
	}
}

func newTestUser(t *testing.T, email string) identity.User {
	t.Helper()

	repository := &capturedUserRepository{}
	service, err := identity.NewService(repository, testTokenConfig())
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	user, err := service.Register(context.Background(), identity.RegisterInput{
		Email:    email,
		Password: integrationPassword,
	})
	if err != nil {
		t.Fatalf("create test User: %v", err)
	}

	return user
}

func newTestPasswordHash(t *testing.T) identity.PasswordHash {
	t.Helper()

	repository := &capturedUserRepository{}
	service, err := identity.NewService(repository, testTokenConfig())
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	_, err = service.Register(context.Background(), identity.RegisterInput{
		Email:    uniqueEmail("password-hash"),
		Password: integrationPassword,
	})
	if err != nil {
		t.Fatalf("create test PasswordHash: %v", err)
	}

	return repository.passwordHash
}

func uniqueEmail(prefix string) string {
	return fmt.Sprintf("%s-%s@example.com", prefix, uuid.NewString())
}

func openTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	dsn := os.Getenv("FLASHDROP_TEST_DSN")
	if dsn == "" {
		t.Skip("FLASHDROP_TEST_DSN is not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), postgresOperationTimeout)
	defer cancel()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pgxpool.New() error = %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Fatalf("PostgreSQL ping error = %v", err)
	}

	t.Cleanup(pool.Close)
	return pool
}

func cleanupUsers(t *testing.T, pool *pgxpool.Pool, userIDs ...uuid.UUID) {
	t.Helper()

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), postgresOperationTimeout)
		defer cancel()

		for _, userID := range userIDs {
			if _, err := pool.Exec(
				ctx,
				`DELETE FROM flashdrop.user_credentials WHERE user_id = $1`,
				userID,
			); err != nil {
				t.Errorf("delete credentials for %s: %v", userID, err)
			}
			if _, err := pool.Exec(
				ctx,
				`DELETE FROM flashdrop.users WHERE id = $1`,
				userID,
			); err != nil {
				t.Errorf("delete user %s: %v", userID, err)
			}
		}
	})
}

func userExists(t *testing.T, pool *pgxpool.Pool, userID uuid.UUID) bool {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), postgresOperationTimeout)
	defer cancel()

	var exists bool
	if err := pool.QueryRow(
		ctx,
		`SELECT EXISTS(SELECT 1 FROM flashdrop.users WHERE id = $1)`,
		userID,
	).Scan(&exists); err != nil {
		t.Fatalf("check user %s: %v", userID, err)
	}

	return exists
}

func countCredentialsByEmail(t *testing.T, pool *pgxpool.Pool, email string) int {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), postgresOperationTimeout)
	defer cancel()

	var count int
	if err := pool.QueryRow(
		ctx,
		`SELECT count(*) FROM flashdrop.user_credentials WHERE email = $1`,
		email,
	).Scan(&count); err != nil {
		t.Fatalf("count credentials for %q: %v", email, err)
	}

	return count
}

func countExistingUsers(
	t *testing.T,
	pool *pgxpool.Pool,
	users []identity.User,
) int {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), postgresOperationTimeout)
	defer cancel()

	count := 0
	for _, user := range users {
		var ignored uuid.UUID
		err := pool.QueryRow(
			ctx,
			`SELECT id FROM flashdrop.users WHERE id = $1`,
			user.ID(),
		).Scan(&ignored)
		switch {
		case err == nil:
			count++
		case errors.Is(err, pgx.ErrNoRows):
		default:
			t.Fatalf("read candidate user %s: %v", user.ID(), err)
		}
	}

	return count
}
