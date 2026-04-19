// Package token provides HMAC-SHA256 based token issuance and verification
// for game session connections.
package token

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrTokenExpired indicates that the token has expired.
var ErrTokenExpired = errors.New("token has expired")

// ErrTokenInvalid indicates that the token signature or format is invalid.
var ErrTokenInvalid = errors.New("token is invalid")

// Claims represents the data embedded in a session connection token.
type Claims struct {
	// SessionID identifies the game session.
	SessionID string `json:"session_id"`
	// GatewayID identifies the assigned game gateway instance.
	GatewayID string `json:"gateway_id"`
	// ExpiresAt is the Unix timestamp when the token expires.
	ExpiresAt int64 `json:"exp"`
	// ReconnectGeneration is incremented on each gateway reassignment.
	ReconnectGeneration int64 `json:"reconnect_generation"`
}

// Issuer issues signed tokens with embedded claims.
type Issuer interface {
	// Issue creates a signed token for the given session and gateway.
	Issue(sessionID, gatewayID string, reconnectGeneration int64) (string, error)
}

// Verifier verifies token signatures and extracts claims.
type Verifier interface {
	// Verify checks the token signature and expiry, returning the embedded claims.
	Verify(tokenString string) (*Claims, error)
}

// HMACSigner implements both Issuer and Verifier using HMAC-SHA256.
type HMACSigner struct {
	secret []byte
	ttl    time.Duration
	now    func() time.Time
}

// NewHMACSigner creates an HMACSigner with the given secret key and token TTL.
func NewHMACSigner(secret string, ttl time.Duration) *HMACSigner {
	return &HMACSigner{
		secret: []byte(secret),
		ttl:    ttl,
		now:    time.Now,
	}
}

// Issue creates a signed token in the format: base64(JSON(payload)) + "." + base64(HMAC-SHA256).
func (s *HMACSigner) Issue(sessionID, gatewayID string, reconnectGeneration int64) (string, error) {
	claims := Claims{
		SessionID:           sessionID,
		GatewayID:           gatewayID,
		ExpiresAt:           s.now().Add(s.ttl).Unix(),
		ReconnectGeneration: reconnectGeneration,
	}

	payload, err := json.Marshal(claims)
	if err != nil {
		return "", fmt.Errorf("marshal claims: %w", err)
	}

	encodedPayload := base64.RawURLEncoding.EncodeToString(payload)
	signature := s.computeSignature(encodedPayload)
	encodedSignature := base64.RawURLEncoding.EncodeToString(signature)

	return encodedPayload + "." + encodedSignature, nil
}

// Verify splits the token, verifies the HMAC-SHA256 signature, unmarshals the
// claims, and checks expiry.
func (s *HMACSigner) Verify(tokenString string) (*Claims, error) {
	parts := strings.SplitN(tokenString, ".", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("%w: malformed token", ErrTokenInvalid)
	}

	encodedPayload, encodedSignature := parts[0], parts[1]

	expectedSignature := s.computeSignature(encodedPayload)
	signature, err := base64.RawURLEncoding.DecodeString(encodedSignature)
	if err != nil {
		return nil, fmt.Errorf("%w: decode signature: %w", ErrTokenInvalid, err)
	}

	if !hmac.Equal(signature, expectedSignature) {
		return nil, fmt.Errorf("%w: signature mismatch", ErrTokenInvalid)
	}

	payload, err := base64.RawURLEncoding.DecodeString(encodedPayload)
	if err != nil {
		return nil, fmt.Errorf("%w: decode payload: %w", ErrTokenInvalid, err)
	}

	var claims Claims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, fmt.Errorf("%w: unmarshal claims: %w", ErrTokenInvalid, err)
	}

	if s.now().Unix() > claims.ExpiresAt {
		return nil, fmt.Errorf("%w: expired at %d", ErrTokenExpired, claims.ExpiresAt)
	}

	return &claims, nil
}

// computeSignature returns the HMAC-SHA256 of the given data using the secret key.
func (s *HMACSigner) computeSignature(data string) []byte {
	mac := hmac.New(sha256.New, s.secret)
	mac.Write([]byte(data))
	return mac.Sum(nil)
}
