package identity

import (
	"strings"
	"testing"
	"time"
)

func TestMintJWT_VerifyJWT(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}

	token, err := MintJWT("user-abc12345", key)
	if err != nil {
		t.Fatalf("MintJWT: %v", err)
	}
	if len(strings.Split(token, ".")) != 3 {
		t.Error("expected 3-part JWT")
	}

	claims, err := VerifyJWT(token, key)
	if err != nil {
		t.Fatalf("VerifyJWT: %v", err)
	}
	if claims.Sub != "user-abc12345" {
		t.Errorf("expected sub=user-abc12345, got %s", claims.Sub)
	}
	if claims.Jti == "" {
		t.Error("expected non-empty jti")
	}
	if claims.Exp <= 0 {
		t.Error("expected exp > 0")
	}
}

func TestVerifyJWT_WrongKey(t *testing.T) {
	key1 := make([]byte, 32)
	key2 := make([]byte, 32)
	for i := range key2 {
		key2[i] = 0xFF
	}

	token, _ := MintJWT("user-abc12345", key1)
	_, err := VerifyJWT(token, key2)
	if err != ErrUnauthenticated {
		t.Errorf("expected ErrUnauthenticated, got %v", err)
	}
}

func TestVerifyJWT_Expired(t *testing.T) {
	key := make([]byte, 32)
	claims := JWTClaims{
		Sub: "user-abc12345",
		Exp: time.Now().Add(-time.Hour).Unix(),
		Iat: time.Now().Add(-2 * time.Hour).Unix(),
		Jti: "test",
	}
	token, err := buildJWT(claims, key)
	if err != nil {
		t.Fatalf("buildJWT: %v", err)
	}
	_, err = VerifyJWT(token, key)
	if err != ErrUnauthenticated {
		t.Errorf("expected ErrUnauthenticated for expired token, got %v", err)
	}
}

func TestVerifyJWT_Tampered(t *testing.T) {
	key := make([]byte, 32)
	token, _ := MintJWT("user-abc12345", key)
	// Tamper with the payload part.
	parts := strings.Split(token, ".")
	parts[1] = parts[1] + "X"
	tampered := strings.Join(parts, ".")
	_, err := VerifyJWT(tampered, key)
	if err != ErrUnauthenticated {
		t.Errorf("expected ErrUnauthenticated for tampered token, got %v", err)
	}
}
