package identity

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// JWTDuration is the session token lifetime (v2.6-design § 6.5: 7 days fixed).
const JWTDuration = 7 * 24 * time.Hour

// JWTClaims holds the payload of an identity session token.
type JWTClaims struct {
	Sub string `json:"sub"` // identity_id
	Exp int64  `json:"exp"` // unix timestamp
	Iat int64  `json:"iat"`
	Jti string `json:"jti"` // unique token id
}

// MintJWT creates a signed HS256 JWT using the provided 32-byte signing key.
func MintJWT(identityID string, signingKey []byte) (string, error) {
	jti, err := randomHex(16)
	if err != nil {
		return "", fmt.Errorf("jwt: random jti: %w", err)
	}
	now := time.Now().UTC()
	claims := JWTClaims{
		Sub: identityID,
		Exp: now.Add(JWTDuration).Unix(),
		Iat: now.Unix(),
		Jti: jti,
	}
	return buildJWT(claims, signingKey)
}

// VerifyJWT verifies signature + expiry and returns the claims.
func VerifyJWT(token string, signingKey []byte) (*JWTClaims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, ErrUnauthenticated
	}
	// Verify signature.
	headerPayload := parts[0] + "." + parts[1]
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return nil, ErrUnauthenticated
	}
	expected := sign([]byte(headerPayload), signingKey)
	if !hmacEqual(expected, sig) {
		return nil, ErrUnauthenticated
	}
	// Decode payload.
	payloadJSON, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, ErrUnauthenticated
	}
	var claims JWTClaims
	if err := json.Unmarshal(payloadJSON, &claims); err != nil {
		return nil, ErrUnauthenticated
	}
	if time.Now().UTC().Unix() > claims.Exp {
		return nil, ErrUnauthenticated
	}
	return &claims, nil
}

func buildJWT(claims JWTClaims, signingKey []byte) (string, error) {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	payloadJSON, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	payload := base64.RawURLEncoding.EncodeToString(payloadJSON)
	headerPayload := header + "." + payload
	sig := sign([]byte(headerPayload), signingKey)
	return headerPayload + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

func sign(data, key []byte) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write(data)
	return mac.Sum(nil)
}

func hmacEqual(a, b []byte) bool {
	return hmac.Equal(a, b)
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", b), nil
}

// ErrTokenExpired signals an expired JWT (returned by VerifyJWT).
var ErrTokenExpired = errors.New("auth: token expired")
