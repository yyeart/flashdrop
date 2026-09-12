package identity

import (
	"bytes"
	"crypto/ed25519"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

var (
	ErrInvalidTokenConfig = errors.New("invalid token config")
	ErrInvalidToken       = errors.New("invalid token")
	ErrInvalidPrivateKey  = errors.New("invalid private key")
	ErrInvalidPublicKey   = errors.New("invalid public key")
)

const (
	DefaultTokenIssuer   = "flashdrop"
	DefaultTokenAudience = "flashdrop-api"
	DefaultTokenTTL      = 15 * time.Minute
)

type TokenConfig struct {
	Issuer     string
	Audience   string
	TTL        time.Duration
	PrivateKey ed25519.PrivateKey
	PublicKey  ed25519.PublicKey
}

type accessClaims struct {
	Role Role `json:"role"`
	jwt.RegisteredClaims
}

func LoadTokenConfig(
	issuer, audience string,
	ttl time.Duration,
	publicTokenFilename string,
	privateTokenFilename string,
) (TokenConfig, error) {
	publicKey, err := parsePublicKey(publicTokenFilename)
	if err != nil {
		return TokenConfig{}, fmt.Errorf("load token config: %w", err)
	}

	privateKey, err := parsePrivateKey(privateTokenFilename)
	if err != nil {
		return TokenConfig{}, fmt.Errorf("load token config: %w", err)
	}

	if issuer == "" {
		issuer = DefaultTokenIssuer
	}

	if audience == "" {
		audience = DefaultTokenAudience
	}

	if ttl == 0 {
		ttl = DefaultTokenTTL
	}

	return TokenConfig{
		Issuer:     issuer,
		Audience:   audience,
		TTL:        ttl,
		PrivateKey: privateKey,
		PublicKey:  publicKey,
	}, nil
}

func issueAccessToken(
	userID uuid.UUID,
	role Role,
	now time.Time,
	cfg TokenConfig,
) (string, error) {
	if userID == uuid.Nil {
		return "", fmt.Errorf("invalid userID parameter: %w", ErrInvalidToken)
	}

	if !role.IsUserOrAdmin() {
		return "", fmt.Errorf("invalid role parameter: %w", ErrInvalidToken)
	}

	if now.IsZero() {
		return "", fmt.Errorf("invalid now parameter: %w", ErrInvalidToken)
	}

	claims := accessClaims{
		Role: role,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID.String(),
			Issuer:    cfg.Issuer,
			Audience:  jwt.ClaimStrings{cfg.Audience},
			IssuedAt:  &jwt.NumericDate{Time: now},
			ExpiresAt: &jwt.NumericDate{Time: now.Add(cfg.TTL)},
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims)

	accessToken, err := token.SignedString(cfg.PrivateKey)
	if err != nil {
		return "", fmt.Errorf("creating access token: %w", err)
	}

	return accessToken, nil
}

func validateTokenConfig(cfg TokenConfig) error {
	trimIssuer := strings.TrimSpace(cfg.Issuer)
	trimAudience := strings.TrimSpace(cfg.Audience)

	if trimIssuer == "" {
		return fmt.Errorf("issuer must not be empty: %w", ErrInvalidTokenConfig)
	}

	if trimAudience == "" {
		return fmt.Errorf("audience must not be empty: %w", ErrInvalidTokenConfig)
	}

	if cfg.TTL <= 0 {
		return fmt.Errorf("time to live must be positive: %w", ErrInvalidTokenConfig)
	}

	if cfg.TTL%time.Second != 0 {
		return fmt.Errorf(
			"time to live must be a multiple of time.Second: %w", ErrInvalidTokenConfig,
		)
	}

	if len(cfg.PrivateKey) != ed25519.PrivateKeySize {
		return fmt.Errorf("invalid private key len: %w", ErrInvalidTokenConfig)
	}

	if len(cfg.PublicKey) != ed25519.PublicKeySize {
		return fmt.Errorf("invalid public key len: %w", ErrInvalidTokenConfig)
	}

	publicFromPrivateInterface := cfg.PrivateKey.Public()

	publicFromPrivate, ok := publicFromPrivateInterface.(ed25519.PublicKey)
	if !ok {
		return fmt.Errorf("failed to type-assert public key: %w", ErrInvalidTokenConfig)
	}

	if !bytes.Equal(cfg.PublicKey, publicFromPrivate) {
		return fmt.Errorf("invalid public/private keys pair: %w", ErrInvalidTokenConfig)
	}

	seedPrivateKey := ed25519.NewKeyFromSeed(cfg.PrivateKey.Seed())
	if !bytes.Equal(cfg.PrivateKey, seedPrivateKey) {
		return fmt.Errorf(
			"key from seed does not match with private key: %w",
			ErrInvalidTokenConfig,
		)
	}

	return nil
}

func parsePrivateKey(filename string) (ed25519.PrivateKey, error) {
	//nolint:gosec // G304: filename comes from trusted process configuration, not user input.
	content, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf(
			"read private key: %w: %w",
			err,
			ErrInvalidPrivateKey,
		)
	}

	block, rest := pem.Decode(content)
	if block == nil {
		return nil, fmt.Errorf(
			"failed to parse private key PEM block: %w",
			ErrInvalidPrivateKey,
		)
	}

	if len(bytes.TrimSpace(rest)) != 0 {
		return nil, fmt.Errorf(
			"invalid pem block: %w", ErrInvalidPrivateKey,
		)
	}

	if block.Type != "PRIVATE KEY" {
		return nil, fmt.Errorf(
			"invalid block type: %w", ErrInvalidPrivateKey,
		)
	}

	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf(
			"parse private key: %w: %w",
			err, ErrInvalidPrivateKey,
		)
	}

	key, ok := parsed.(ed25519.PrivateKey)
	if !ok {
		return nil, fmt.Errorf(
			"invalid private key type-assertion: %w", ErrInvalidPrivateKey,
		)
	}

	if len(key) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf(
			"invalid private key len: %w", ErrInvalidPrivateKey,
		)
	}

	return key, nil
}

func parsePublicKey(filename string) (ed25519.PublicKey, error) {
	//nolint:gosec // G304: filename comes from trusted process configuration, not user input.
	content, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf(
			"read public key: %w: %w",
			err,
			ErrInvalidPublicKey,
		)
	}

	block, rest := pem.Decode(content)
	if block == nil {
		return nil, fmt.Errorf(
			"failed to parse public key PEM block: %w",
			ErrInvalidPublicKey,
		)
	}

	if len(bytes.TrimSpace(rest)) != 0 {
		return nil, fmt.Errorf(
			"invalid pem block: %w", ErrInvalidPublicKey,
		)
	}

	if block.Type != "PUBLIC KEY" {
		return nil, fmt.Errorf(
			"invalid block type: %w", ErrInvalidPublicKey,
		)
	}

	parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf(
			"parse public key: %w: %w",
			err, ErrInvalidPublicKey,
		)
	}

	key, ok := parsed.(ed25519.PublicKey)
	if !ok {
		return nil, fmt.Errorf(
			"invalid public key type-assertion: %w", ErrInvalidPublicKey,
		)
	}

	if len(key) != ed25519.PublicKeySize {
		return nil, fmt.Errorf(
			"invalid public key len: %w", ErrInvalidPublicKey,
		)
	}

	return key, nil
}
