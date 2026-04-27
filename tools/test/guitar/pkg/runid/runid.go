// Package runid generates unique run identifiers for guitar test execution.
package runid

import (
	"crypto/rand"
	"fmt"
)

const (
	// base36Chars is the character set for base36 encoding.
	base36Chars = "0123456789abcdefghijklmnopqrstuvwxyz"
	// prefix is the fixed prefix for all run IDs.
	prefix = "lt"
	// idLen is the length of the random base36 suffix.
	idLen = 6
)

// Generate creates a new run ID with the format "lt" + 6 lowercase base36 random characters.
// The returned value satisfies the deploy env name constraint ^[a-z][a-z0-9]{0,7}$.
func Generate() (string, error) {
	buf := make([]byte, idLen)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate random bytes: %w", err)
	}

	id := make([]byte, idLen)
	for i := 0; i < idLen; i++ {
		id[i] = base36Chars[buf[i]%36]
	}

	return prefix + string(id), nil
}
