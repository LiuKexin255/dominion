package mongo

import (
	"crypto/hmac"
	"crypto/sha256"
	"strings"
)

const (
	mongoPasswordHMACKey  = "dominion-mongo-stable-password"
	mongoPasswordAlphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	mongoPasswordMinLen   = 24
	mongoPasswordJoiner   = "\x00"
)

// generateStablePassword deterministically derives the MongoDB admin password.
func generateStablePassword(inputs ...string) string {
	normalized := make([]string, 0, len(inputs))
	for _, input := range inputs {
		normalized = append(normalized, strings.TrimSpace(input))
	}

	mac := hmac.New(sha256.New, []byte(mongoPasswordHMACKey))
	_, _ = mac.Write([]byte(strings.Join(normalized, mongoPasswordJoiner)))
	sum := mac.Sum(nil)

	encoded := make([]byte, 0, len(sum))
	for _, b := range sum {
		encoded = append(encoded, mongoPasswordAlphabet[int(b)%len(mongoPasswordAlphabet)])
	}
	if len(encoded) >= mongoPasswordMinLen {
		return string(encoded)
	}

	for len(encoded) < mongoPasswordMinLen {
		for _, b := range sum {
			encoded = append(encoded, mongoPasswordAlphabet[int(b)%len(mongoPasswordAlphabet)])
			if len(encoded) >= mongoPasswordMinLen {
				break
			}
		}
	}

	return string(encoded)
}
