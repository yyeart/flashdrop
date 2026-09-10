package identity

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

const registerTestPassword = "correct horse battery staple"

type recordingUserRepository struct {
	calls        int
	contextMark  string
	user         User
	passwordHash PasswordHash
	err          error
}

type registerContextKey struct{}

func (r *recordingUserRepository) CreateUser(
	ctx context.Context,
	user User,
	passwordHash PasswordHash,
) error {
	r.calls++
	if mark, ok := ctx.Value(registerContextKey{}).(string); ok {
		r.contextMark = mark
	}
	r.user = user
	r.passwordHash = passwordHash

	return r.err
}

func TestService_Register_NormalizesEmailAndUsesServerValues(t *testing.T) {
	repository := &recordingUserRepository{}
	service := NewService(repository)

	wantID := uuid.MustParse("00000000-0000-0000-0000-000000000123")
	wantTime := time.Date(2026, time.September, 9, 18, 30, 0, 0, time.FixedZone("test", 3*60*60))
	wantHash := PasswordHash{encoded: "$argon2id$test-hash"}
	service.newID = func() uuid.UUID { return wantID }
	service.now = func() time.Time { return wantTime }
	service.hashPassword = func(password string) (PasswordHash, error) {
		if password != registerTestPassword {
			t.Fatalf("hashPassword() password = %q, want original password", password)
		}

		return wantHash, nil
	}

	const contextMark = "registration-request"
	ctx := context.WithValue(context.Background(), registerContextKey{}, contextMark)
	got, err := service.Register(ctx, RegisterInput{
		Email:    "  USER@Example.COM  ",
		Password: registerTestPassword,
	})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	if repository.calls != 1 {
		t.Fatalf("CreateUser() calls = %d, want 1", repository.calls)
	}
	if repository.contextMark != contextMark {
		t.Fatal("CreateUser() did not receive the registration context")
	}
	if repository.passwordHash != wantHash {
		t.Fatalf("CreateUser() password hash = %q, want %q", repository.passwordHash, wantHash)
	}

	wantEmail := "user@example.com"
	wantCreatedAt := wantTime.UTC()
	assertRegisteredUser(t, got, wantID, wantEmail, RoleUser, wantCreatedAt)
	assertRegisteredUser(t, repository.user, wantID, wantEmail, RoleUser, wantCreatedAt)
}

func TestService_Register_InvalidEmailDoesNotCallDependencies(t *testing.T) {
	tests := map[string]string{
		"empty":        "",
		"spaces only":  "   ",
		"malformed":    "not-an-email",
		"display name": "Alice <USER@Example.COM>",
	}

	for name, email := range tests {
		t.Run(name, func(t *testing.T) {
			repository := &recordingUserRepository{}
			service := NewService(repository)
			hashCalls := 0
			service.hashPassword = func(string) (PasswordHash, error) {
				hashCalls++
				return PasswordHash{encoded: "hash"}, nil
			}

			_, err := service.Register(context.Background(), RegisterInput{
				Email:    email,
				Password: registerTestPassword,
			})
			if !errors.Is(err, ErrInvalidEmail) {
				t.Fatalf("Register() error = %v, want ErrInvalidEmail", err)
			}
			if hashCalls != 0 {
				t.Fatalf("hashPassword() calls = %d, want 0", hashCalls)
			}
			if repository.calls != 0 {
				t.Fatalf("CreateUser() calls = %d, want 0", repository.calls)
			}
		})
	}
}

func TestService_Register_RejectsPasswordOutsideByteLimits(t *testing.T) {
	tests := map[string]string{
		"seven ASCII bytes": strings.Repeat("a", 7),
		"129 ASCII bytes":   strings.Repeat("a", 129),
		"130 UTF-8 bytes":   strings.Repeat("я", 65),
	}

	for name, password := range tests {
		t.Run(name, func(t *testing.T) {
			repository := &recordingUserRepository{}
			service := NewService(repository)

			_, err := service.Register(context.Background(), RegisterInput{
				Email:    "user@example.com",
				Password: password,
			})
			if !errors.Is(err, ErrInvalidPasswordLength) {
				t.Fatalf("Register() error = %v, want ErrInvalidPasswordLength", err)
			}
			if repository.calls != 0 {
				t.Fatalf("CreateUser() calls = %d, want 0", repository.calls)
			}
		})
	}
}

func TestService_Register_PreservesRepositoryErrorIdentity(t *testing.T) {
	wantErr := errors.New("repository failure")
	repository := &recordingUserRepository{err: wantErr}
	service := NewService(repository)
	service.hashPassword = func(string) (PasswordHash, error) {
		return PasswordHash{encoded: "hash"}, nil
	}

	got, err := service.Register(context.Background(), RegisterInput{
		Email:    "user@example.com",
		Password: registerTestPassword,
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("Register() error = %v, want errors.Is(..., repository error)", err)
	}
	if got.ID() != uuid.Nil || got.Email() != "" || got.Role() != "" || !got.CreatedAt().IsZero() {
		t.Fatalf("Register() user = %#v, want zero User on repository error", got)
	}
}

func TestService_Register_PreservesHashErrorIdentity(t *testing.T) {
	wantErr := errors.New("hash failure")
	repository := &recordingUserRepository{}
	service := NewService(repository)
	service.hashPassword = func(string) (PasswordHash, error) { return PasswordHash{}, wantErr }

	_, err := service.Register(context.Background(), RegisterInput{
		Email:    "user@example.com",
		Password: registerTestPassword,
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("Register() error = %v, want errors.Is(..., hash error)", err)
	}
	if repository.calls != 0 {
		t.Fatalf("CreateUser() calls = %d, want 0", repository.calls)
	}
}

func assertRegisteredUser(
	t *testing.T,
	got User,
	wantID uuid.UUID,
	wantEmail string,
	wantRole Role,
	wantCreatedAt time.Time,
) {
	t.Helper()

	if got.ID() != wantID {
		t.Errorf("User.ID() = %s, want %s", got.ID(), wantID)
	}
	if got.Email() != wantEmail {
		t.Errorf("User.Email() = %q, want %q", got.Email(), wantEmail)
	}
	if got.Role() != wantRole {
		t.Errorf("User.Role() = %q, want %q", got.Role(), wantRole)
	}
	if !got.CreatedAt().Equal(wantCreatedAt) {
		t.Errorf("User.CreatedAt() = %s, want %s", got.CreatedAt(), wantCreatedAt)
	}
}
