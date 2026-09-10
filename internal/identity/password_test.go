package identity

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"testing"

	"golang.org/x/crypto/argon2"
)

const testPassword = "correct horse battery staple"

func TestHashPasswordVerify(t *testing.T) {
	passwordHash, err := hashPassword(testPassword)
	if err != nil {
		t.Fatalf("hashPassword() error = %v", err)
	}
	encoded := mustEncodePasswordHash(t, passwordHash)

	matched, err := verifyPassword(testPassword, encoded)
	if err != nil {
		t.Fatalf("verifyPassword(correct) error = %v", err)
	}
	if !matched {
		t.Fatal("verifyPassword(correct) = false, want true")
	}

	matched, err = verifyPassword("wrong password", encoded)
	if err != nil {
		t.Fatalf("verifyPassword(wrong) error = %v", err)
	}
	if matched {
		t.Fatal("verifyPassword(wrong) = true, want false")
	}
}

func TestHashPasswordUsesRandomSalt(t *testing.T) {
	firstHash, err := hashPassword(testPassword)
	if err != nil {
		t.Fatalf("hashPassword(first) error = %v", err)
	}
	secondHash, err := hashPassword(testPassword)
	if err != nil {
		t.Fatalf("HashPassword(second) error = %v", err)
	}
	first := mustEncodePasswordHash(t, firstHash)
	second := mustEncodePasswordHash(t, secondHash)

	if first == second {
		t.Fatal("two password hashes are equal, want different salts")
	}

	firstSalt, _, err := parsePasswordHash(first)
	if err != nil {
		t.Fatalf("parsePasswordHash(first) error = %v", err)
	}
	secondSalt, _, err := parsePasswordHash(second)
	if err != nil {
		t.Fatalf("parsePasswordHash(second) error = %v", err)
	}
	if bytes.Equal(firstSalt, secondSalt) {
		t.Fatal("two generated salts are equal")
	}
}

func TestHashPasswordEncodesExpectedPHCParametersAndLengths(t *testing.T) {
	passwordHash, err := hashPassword(testPassword)
	if err != nil {
		t.Fatalf("hashPassword() error = %v", err)
	}
	encoded := mustEncodePasswordHash(t, passwordHash)

	parts := strings.Split(encoded, "$")
	if len(parts) != 6 {
		t.Fatalf("PHC section count = %d, want 6", len(parts))
	}
	if parts[1] != "argon2id" {
		t.Fatalf("algorithm = %q, want argon2id", parts[1])
	}
	if parts[2] != fmt.Sprintf("v=%d", argon2.Version) {
		t.Fatalf("version = %q, want v=%d", parts[2], argon2.Version)
	}
	if parts[3] != "m=19456,t=2,p=1" {
		t.Fatalf("parameters = %q, want m=19456,t=2,p=1", parts[3])
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		t.Fatalf("decode salt: %v", err)
	}
	if len(salt) != 16 {
		t.Fatalf("salt length = %d, want 16", len(salt))
	}

	hash, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		t.Fatalf("decode hash: %v", err)
	}
	if len(hash) != 32 {
		t.Fatalf("hash length = %d, want 32", len(hash))
	}
}

func TestVerifyPasswordRejectsUnsupportedPHC(t *testing.T) {
	passwordHash, err := hashPassword(testPassword)
	if err != nil {
		t.Fatalf("hashPassword() error = %v", err)
	}
	encoded := mustEncodePasswordHash(t, passwordHash)

	tests := map[string]string{
		"algorithm": strings.Replace(encoded, "$argon2id$", "$argon2i$", 1),
		"version": strings.Replace(
			encoded,
			fmt.Sprintf("$v=%d$", argon2.Version),
			"$v=16$",
			1,
		),
		"memory":     strings.Replace(encoded, "m=19456", "m=19455", 1),
		"iterations": strings.Replace(encoded, "t=2", "t=3", 1),
		"parallelism": strings.Replace(
			encoded,
			"p=1",
			"p=2",
			1,
		),
	}

	for name, candidate := range tests {
		t.Run(name, func(t *testing.T) {
			matched, err := verifyPassword(testPassword, candidate)
			if !errors.Is(err, errUnsupportedHash) {
				t.Fatalf("verifyPassword() error = %v, want ErrUnsupportedHash", err)
			}
			if matched {
				t.Fatal("verifyPassword() = true for unsupported PHC")
			}
		})
	}
}

func TestPasswordHashEncodedRejectsZeroValue(t *testing.T) {
	var passwordHash PasswordHash
	_, err := passwordHash.Encoded()
	if !errors.Is(err, errUnsupportedHash) {
		t.Fatalf("PasswordHash{}.Encoded() error = %v, want errUnsupportedHash", err)
	}
}

func TestVerifyPasswordRejectsMalformedPHCWithoutPanic(t *testing.T) {
	valid := testPHCString(16, 32)
	validParts := strings.Split(valid, "$")

	tests := map[string]string{
		"empty":                  "",
		"too long":               strings.Repeat("x", 257),
		"missing leading dollar": strings.TrimPrefix(valid, "$"),
		"missing section":        strings.Join(validParts[:5], "$"),
		"invalid version":        strings.Replace(valid, "v=19", "v=nope", 1),
		"version suffix":         strings.Replace(valid, "v=19", "v=19x", 1),
		"invalid parameters":     strings.Replace(valid, "m=19456", "m=nope", 1),
		"extra parameter": strings.Replace(
			valid,
			"m=19456,t=2,p=1",
			"m=19456,t=2,p=1,x=1",
			1,
		),
		"invalid salt base64": strings.Replace(
			valid,
			base64.RawStdEncoding.EncodeToString(make([]byte, 16)),
			"%%%",
			1,
		),
		"short salt": testPHCString(15, 32),
		"short hash": testPHCString(16, 31),
	}

	for name, candidate := range tests {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recovered := recover(); recovered != nil {
					t.Fatalf("VerifyPassword() panicked: %v", recovered)
				}
			}()

			matched, err := verifyPassword(testPassword, candidate)
			if !errors.Is(err, errMalformedPasswordHash) {
				t.Fatalf(
					"verifyPassword() error = %v, want ErrMalformedPasswordHash",
					err,
				)
			}
			if matched {
				t.Fatal("verifyPassword() = true for malformed PHC")
			}
		})
	}
}

func TestHashPasswordValidatesUTF8ByteLength(t *testing.T) {
	tests := []struct {
		name     string
		password string
		wantErr  bool
	}{
		{name: "seven ASCII bytes", password: strings.Repeat("a", 7), wantErr: true},
		{name: "eight ASCII bytes", password: strings.Repeat("a", 8)},
		{name: "eight UTF-8 bytes", password: strings.Repeat("я", 4)},
		{name: "128 UTF-8 bytes", password: strings.Repeat("я", 64)},
		{name: "130 UTF-8 bytes", password: strings.Repeat("я", 65), wantErr: true},
		{name: "129 ASCII bytes", password: strings.Repeat("a", 129), wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := hashPassword(test.password)
			if test.wantErr {
				if !errors.Is(err, ErrInvalidPasswordLength) {
					t.Fatalf(
						"hashPassword() error = %v, want ErrInvalidPasswordLength",
						err,
					)
				}
				return
			}

			if err != nil {
				t.Fatalf("hashPassword() error = %v", err)
			}
		})
	}
}

func testPHCString(saltLen, hashLen int) string {
	encoding := base64.RawStdEncoding

	return fmt.Sprintf(
		"$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version,
		argonMemory,
		argonIterations,
		argonParallelism,
		encoding.EncodeToString(make([]byte, saltLen)),
		encoding.EncodeToString(make([]byte, hashLen)),
	)
}

func mustEncodePasswordHash(t *testing.T, passwordHash PasswordHash) string {
	t.Helper()

	encoded, err := passwordHash.Encoded()
	if err != nil {
		t.Fatalf("PasswordHash.Encoded() error = %v", err)
	}

	return encoded
}
