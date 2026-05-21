package token

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestHMACSigner_Issue_and_Verify_happy_path(t *testing.T) {
	signer := NewHMACSigner("test-secret", 1*time.Hour)

	tokenStr, err := signer.Issue("sess-123", "gw-0", 1, TokenAudienceInternal, 0)
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
	if claims.OwnerGatewayID != "gw-0" {
		t.Fatalf("OwnerGatewayID = %q, want %q", claims.OwnerGatewayID, "gw-0")
	}
	if claims.ReconnectGeneration != 0 {
		t.Fatalf("ReconnectGeneration = %d, want %d", claims.ReconnectGeneration, 0)
	}
	if claims.Audience != TokenAudienceInternal {
		t.Fatalf("Audience = %q, want %q", claims.Audience, TokenAudienceInternal)
	}
	expectedExpiry := time.Now().Add(1 * time.Hour).Unix()
	if claims.ExpiresAt < expectedExpiry-5 || claims.ExpiresAt > expectedExpiry+5 {
		t.Fatalf("ExpiresAt = %d, want approximately %d", claims.ExpiresAt, expectedExpiry)
	}
}

func TestHMACSigner_Verify_expired_token(t *testing.T) {
	signer := NewHMACSigner("test-secret", -1*time.Second)

	tokenStr, err := signer.Issue("sess-expired", "gw-0", 1, TokenAudienceInternal, 0)
	if err != nil {
		t.Fatalf("Issue() unexpected error: %v", err)
	}

	_, err = signer.Verify(tokenStr)
	if err == nil {
		t.Fatal("Verify() expected error for expired token")
	}
	if !errors.Is(err, ErrTokenExpired) {
		t.Fatalf("Verify() error = %v, want ErrTokenExpired", err)
	}
}

func TestHMACSigner_VerifyWithGrace_accepts_within_grace(t *testing.T) {
	frozenNow := time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC)
	signer := NewHMACSigner("test-secret", 15*time.Minute)
	signer.SetNow(func() time.Time { return frozenNow })

	pastSigner := NewHMACSigner("test-secret", 15*time.Minute)
	pastSigner.SetNow(func() time.Time { return frozenNow.Add(-20 * time.Minute) })

	tokenStr, err := pastSigner.Issue("sess-1", "gw-0", 1, TokenAudienceInternal, 2)
	if err != nil {
		t.Fatalf("Issue() unexpected error: %v", err)
	}

	// Verify with 60-minute grace should accept the 20-min-expired token.
	claims, err := signer.VerifyWithGrace(tokenStr, 60*time.Minute)
	if err != nil {
		t.Fatalf("VerifyWithGrace() error = %v, want success within grace window", err)
	}
	if claims.SessionID != "sess-1" {
		t.Fatalf("SessionID = %q, want %q", claims.SessionID, "sess-1")
	}
}

func TestHMACSigner_VerifyWithGrace_rejects_outside_grace(t *testing.T) {
	frozenNow := time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC)
	signer := NewHMACSigner("test-secret", 15*time.Minute)
	signer.SetNow(func() time.Time { return frozenNow })

	pastSigner := NewHMACSigner("test-secret", 15*time.Minute)
	pastSigner.SetNow(func() time.Time { return frozenNow.Add(-90 * time.Minute) })

	tokenStr, err := pastSigner.Issue("sess-1", "gw-0", 1, TokenAudienceInternal, 0)
	if err != nil {
		t.Fatalf("Issue() unexpected error: %v", err)
	}

	_, err = signer.VerifyWithGrace(tokenStr, 60*time.Minute)
	if err == nil {
		t.Fatal("VerifyWithGrace() expected error outside grace window")
	}
}

func TestHMACSigner_Verify_tampered_payload(t *testing.T) {
	signer := NewHMACSigner("test-secret", 1*time.Hour)

	tokenStr, err := signer.Issue("sess-123", "gw-0", 1, TokenAudienceInternal, 0)
	if err != nil {
		t.Fatalf("Issue() unexpected error: %v", err)
	}

	parts := strings.SplitN(tokenStr, ".", 2)
	tamperedPayload := parts[0] + "tampered"
	tamperedToken := tamperedPayload + "." + parts[1]

	_, err = signer.Verify(tamperedToken)
	if err == nil {
		t.Fatal("Verify() expected error for tampered payload")
	}
	if !errors.Is(err, ErrTokenInvalid) {
		t.Fatalf("Verify() error = %v, want ErrTokenInvalid", err)
	}
}

func TestHMACSigner_Verify_tampered_signature(t *testing.T) {
	signer := NewHMACSigner("test-secret", 1*time.Hour)

	tokenStr, err := signer.Issue("sess-123", "gw-0", 1, TokenAudienceInternal, 0)
	if err != nil {
		t.Fatalf("Issue() unexpected error: %v", err)
	}

	parts := strings.SplitN(tokenStr, ".", 2)
	tamperedToken := parts[0] + "." + parts[1] + "tampered"

	_, err = signer.Verify(tamperedToken)
	if err == nil {
		t.Fatal("Verify() expected error for tampered signature")
	}
	if !errors.Is(err, ErrTokenInvalid) {
		t.Fatalf("Verify() error = %v, want ErrTokenInvalid", err)
	}
}

func TestHMACSigner_Verify_different_secret(t *testing.T) {
	issuer := NewHMACSigner("secret-a", 1*time.Hour)
	verifier := NewHMACSigner("secret-b", 1*time.Hour)

	tokenStr, err := issuer.Issue("sess-123", "gw-0", 1, TokenAudienceInternal, 0)
	if err != nil {
		t.Fatalf("Issue() unexpected error: %v", err)
	}

	_, err = verifier.Verify(tokenStr)
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
			signer := NewHMACSigner("test-secret", 1*time.Hour)
			_, err := signer.Verify(tt.token)
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
		ownerGatewayID      string
		ownerEpoch          int64
		audience            string
		reconnectGeneration int64
	}{
		{name: "first generation", sessionID: "sess-001", ownerGatewayID: "gw-0", ownerEpoch: 1, audience: TokenAudienceInternal, reconnectGeneration: 0},
		{name: "reconnected", sessionID: "sess-002", ownerGatewayID: "gw-3", ownerEpoch: 2, audience: TokenAudienceInternal, reconnectGeneration: 5},
		{name: "long ids", sessionID: "session-with-a-very-long-id-abc123", ownerGatewayID: "game-gateway-42", ownerEpoch: 1, audience: TokenAudienceInternal, reconnectGeneration: 99},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixedNow := time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC)
			signer := NewHMACSigner("test-secret", 30*time.Minute)
			signer.SetNow(func() time.Time { return fixedNow })

			tokenStr, err := signer.Issue(tt.sessionID, tt.ownerGatewayID, tt.ownerEpoch, tt.audience, tt.reconnectGeneration)
			if err != nil {
				t.Fatalf("Issue() unexpected error: %v", err)
			}

			claims, err := signer.Verify(tokenStr)
			if err != nil {
				t.Fatalf("Verify() unexpected error: %v", err)
			}

			if claims.SessionID != tt.sessionID {
				t.Fatalf("SessionID = %q, want %q", claims.SessionID, tt.sessionID)
			}
			if claims.OwnerGatewayID != tt.ownerGatewayID {
				t.Fatalf("OwnerGatewayID = %q, want %q", claims.OwnerGatewayID, tt.ownerGatewayID)
			}
			if claims.OwnerEpoch != tt.ownerEpoch {
				t.Fatalf("OwnerEpoch = %d, want %d", claims.OwnerEpoch, tt.ownerEpoch)
			}
			if claims.Audience != tt.audience {
				t.Fatalf("Audience = %q, want %q", claims.Audience, tt.audience)
			}
			if claims.ReconnectGeneration != tt.reconnectGeneration {
				t.Fatalf("ReconnectGeneration = %d, want %d", claims.ReconnectGeneration, tt.reconnectGeneration)
			}
			expectedExp := fixedNow.Add(30 * time.Minute).Unix()
			if claims.ExpiresAt != expectedExp {
				t.Fatalf("ExpiresAt = %d, want %d", claims.ExpiresAt, expectedExp)
			}
			if claims.IssuedAt != fixedNow.Unix() {
				t.Fatalf("IssuedAt = %d, want %d", claims.IssuedAt, fixedNow.Unix())
			}
		})
	}
}

func TestValidateOwnerEpoch(t *testing.T) {
	tests := []struct {
		name    string
		epoch   int64
		wantErr bool
	}{
		{name: "valid epoch", epoch: 1, wantErr: false},
		{name: "valid large epoch", epoch: 100, wantErr: false},
		{name: "zero epoch invalid", epoch: 0, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			claims := &Claims{OwnerEpoch: tt.epoch}
			err := claims.ValidateOwnerEpoch()
			if tt.wantErr && err == nil {
				t.Fatal("ValidateOwnerEpoch() expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("ValidateOwnerEpoch() unexpected error: %v", err)
			}
		})
	}
}

func TestValidateAudience(t *testing.T) {
	tests := []struct {
		name     string
		claims   Claims
		expected string
		wantErr  bool
	}{
		{name: "matching audience", claims: Claims{Audience: TokenAudienceInternal}, expected: TokenAudienceInternal, wantErr: false},
		{name: "mismatched audience", claims: Claims{Audience: "wrong-aud"}, expected: TokenAudienceInternal, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.claims.ValidateAudience(tt.expected)
			if tt.wantErr && err == nil {
				t.Fatal("ValidateAudience() expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("ValidateAudience() unexpected error: %v", err)
			}
		})
	}
}

func TestHMACSigner_Issue_includes_all_fields(t *testing.T) {
	fixedNow := time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC)
	signer := NewHMACSigner("test-secret", 30*time.Minute)
	signer.SetNow(func() time.Time { return fixedNow })

	tokenStr, err := signer.Issue("sess-all", "gw-5", 10, TokenAudienceInternal, 3)
	if err != nil {
		t.Fatalf("Issue() unexpected error: %v", err)
	}

	claims, err := signer.Verify(tokenStr)
	if err != nil {
		t.Fatalf("Verify() unexpected error: %v", err)
	}

	if claims.SessionID != "sess-all" {
		t.Fatalf("SessionID = %q, want sess-all", claims.SessionID)
	}
	if claims.OwnerGatewayID != "gw-5" {
		t.Fatalf("OwnerGatewayID = %q, want gw-5", claims.OwnerGatewayID)
	}
	if claims.OwnerEpoch != 10 {
		t.Fatalf("OwnerEpoch = %d, want 10", claims.OwnerEpoch)
	}
	if claims.Audience != TokenAudienceInternal {
		t.Fatalf("Audience = %q, want %q", claims.Audience, TokenAudienceInternal)
	}
	if claims.IssuedAt != fixedNow.Unix() {
		t.Fatalf("IssuedAt = %d, want %d", claims.IssuedAt, fixedNow.Unix())
	}
	if claims.ExpiresAt != fixedNow.Add(30*time.Minute).Unix() {
		t.Fatalf("ExpiresAt = %d, want %d", claims.ExpiresAt, fixedNow.Add(30*time.Minute).Unix())
	}
	if claims.ReconnectGeneration != 3 {
		t.Fatalf("ReconnectGeneration = %d, want 3", claims.ReconnectGeneration)
	}
}
