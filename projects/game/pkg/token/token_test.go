package token

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestHMACSigner_Issue_and_Verify_happy_path(t *testing.T) {
	// given
	signer := NewHMACSigner("test-secret", 1*time.Hour)

	// when
	tokenStr, err := signer.Issue("sess-123", "gw-0", 1)

	// then
	if err != nil {
		t.Fatalf("Issue() unexpected error: %v", err)
	}
	if tokenStr == "" {
		t.Fatal("Issue() returned empty token")
	}

	claims, err := signer.Verify(tokenStr)
	if err != nil {
		t.Fatalf("Verify() unexpected error: %v", err)
	}
	if claims.SessionID != "sess-123" {
		t.Fatalf("SessionID = %q, want %q", claims.SessionID, "sess-123")
	}
	if claims.GatewayID != "gw-0" {
		t.Fatalf("GatewayID = %q, want %q", claims.GatewayID, "gw-0")
	}
	if claims.ReconnectGeneration != 1 {
		t.Fatalf("ReconnectGeneration = %d, want %d", claims.ReconnectGeneration, 1)
	}
	expectedExpiry := time.Now().Add(1 * time.Hour).Unix()
	if claims.ExpiresAt < expectedExpiry-5 || claims.ExpiresAt > expectedExpiry+5 {
		t.Fatalf("ExpiresAt = %d, want approximately %d", claims.ExpiresAt, expectedExpiry)
	}
}

func TestHMACSigner_Verify_expired_token(t *testing.T) {
	// given
	signer := NewHMACSigner("test-secret", -1*time.Second)

	tokenStr, err := signer.Issue("sess-expired", "gw-0", 0)
	if err != nil {
		t.Fatalf("Issue() unexpected error: %v", err)
	}

	// when
	_, err = signer.Verify(tokenStr)

	// then
	if err == nil {
		t.Fatal("Verify() expected error for expired token")
	}
	if !errors.Is(err, ErrTokenExpired) {
		t.Fatalf("Verify() error = %v, want ErrTokenExpired", err)
	}
}

func TestHMACSigner_Verify_tampered_payload(t *testing.T) {
	// given
	signer := NewHMACSigner("test-secret", 1*time.Hour)

	tokenStr, err := signer.Issue("sess-123", "gw-0", 1)
	if err != nil {
		t.Fatalf("Issue() unexpected error: %v", err)
	}

	// tamper: modify the payload portion
	parts := strings.SplitN(tokenStr, ".", 2)
	tamperedPayload := parts[0] + "tampered"
	tamperedToken := tamperedPayload + "." + parts[1]

	// when
	_, err = signer.Verify(tamperedToken)

	// then
	if err == nil {
		t.Fatal("Verify() expected error for tampered payload")
	}
	if !errors.Is(err, ErrTokenInvalid) {
		t.Fatalf("Verify() error = %v, want ErrTokenInvalid", err)
	}
}

func TestHMACSigner_Verify_tampered_signature(t *testing.T) {
	// given
	signer := NewHMACSigner("test-secret", 1*time.Hour)

	tokenStr, err := signer.Issue("sess-123", "gw-0", 1)
	if err != nil {
		t.Fatalf("Issue() unexpected error: %v", err)
	}

	// tamper: modify the signature portion
	parts := strings.SplitN(tokenStr, ".", 2)
	tamperedToken := parts[0] + "." + parts[1] + "tampered"

	// when
	_, err = signer.Verify(tamperedToken)

	// then
	if err == nil {
		t.Fatal("Verify() expected error for tampered signature")
	}
	if !errors.Is(err, ErrTokenInvalid) {
		t.Fatalf("Verify() error = %v, want ErrTokenInvalid", err)
	}
}

func TestHMACSigner_Verify_different_secret(t *testing.T) {
	// given
	issuer := NewHMACSigner("secret-a", 1*time.Hour)
	verifier := NewHMACSigner("secret-b", 1*time.Hour)

	tokenStr, err := issuer.Issue("sess-123", "gw-0", 1)
	if err != nil {
		t.Fatalf("Issue() unexpected error: %v", err)
	}

	// when
	_, err = verifier.Verify(tokenStr)

	// then
	if err == nil {
		t.Fatal("Verify() expected error for different secret")
	}
	if !errors.Is(err, ErrTokenInvalid) {
		t.Fatalf("Verify() error = %v, want ErrTokenInvalid", err)
	}
}

func TestHMACSigner_Verify_malformed_token(t *testing.T) {
	tests := []struct {
		name  string
		token string
	}{
		{name: "no dot separator", token: "justpayload"},
		{name: "empty string", token: ""},
		{name: "empty payload", token: ".signature"},
		{name: "empty signature", token: "payload."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			signer := NewHMACSigner("test-secret", 1*time.Hour)

			// when
			_, err := signer.Verify(tt.token)

			// then
			if err == nil {
				t.Fatal("Verify() expected error for malformed token")
			}
			if !errors.Is(err, ErrTokenInvalid) {
				t.Fatalf("Verify() error = %v, want ErrTokenInvalid", err)
			}
		})
	}
}

func TestHMACSigner_all_claims_fields(t *testing.T) {
	tests := []struct {
		name                string
		sessionID           string
		gatewayID           string
		reconnectGeneration int64
	}{
		{name: "first generation", sessionID: "sess-001", gatewayID: "gw-0", reconnectGeneration: 0},
		{name: "reconnected", sessionID: "sess-002", gatewayID: "gw-3", reconnectGeneration: 5},
		{name: "long ids", sessionID: "session-with-a-very-long-id-abc123", gatewayID: "game-gateway-42", reconnectGeneration: 99},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			fixedNow := time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC)
			signer := &HMACSigner{
				secret: []byte("test-secret"),
				ttl:    30 * time.Minute,
				now:    func() time.Time { return fixedNow },
			}

			// when
			tokenStr, err := signer.Issue(tt.sessionID, tt.gatewayID, tt.reconnectGeneration)
			if err != nil {
				t.Fatalf("Issue() unexpected error: %v", err)
			}

			claims, err := signer.Verify(tokenStr)
			if err != nil {
				t.Fatalf("Verify() unexpected error: %v", err)
			}

			// then
			if claims.SessionID != tt.sessionID {
				t.Fatalf("SessionID = %q, want %q", claims.SessionID, tt.sessionID)
			}
			if claims.GatewayID != tt.gatewayID {
				t.Fatalf("GatewayID = %q, want %q", claims.GatewayID, tt.gatewayID)
			}
			if claims.ReconnectGeneration != tt.reconnectGeneration {
				t.Fatalf("ReconnectGeneration = %d, want %d", claims.ReconnectGeneration, tt.reconnectGeneration)
			}
			expectedExp := fixedNow.Add(30 * time.Minute).Unix()
			if claims.ExpiresAt != expectedExp {
				t.Fatalf("ExpiresAt = %d, want %d", claims.ExpiresAt, expectedExp)
			}
		})
	}
}
