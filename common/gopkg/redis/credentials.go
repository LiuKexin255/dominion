package redis

import (
	"crypto/hmac"
	"crypto/sha256"
	"strings"
)

const (
	redisPasswordHMACKey  = "dominion-redis-stable-password"
	redisPasswordAlphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	redisPasswordMinLen   = 24
	redisPasswordJoiner   = "\x00"
)

// generateStablePassword deterministically derives the Redis instance password.
//
// It is byte-for-byte the same algorithm the deploy service uses to inject the
// instance credential (specs/067-redis-infra-support/research.md D4), so a
// client deriving here authenticates against the same deployment.
func generateStablePassword(inputs ...string) string {
	var normalized []string
	for _, input := range inputs {
		normalized = append(normalized, strings.TrimSpace(input))
	}
	mac := hmac.New(sha256.New, []byte(redisPasswordHMACKey))
	_, _ = mac.Write([]byte(strings.Join(normalized, redisPasswordJoiner)))
	sum := mac.Sum(nil)

	var encoded []byte
	for _, b := range sum {
		encoded = append(encoded, redisPasswordAlphabet[int(b)%len(redisPasswordAlphabet)])
	}
	if len(encoded) >= redisPasswordMinLen {
		return string(encoded)
	}

	for len(encoded) < redisPasswordMinLen {
		for _, b := range sum {
			encoded = append(encoded, redisPasswordAlphabet[int(b)%len(redisPasswordAlphabet)])
			if len(encoded) >= redisPasswordMinLen {
				break
			}
		}
	}

	return string(encoded)
}
