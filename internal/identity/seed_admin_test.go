package identity

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

const seedAdminTestPassword = "correct horse battery staple"

type seedAdminTestContextKey struct{}

type seedAdminFindResult struct {
	credentials LoginCredentials
	found       bool
	err         error
}

type seedAdminTestRepository struct {
	findResults []seedAdminFindResult
	createErr   error

	findCalls      int
	createCalls    int
	emails         []string
	createdUser    User
	createdHash    PasswordHash
	findContexts   []context.Context
	createContexts []context.Context
}

func (r *seedAdminTestRepository) FindLoginCreds(
	ctx context.Context,
	email string,
) (LoginCredentials, bool, error) {
	r.findContexts = append(r.findContexts, ctx)
	r.emails = append(r.emails, email)

	index := r.findCalls
	r.findCalls++
	if index >= len(r.findResults) {
		return LoginCredentials{}, false, nil
	}

	result := r.findResults[index]
	return result.credentials, result.found, result.err
}

func (r *seedAdminTestRepository) CreateUser(
	ctx context.Context,
	user User,
	passwordHash PasswordHash,
) error {
	r.createContexts = append(r.createContexts, ctx)
	r.createCalls++
	r.createdUser = user
	r.createdHash = passwordHash
	return r.createErr
}

func mustSeedAdminPasswordHash(t *testing.T, password string) PasswordHash {
	t.Helper()

	hash, err := hashPassword(password)
	if err != nil {
		t.Fatalf("hashPassword() error = %v", err)
	}

	return hash
}

func TestAdminSeeder_CreatesNormalizedAdministrator(t *testing.T) {
	repository := &seedAdminTestRepository{}
	seeder := NewAdminSeeder(repository)
	wantID := uuid.MustParse("00000000-0000-0000-0000-000000000123")
	wantTime := time.Date(2026, time.September, 12, 12, 0, 0, 123, time.FixedZone("test", 3*60*60))
	seeder.newID = func() uuid.UUID { return wantID }
	seeder.now = func() time.Time { return wantTime }

	ctx := context.WithValue(context.Background(), seedAdminTestContextKey{}, "seed-request")
	err := seeder.SeedAdmin(ctx, SeedAdminInput{
		Email:    "  Admin@Example.COM  ",
		Password: seedAdminTestPassword,
	})
	if err != nil {
		t.Fatalf("SeedAdmin() error = %v", err)
	}

	if repository.findCalls != 1 || repository.createCalls != 1 {
		t.Fatalf("repository calls = find %d, create %d; want 1 and 1", repository.findCalls, repository.createCalls)
	}
	assertSeedAdminRepositoryContexts(t, repository, ctx)
	if len(repository.emails) != 1 || repository.emails[0] != "admin@example.com" {
		t.Fatalf("FindLoginCreds() emails = %q, want [admin@example.com]", repository.emails)
	}
	if repository.createdUser.ID() != wantID {
		t.Fatalf("created user ID = %s, want %s", repository.createdUser.ID(), wantID)
	}
	if repository.createdUser.Email() != "admin@example.com" {
		t.Fatalf("created user email = %q, want admin@example.com", repository.createdUser.Email())
	}
	if repository.createdUser.Role() != RoleAdmin {
		t.Fatalf("created user role = %q, want %q", repository.createdUser.Role(), RoleAdmin)
	}
	if !repository.createdUser.CreatedAt().Equal(wantTime.UTC()) {
		t.Fatalf("created user time = %s, want %s", repository.createdUser.CreatedAt(), wantTime.UTC())
	}

	encoded, err := repository.createdHash.Encoded()
	if err != nil {
		t.Fatalf("created password hash is invalid: %v", err)
	}
	if encoded == seedAdminTestPassword {
		t.Fatal("CreateUser() received the plaintext password instead of a hash")
	}
	matched, err := verifyPassword(seedAdminTestPassword, encoded)
	if err != nil || !matched {
		t.Fatalf("created password hash does not verify: matched=%v error=%v", matched, err)
	}
}

func TestAdminSeeder_ExistingAdministratorIsIdempotent(t *testing.T) {
	repository := &seedAdminTestRepository{findResults: []seedAdminFindResult{{
		found: true,
		credentials: LoginCredentials{
			UserID:       uuid.MustParse("00000000-0000-0000-0000-000000000123"),
			Role:         RoleAdmin,
			PasswordHash: mustSeedAdminPasswordHash(t, seedAdminTestPassword),
		},
	}}}

	err := NewAdminSeeder(repository).SeedAdmin(context.Background(), SeedAdminInput{
		Email:    "admin@example.com",
		Password: seedAdminTestPassword,
	})
	if err != nil {
		t.Fatalf("SeedAdmin() error = %v", err)
	}
	if repository.createCalls != 0 {
		t.Fatalf("CreateUser() calls = %d, want 0", repository.createCalls)
	}
}

func TestAdminSeeder_ExistingAccountConflicts(t *testing.T) {
	validHash := mustSeedAdminPasswordHash(t, seedAdminTestPassword)
	tests := []struct {
		name         string
		role         Role
		passwordHash PasswordHash
		password     string
	}{
		{name: "ordinary user", role: RoleUser, passwordHash: validHash, password: seedAdminTestPassword},
		{name: "administrator with another password", role: RoleAdmin, passwordHash: validHash, password: "another valid password"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repository := &seedAdminTestRepository{findResults: []seedAdminFindResult{{
				found: true,
				credentials: LoginCredentials{
					UserID:       uuid.MustParse("00000000-0000-0000-0000-000000000123"),
					Role:         tt.role,
					PasswordHash: tt.passwordHash,
				},
			}}}

			err := NewAdminSeeder(repository).SeedAdmin(context.Background(), SeedAdminInput{
				Email:    "admin@example.com",
				Password: tt.password,
			})
			if !errors.Is(err, ErrSeedAdminConflict) {
				t.Fatalf("SeedAdmin() error = %v, want ErrSeedAdminConflict", err)
			}
			if repository.createCalls != 0 {
				t.Fatalf("CreateUser() calls = %d, want 0", repository.createCalls)
			}
		})
	}
}

func TestAdminSeeder_ResolvesConcurrentDuplicate(t *testing.T) {
	repository := &seedAdminTestRepository{
		findResults: []seedAdminFindResult{
			{},
			{
				found: true,
				credentials: LoginCredentials{
					UserID:       uuid.MustParse("00000000-0000-0000-0000-000000000123"),
					Role:         RoleAdmin,
					PasswordHash: mustSeedAdminPasswordHash(t, seedAdminTestPassword),
				},
			},
		},
		createErr: ErrEmailAlreadyExists,
	}

	ctx := context.WithValue(context.Background(), seedAdminTestContextKey{}, "seed-race-request")
	err := NewAdminSeeder(repository).SeedAdmin(ctx, SeedAdminInput{
		Email:    "admin@example.com",
		Password: seedAdminTestPassword,
	})
	if err != nil {
		t.Fatalf("SeedAdmin() error = %v", err)
	}
	if repository.findCalls != 2 || repository.createCalls != 1 {
		t.Fatalf("repository calls = find %d, create %d; want 2 and 1", repository.findCalls, repository.createCalls)
	}
	assertSeedAdminRepositoryContexts(t, repository, ctx)
}

func assertSeedAdminRepositoryContexts(t *testing.T, repository *seedAdminTestRepository, want context.Context) {
	t.Helper()
	for i, got := range repository.findContexts {
		if got != want {
			t.Errorf("FindLoginCreds() call %d did not receive the caller context", i+1)
		}
	}
	for i, got := range repository.createContexts {
		if got != want {
			t.Errorf("CreateUser() call %d did not receive the caller context", i+1)
		}
	}
}

func TestAdminSeeder_PreservesDependencyAndContextErrors(t *testing.T) {
	repositoryFailure := errors.New("repository failure")
	hashFailure := errors.New("hash failure")
	tests := []struct {
		name       string
		repository *seedAdminTestRepository
		configure  func(*AdminSeeder)
		ctx        context.Context
		want       error
	}{
		{
			name: "find failure",
			repository: &seedAdminTestRepository{findResults: []seedAdminFindResult{{
				err: repositoryFailure,
			}}},
			want: repositoryFailure,
		},
		{
			name:       "hash failure",
			repository: &seedAdminTestRepository{},
			configure: func(seeder *AdminSeeder) {
				seeder.hashPassword = func(string) (PasswordHash, error) {
					return PasswordHash{}, hashFailure
				}
			},
			want: hashFailure,
		},
		{
			name:       "create failure",
			repository: &seedAdminTestRepository{createErr: repositoryFailure},
			want:       repositoryFailure,
		},
		{
			name: "canceled context",
			repository: &seedAdminTestRepository{findResults: []seedAdminFindResult{{
				err: context.Canceled,
			}}},
			want: context.Canceled,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			seeder := NewAdminSeeder(tt.repository)
			if tt.configure != nil {
				tt.configure(seeder)
			}
			ctx := tt.ctx
			if ctx == nil {
				ctx = context.Background()
			}

			err := seeder.SeedAdmin(ctx, SeedAdminInput{
				Email:    "admin@example.com",
				Password: seedAdminTestPassword,
			})
			if !errors.Is(err, tt.want) {
				t.Fatalf("SeedAdmin() error = %v, want wrapped %v", err, tt.want)
			}
		})
	}
}

func TestAdminSeeder_RejectsInvalidInputBeforeRepository(t *testing.T) {
	tests := []struct {
		name  string
		input SeedAdminInput
		want  error
	}{
		{name: "invalid email", input: SeedAdminInput{Email: "invalid", Password: seedAdminTestPassword}, want: ErrInvalidEmail},
		{name: "invalid password", input: SeedAdminInput{Email: "admin@example.com", Password: "short"}, want: ErrInvalidPasswordLength},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repository := &seedAdminTestRepository{}
			err := NewAdminSeeder(repository).SeedAdmin(context.Background(), tt.input)
			if !errors.Is(err, tt.want) {
				t.Fatalf("SeedAdmin() error = %v, want %v", err, tt.want)
			}
			if repository.findCalls != 0 || repository.createCalls != 0 {
				t.Fatalf("repository calls = find %d, create %d; want none", repository.findCalls, repository.createCalls)
			}
		})
	}
}
