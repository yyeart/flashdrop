package identity

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"

	"golang.org/x/crypto/argon2"
)

const (
	argonMemory      uint32 = 19 * 1024 // KiB
	argonIterations  uint32 = 2
	argonParallelism uint8  = 1
	saltLength              = 16
	keyLength        uint32 = 32
)

var (
	ErrInvalidPasswordLength = errors.New("password must be between 8 and 128 bytes")
	errMalformedPasswordHash = errors.New("malformed password hash")
	errUnsupportedHash       = errors.New("unsupported password hash")
)

func hashPassword(password string) (string, error) {
	if err := validatePassword(password); err != nil {
		return "", err
	}

	salt := make([]byte, saltLength)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return "", fmt.Errorf("generate password salt: %w", err)
	}

	hash := argon2.IDKey(
		[]byte(password),
		salt,
		argonIterations,
		argonMemory,
		argonParallelism,
		keyLength,
	)

	encoding := base64.RawStdEncoding

	return fmt.Sprintf(
		"$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version,
		argonMemory,
		argonIterations,
		argonParallelism,
		encoding.EncodeToString(salt),
		encoding.EncodeToString(hash),
	), nil
}

func verifyPassword(password, encoded string) (bool, error) {
	salt, expected, err := parsePasswordHash(encoded)
	if err != nil {
		return false, err
	}

	actual := argon2.IDKey(
		[]byte(password),
		salt,
		argonIterations,
		argonMemory,
		argonParallelism,
		keyLength,
	)

	return subtle.ConstantTimeCompare(actual, expected) == 1, nil
}

func validatePassword(password string) error {
	length := len([]byte(password))
	if length < 8 || length > 128 {
		return ErrInvalidPasswordLength
	}

	return nil
}

func parsePasswordHash(encoded string) ([]byte, []byte, error) {
	if len(encoded) > 256 {
		return nil, nil, errMalformedPasswordHash
	}

	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" {
		return nil, nil, errMalformedPasswordHash
	}
	if parts[1] != "argon2id" {
		return nil, nil, errUnsupportedHash
	}

	if err := validateVersion(parts); err != nil {
		return nil, nil, err
	}

	if err := validateParameters(parts); err != nil {
		return nil, nil, err
	}

	encoding := base64.RawStdEncoding

	salt, err := encoding.DecodeString(parts[4])
	if err != nil || len(salt) != saltLength {
		return nil, nil, errMalformedPasswordHash
	}

	hash, err := encoding.DecodeString(parts[5])
	if err != nil || len(hash) != int(keyLength) {
		return nil, nil, errMalformedPasswordHash
	}

	return salt, hash, nil
}

func validateVersion(parts []string) error {
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil ||
		parts[2] != fmt.Sprintf("v=%d", version) {
		return errMalformedPasswordHash
	}

	if version != argon2.Version {
		return errUnsupportedHash
	}

	return nil
}

func validateParameters(parts []string) error {
	var memory, iterations uint32
	var parallelism uint8

	if _, err := fmt.Sscanf(
		parts[3],
		"m=%d,t=%d,p=%d",
		&memory, &iterations, &parallelism,
	); err != nil {
		return errMalformedPasswordHash
	}

	canonical := fmt.Sprintf(
		"m=%d,t=%d,p=%d",
		memory,
		iterations,
		parallelism,
	)

	if parts[3] != canonical {
		return errMalformedPasswordHash
	}

	if memory != argonMemory ||
		iterations != argonIterations ||
		parallelism != argonParallelism {
		return errUnsupportedHash
	}

	return nil
}
