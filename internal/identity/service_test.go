package identity

import (
	"bytes"
	"crypto/ed25519"
	"testing"
	"time"
)

func validTestTokenConfig() TokenConfig {
	privateKey := ed25519.NewKeyFromSeed(make([]byte, ed25519.SeedSize))
	publicKey, ok := privateKey.Public().(ed25519.PublicKey)
	if !ok {
		panic("ed25519 private key returned a non-ed25519 public key")
	}

	return TokenConfig{
		Issuer: "flashdrop", Audience: "flashdrop-api", TTL: 15 * time.Minute,
		PrivateKey: privateKey,
		PublicKey:  publicKey,
	}
}

func TestNewService_TokenConfig(t *testing.T) {
	tests := []struct {
		name      string
		change    func(*TokenConfig)
		wantError bool
	}{
		{name: "valid"},
		{name: "empty issuer", change: func(c *TokenConfig) { c.Issuer = "" }, wantError: true},
		{name: "whitespace issuer", change: func(c *TokenConfig) { c.Issuer = " \t\n" }, wantError: true},
		{name: "empty audience", change: func(c *TokenConfig) { c.Audience = "" }, wantError: true},
		{name: "whitespace audience", change: func(c *TokenConfig) { c.Audience = " \t\n" }, wantError: true},
		{name: "zero TTL", change: func(c *TokenConfig) { c.TTL = 0 }, wantError: true},
		{name: "negative TTL", change: func(c *TokenConfig) { c.TTL = -time.Second }, wantError: true},
		{name: "fractional TTL", change: func(c *TokenConfig) { c.TTL = time.Second + time.Nanosecond }, wantError: true},
		{name: "missing key", change: func(c *TokenConfig) { c.PrivateKey = nil }, wantError: true},
		{name: "empty key", change: func(c *TokenConfig) { c.PrivateKey = ed25519.PrivateKey{} }, wantError: true},
		{name: "short key", change: func(c *TokenConfig) { c.PrivateKey = make(ed25519.PrivateKey, ed25519.PrivateKeySize-1) }, wantError: true},
		{name: "long key", change: func(c *TokenConfig) { c.PrivateKey = make(ed25519.PrivateKey, ed25519.PrivateKeySize+1) }, wantError: true},
		{name: "missing public key", change: func(c *TokenConfig) { c.PublicKey = nil }, wantError: true},
		{name: "short public key", change: func(c *TokenConfig) { c.PublicKey = make(ed25519.PublicKey, ed25519.PublicKeySize-1) }, wantError: true},
		{name: "long public key", change: func(c *TokenConfig) { c.PublicKey = make(ed25519.PublicKey, ed25519.PublicKeySize+1) }, wantError: true},
		{name: "mismatched key pair", change: func(c *TokenConfig) {
			otherPrivateKey := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{1}, ed25519.SeedSize))
			otherPublicKey, ok := otherPrivateKey.Public().(ed25519.PublicKey)
			if !ok {
				panic("ed25519 private key returned a non-ed25519 public key")
			}
			c.PublicKey = otherPublicKey
		}, wantError: true},
		{name: "private key with corrupted public half", change: func(c *TokenConfig) {
			c.PrivateKey = bytes.Clone(c.PrivateKey)
			c.PublicKey = bytes.Clone(c.PublicKey)
			c.PrivateKey[ed25519.SeedSize] ^= 0xff
			c.PublicKey[0] ^= 0xff
		}, wantError: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validTestTokenConfig()
			if tt.change != nil {
				tt.change(&cfg)
			}
			service, err := NewService(&recordingUserRepository{}, cfg)
			checkNewServiceResult(t, service, err, tt.wantError)
		})
	}
}

func checkNewServiceResult(t *testing.T, service *Service, err error, wantError bool) {
	t.Helper()
	if wantError {
		if err == nil {
			t.Error("NewService() error = nil, want error")
		}
		if service != nil {
			t.Error("NewService() returned a service for invalid configuration")
		}
		return
	}
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	if service == nil {
		t.Fatal("NewService() returned nil service")
	}
}

func TestNewService_CopiesTokenKeys(t *testing.T) {
	cfg := validTestTokenConfig()
	wantPrivateKey := bytes.Clone(cfg.PrivateKey)
	wantPublicKey := bytes.Clone(cfg.PublicKey)
	service, err := NewService(&recordingUserRepository{}, cfg)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	for i := range cfg.PrivateKey {
		cfg.PrivateKey[i] ^= 0xff
	}
	for i := range cfg.PublicKey {
		cfg.PublicKey[i] ^= 0xff
	}
	if !bytes.Equal(service.tokenConfig.PrivateKey, wantPrivateKey) {
		t.Fatal("service private key changed after mutating the original slice")
	}
	if !bytes.Equal(service.tokenConfig.PublicKey, wantPublicKey) {
		t.Fatal("service public key changed after mutating the original slice")
	}
}
