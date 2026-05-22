// Package token provides HMAC-SHA256 based token issuance and verification
// for game runtime connections. It supports Issuer (sign tokens), Verifier
// (full validation including signature, expiry, audience), and OwnerExtractor
// (decode tokens without verification for routing purposes).
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

// TokenAudienceInternal is the audience value for runtime-internal routing
// tokens.
const TokenAudienceInternal = "game-runtime"

// ErrTokenExpired indicates that the token has expired.
var ErrTokenExpired = errors.New("token has expired")

// ErrTokenInvalid indicates that the token signature or format is invalid.
var ErrTokenInvalid = errors.New("token is invalid")

// ErrInvalidOwnerEpoch indicates that the token has an invalid owner epoch (0).
var ErrInvalidOwnerEpoch = errors.New("token owner epoch is invalid (must be >= 1)")

// ErrInvalidAudience indicates that the token audience does not match the
// expected value.
var ErrInvalidAudience = errors.New("token audience does not match expected value")

// Claims represents the data embedded in a session connection token.
type Claims struct {
	// SessionID identifies the game session.
	SessionID string `json:"session_id"`
	// OwnerRuntimeID identifies the runtime instance that owns this session.
	OwnerRuntimeID string `json:"owner_runtime_id"`
	// OwnerEpoch is the ownership epoch (monotonically increasing) for failover.
	// Must be >= 1. 0 indicates an invalid/uninitialized token.
	OwnerEpoch int64 `json:"owner_epoch"`
	// Audience identifies the intended token consumer.
	Audience string `json:"aud"`
	// IssuedAt is the Unix timestamp when the token was issued.
	IssuedAt int64 `json:"iat"`
	// ExpiresAt is the Unix timestamp when the token expires.
	ExpiresAt int64 `json:"exp"`
	// ReconnectGeneration is incremented on each runtime reassignment.
	ReconnectGeneration int64 `json:"reconnect_generation"`
}

// Issuer issues signed tokens with embedded claims.
type Issuer interface {
	// Issue creates a signed token for the given session and runtime.
	Issue(sessionID, ownerRuntimeID string, ownerEpoch int64, audience string, reconnectGeneration int64) (string, error)
}

// Verifier verifies token signatures and extracts claims.
type Verifier interface {
	// Verify checks the token signature and expiry, returning the embedded claims.
	Verify(tokenString string) (*Claims, error)
	// VerifyWithGrace checks the token signature, allowing tokens expired within
	// the grace duration to pass expiry validation.
	VerifyWithGrace(tokenString string, grace time.Duration) (*Claims, error)
}

// OwnerExtractor decodes tokens without performing signature or expiry
// verification. This is useful for routing where the full token content needs
// to be read before the Verifier is available.
type OwnerExtractor interface {
	// ParseRoutingClaims decodes the token payload without verifying the
	// signature, expiry, or audience. It is intended for routing decisions
	// where full validation is performed later by a Verifier.
	ParseRoutingClaims(tokenString string) (*Claims, error)
}

// ValidateOwnerEpoch returns an error if the owner epoch is 0 (invalid).
func (c *Claims) ValidateOwnerEpoch() error {
	if c.OwnerEpoch == 0 {
		return ErrInvalidOwnerEpoch
	}
	return nil
}

// ValidateAudience returns an error if the token audience does not match the
// expected value.
func (c *Claims) ValidateAudience(expected string) error {
	if c.Audience != expected {
		return fmt.Errorf("%w: got %q, want %q", ErrInvalidAudience, c.Audience, expected)
	}
	return nil
}

// HMACSigner implements Issuer, Verifier, and OwnerExtractor using HMAC-SHA256.
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

// SetNow overrides the clock function used for token timestamps and expiry
// checks. Intended for testing.
func (s *HMACSigner) SetNow(fn func() time.Time) {
	s.now = fn
}

// Issue creates a signed token in the format: base64(JSON(payload)) + "." + base64(HMAC-SHA256).
func (s *HMACSigner) Issue(sessionID, ownerRuntimeID string, ownerEpoch int64, audience string, reconnectGeneration int64) (string, error) {
	claims := Claims{
		SessionID:           sessionID,
		OwnerRuntimeID:      ownerRuntimeID,
		OwnerEpoch:          ownerEpoch,
		Audience:            audience,
		IssuedAt:            s.now().Unix(),
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
	return s.verifyCore(tokenString, nil)
}

// VerifyWithGrace splits the token, verifies the HMAC-SHA256 signature,
// unmarshals the claims, and checks expiry. Tokens expired within the grace
// duration are accepted.
func (s *HMACSigner) VerifyWithGrace(tokenString string, grace time.Duration) (*Claims, error) {
	return s.verifyCore(tokenString, &grace)
}

// ParseRoutingClaims decodes the token payload without verifying the signature,
// expiry, or audience. Intended for routing where full validation is deferred.
func (s *HMACSigner) ParseRoutingClaims(tokenString string) (*Claims, error) {
	return parseRoutingClaimsCore(tokenString)
}

// ValidateRuntimeToken performs full validation: signature verification, owner
// epoch check, and audience check against the expected audience.
func (s *HMACSigner) ValidateRuntimeToken(tokenString string, expectedAudience string) (*Claims, error) {
	claims, err := s.Verify(tokenString)
	if err != nil {
		return nil, err
	}
	if err := claims.ValidateOwnerEpoch(); err != nil {
		return nil, err
	}
	if err := claims.ValidateAudience(expectedAudience); err != nil {
		return nil, err
	}
	return claims, nil
}

// verifyCore is the shared verification logic. When grace is non-nil, tokens
// expired within the grace window are still accepted.
func (s *HMACSigner) verifyCore(tokenString string, grace *time.Duration) (*Claims, error) {
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

	expiryThreshold := claims.ExpiresAt
	if grace != nil {
		expiryThreshold += int64(grace.Seconds())
	}

	if s.now().Unix() > expiryThreshold {
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

// Parser is a zero-field token decoder that implements OwnerExtractor.
// It performs base64 decoding and JSON unmarshalling only — no signature,
// expiry, or audience checks. Use this when you only need to read the
// claims for routing decisions and do not have access to the signing secret.
type Parser struct{}

// NewParser creates a new Parser.
func NewParser() *Parser {
	return &Parser{}
}

// ParseRoutingClaims decodes the token payload without verifying the signature,
// expiry, or audience.
func (p *Parser) ParseRoutingClaims(tokenString string) (*Claims, error) {
	return parseRoutingClaimsCore(tokenString)
}

// parseRoutingClaimsCore is the shared decoding logic used by both HMACSigner
// and Parser. It base64-decodes the payload and JSON-unmarshals the claims,
// without performing any signature, expiry, or audience validation.
func parseRoutingClaimsCore(tokenString string) (*Claims, error) {
	parts := strings.SplitN(tokenString, ".", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("%w: malformed token", ErrTokenInvalid)
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, fmt.Errorf("%w: decode payload: %w", ErrTokenInvalid, err)
	}

	var claims Claims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, fmt.Errorf("%w: unmarshal claims: %w", ErrTokenInvalid, err)
	}

	return &claims, nil
}
