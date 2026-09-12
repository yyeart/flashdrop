package identity

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadTokenConfig_DefaultsAndKeys(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}

	publicFilename, privateFilename := writeTokenKeyPair(t, publicKey, privateKey, "\n\t", "\n\t")

	cfg, err := LoadTokenConfig("", "", 0, publicFilename, privateFilename)
	if err != nil {
		t.Fatalf("LoadTokenConfig() error = %v", err)
	}

	if cfg.Issuer != DefaultTokenIssuer {
		t.Errorf("Issuer = %q, want %q", cfg.Issuer, DefaultTokenIssuer)
	}
	if cfg.Audience != DefaultTokenAudience {
		t.Errorf("Audience = %q, want %q", cfg.Audience, DefaultTokenAudience)
	}
	if cfg.TTL != DefaultTokenTTL {
		t.Errorf("TTL = %v, want %v", cfg.TTL, DefaultTokenTTL)
	}
	if !bytes.Equal(cfg.PrivateKey, privateKey) {
		t.Error("PrivateKey differs from the PKCS#8 key")
	}
	if !bytes.Equal(cfg.PublicKey, publicKey) {
		t.Error("PublicKey differs from the PKIX key")
	}
}

func TestLoadTokenConfig_PreservesExplicitValues(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}

	publicFilename, privateFilename := writeTokenKeyPair(t, publicKey, privateKey, "", "")

	wantTTL := 30 * time.Minute
	cfg, err := LoadTokenConfig("issuer", "audience", wantTTL, publicFilename, privateFilename)
	if err != nil {
		t.Fatalf("LoadTokenConfig() error = %v", err)
	}

	if cfg.Issuer != "issuer" || cfg.Audience != "audience" || cfg.TTL != wantTTL {
		t.Fatalf("LoadTokenConfig() = %#v, want explicit issuer, audience and TTL", cfg)
	}
}

func TestTokenKeyParsers_RejectNonWhitespaceRemainder(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}

	const secretRemainder = "must-not-leak"
	publicFilename, privateFilename := writeTokenKeyPair(
		t, publicKey, privateKey, secretRemainder, secretRemainder,
	)

	if _, err := parsePrivateKey(privateFilename); !errors.Is(err, ErrInvalidPrivateKey) {
		t.Fatalf("parsePrivateKey() error = %v, want ErrInvalidPrivateKey", err)
	} else if strings.Contains(err.Error(), secretRemainder) {
		t.Fatal("parsePrivateKey() error contains key-file contents")
	}

	if _, err := parsePublicKey(publicFilename); !errors.Is(err, ErrInvalidPublicKey) {
		t.Fatalf("parsePublicKey() error = %v, want ErrInvalidPublicKey", err)
	} else if strings.Contains(err.Error(), secretRemainder) {
		t.Fatal("parsePublicKey() error contains key-file contents")
	}
}

func TestTokenKeyParsers_RejectWrongKeyType(t *testing.T) {
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}

	privateDER, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		t.Fatalf("MarshalPKCS8PrivateKey() error = %v", err)
	}
	publicDER, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	if err != nil {
		t.Fatalf("MarshalPKIXPublicKey() error = %v", err)
	}

	privateFilename := writePEMFile(t, "wrong-private.pem", "PRIVATE KEY", privateDER, "")
	publicFilename := writePEMFile(t, "wrong-public.pem", "PUBLIC KEY", publicDER, "")

	if _, err := parsePrivateKey(privateFilename); !errors.Is(err, ErrInvalidPrivateKey) {
		t.Fatalf("parsePrivateKey() error = %v, want ErrInvalidPrivateKey", err)
	}
	if _, err := parsePublicKey(publicFilename); !errors.Is(err, ErrInvalidPublicKey) {
		t.Fatalf("parsePublicKey() error = %v, want ErrInvalidPublicKey", err)
	}
}

func TestTokenKeyParsers_RejectWrongPEMBlockType(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}

	privateDER, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		t.Fatalf("MarshalPKCS8PrivateKey() error = %v", err)
	}
	publicDER, err := x509.MarshalPKIXPublicKey(publicKey)
	if err != nil {
		t.Fatalf("MarshalPKIXPublicKey() error = %v", err)
	}

	privateFilename := writePEMFile(t, "public-as-private.pem", "PUBLIC KEY", privateDER, "")
	publicFilename := writePEMFile(t, "private-as-public.pem", "PRIVATE KEY", publicDER, "")

	if _, err := parsePrivateKey(privateFilename); !errors.Is(err, ErrInvalidPrivateKey) {
		t.Fatalf("parsePrivateKey() error = %v, want ErrInvalidPrivateKey", err)
	}
	if _, err := parsePublicKey(publicFilename); !errors.Is(err, ErrInvalidPublicKey) {
		t.Fatalf("parsePublicKey() error = %v, want ErrInvalidPublicKey", err)
	}
}

func TestTokenKeyParsers_RejectMissingFiles(t *testing.T) {
	missingFilename := filepath.Join(t.TempDir(), "missing.pem")

	if _, err := parsePrivateKey(missingFilename); err == nil {
		t.Fatal("parsePrivateKey() error = nil, want error")
	} else {
		assertKeyParserError(t, err, ErrInvalidPrivateKey, "")
		if !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("parsePrivateKey() error = %v, want os.ErrNotExist", err)
		}
	}

	if _, err := parsePublicKey(missingFilename); err == nil {
		t.Fatal("parsePublicKey() error = nil, want error")
	} else {
		assertKeyParserError(t, err, ErrInvalidPublicKey, "")
		if !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("parsePublicKey() error = %v, want os.ErrNotExist", err)
		}
	}
}

func TestTokenKeyParsers_RejectMalformedPEMWithoutLeakingContents(t *testing.T) {
	const secretContent = "not-a-pem-secret-content"
	filename := writeRawFile(t, "malformed.pem", []byte(secretContent))

	_, privateErr := parsePrivateKey(filename)
	assertKeyParserError(t, privateErr, ErrInvalidPrivateKey, secretContent)

	_, publicErr := parsePublicKey(filename)
	assertKeyParserError(t, publicErr, ErrInvalidPublicKey, secretContent)
}

func TestTokenKeyParsers_RejectMalformedDERWithoutLeakingContents(t *testing.T) {
	const secretDER = "invalid-der-secret-content"
	privateFilename := writePEMFile(t, "malformed-private.pem", "PRIVATE KEY", []byte(secretDER), "")
	publicFilename := writePEMFile(t, "malformed-public.pem", "PUBLIC KEY", []byte(secretDER), "")

	_, privateErr := parsePrivateKey(privateFilename)
	assertKeyParserError(t, privateErr, ErrInvalidPrivateKey, secretDER)

	_, publicErr := parsePublicKey(publicFilename)
	assertKeyParserError(t, publicErr, ErrInvalidPublicKey, secretDER)
}

func assertKeyParserError(t *testing.T, err, sentinel error, forbidden string) {
	t.Helper()

	if !errors.Is(err, sentinel) {
		t.Fatalf("parser error = %v, want %v", err, sentinel)
	}
	if forbidden != "" && strings.Contains(err.Error(), forbidden) {
		t.Fatalf("parser error contains protected input %q", forbidden)
	}
}

func writeTokenKeyPair(
	t *testing.T,
	publicKey ed25519.PublicKey,
	privateKey ed25519.PrivateKey,
	publicRemainder string,
	privateRemainder string,
) (string, string) {
	t.Helper()

	privateDER, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		t.Fatalf("MarshalPKCS8PrivateKey() error = %v", err)
	}
	publicDER, err := x509.MarshalPKIXPublicKey(publicKey)
	if err != nil {
		t.Fatalf("MarshalPKIXPublicKey() error = %v", err)
	}

	return writePEMFile(t, "public.pem", "PUBLIC KEY", publicDER, publicRemainder),
		writePEMFile(t, "private.pem", "PRIVATE KEY", privateDER, privateRemainder)
}

func writePEMFile(t *testing.T, name, blockType string, der []byte, remainder string) string {
	t.Helper()

	content := append(pem.EncodeToMemory(&pem.Block{Type: blockType, Bytes: der}), remainder...)
	return writeRawFile(t, name, content)
}

func writeRawFile(t *testing.T, name string, content []byte) string {
	t.Helper()

	filename := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(filename, content, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	return filename
}
