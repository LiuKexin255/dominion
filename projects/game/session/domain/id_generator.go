package domain

import (
	"context"
	"crypto/rand"
	"encoding/hex"
)

// IDGenerator generates unique session identifiers.
type IDGenerator interface {
	// NewID generates a new unique session ID.
	NewID(ctx context.Context) (string, error)
}

// CryptoIDGenerator generates IDs using cryptographic random bytes.
type CryptoIDGenerator struct{}

// NewID generates a 32-character hex-encoded session ID from 16 random bytes.
func (g *CryptoIDGenerator) NewID(_ context.Context) (string, error) {
	return newID()
}

// defaultNewID generates 16 random bytes and returns them as a hex string.
func defaultNewID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// newID is a package-level variable allowing test replacement.
var newID = defaultNewID
