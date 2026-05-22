package token

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestHMACSigner_Issue_and_Verify_happy_path(t *testing.T) {
	signer := NewHMACSigner("test-secret", 1*time.Hour)

	tokenStr, err := signer.Issue("sess-123", "rt-0", 1, TokenAudienceInternal, 0)
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
	if claims.OwnerRuntimeID != "rt-0" {
		t.Fatalf("OwnerRuntimeID = %q, want %q", claims.OwnerRuntimeID, "rt-0")
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

	tokenStr, err := signer.Issue("sess-expired", "rt-0", 1, TokenAudienceInternal, 0)
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

	tokenStr, err := pastSigner.Issue("sess-1", "rt-0", 1, TokenAudienceInternal, 2)
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

	tokenStr, err := pastSigner.Issue("sess-1", "rt-0", 1, TokenAudienceInternal, 0)
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

	tokenStr, err := signer.Issue("sess-123", "rt-0", 1, TokenAudienceInternal, 0)
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

	tokenStr, err := signer.Issue("sess-123", "rt-0", 1, TokenAudienceInternal, 0)
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

	tokenStr, err := issuer.Issue("sess-123", "rt-0", 1, TokenAudienceInternal, 0)
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
		ownerRuntimeID      string
		ownerEpoch          int64
		audience            string
		reconnectGeneration int64
	}{
		{name: "first generation", sessionID: "sess-001", ownerRuntimeID: "rt-0", ownerEpoch: 1, audience: TokenAudienceInternal, reconnectGeneration: 0},
		{name: "reconnected", sessionID: "sess-002", ownerRuntimeID: "rt-3", ownerEpoch: 2, audience: TokenAudienceInternal, reconnectGeneration: 5},
		{name: "long ids", sessionID: "session-with-a-very-long-id-abc123", ownerRuntimeID: "game-runtime-42", ownerEpoch: 1, audience: TokenAudienceInternal, reconnectGeneration: 99},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixedNow := time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC)
			signer := NewHMACSigner("test-secret", 30*time.Minute)
			signer.SetNow(func() time.Time { return fixedNow })

			tokenStr, err := signer.Issue(tt.sessionID, tt.ownerRuntimeID, tt.ownerEpoch, tt.audience, tt.reconnectGeneration)
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
			if claims.OwnerRuntimeID != tt.ownerRuntimeID {
				t.Fatalf("OwnerRuntimeID = %q, want %q", claims.OwnerRuntimeID, tt.ownerRuntimeID)
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

	tokenStr, err := signer.Issue("sess-all", "rt-5", 10, TokenAudienceInternal, 3)
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
	if claims.OwnerRuntimeID != "rt-5" {
		t.Fatalf("OwnerRuntimeID = %q, want rt-5", claims.OwnerRuntimeID)
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

func TestParseRoutingClaims_no_verification(t *testing.T) {
	signer := NewHMACSigner("test-secret", 1*time.Hour)
	// Issue a token with a specific ownerRuntimeID
	tokenStr, err := signer.Issue("sess-routing", "rt-7", 5, TokenAudienceInternal, 1)
	if err != nil {
		t.Fatalf("Issue() unexpected error: %v", err)
	}

	claims, err := signer.ParseRoutingClaims(tokenStr)
	if err != nil {
		t.Fatalf("ParseRoutingClaims() unexpected error: %v", err)
	}

	if claims.OwnerRuntimeID != "rt-7" {
		t.Fatalf("OwnerRuntimeID = %q, want rt-7", claims.OwnerRuntimeID)
	}
	if claims.OwnerEpoch != 5 {
		t.Fatalf("OwnerEpoch = %d, want 5", claims.OwnerEpoch)
	}
	if claims.SessionID != "sess-routing" {
		t.Fatalf("SessionID = %q, want sess-routing", claims.SessionID)
	}
}

func TestParseRoutingClaims_expired_accepted(t *testing.T) {
	signer := NewHMACSigner("test-secret", -1*time.Second)
	tokenStr, err := signer.Issue("sess-expired-routing", "rt-9", 3, TokenAudienceInternal, 0)
	if err != nil {
		t.Fatalf("Issue() unexpected error: %v", err)
	}

	// ParseRoutingClaims should accept expired tokens (no expiry check)
	claims, err := signer.ParseRoutingClaims(tokenStr)
	if err != nil {
		t.Fatalf("ParseRoutingClaims() unexpected error for expired token: %v", err)
	}
	if claims.OwnerRuntimeID != "rt-9" {
		t.Fatalf("OwnerRuntimeID = %q, want rt-9", claims.OwnerRuntimeID)
	}
}

func TestValidateRuntimeToken_rejects_expired(t *testing.T) {
	signer := NewHMACSigner("test-secret", -1*time.Second)
	tokenStr, err := signer.Issue("sess-val", "rt-0", 1, TokenAudienceInternal, 0)
	if err != nil {
		t.Fatalf("Issue() unexpected error: %v", err)
	}

	_, err = signer.ValidateRuntimeToken(tokenStr, TokenAudienceInternal)
	if err == nil {
		t.Fatal("ValidateRuntimeToken() expected error for expired token")
	}
}

func TestValidateRuntimeToken_success(t *testing.T) {
	signer := NewHMACSigner("test-secret", 1*time.Hour)
	tokenStr, err := signer.Issue("sess-val-ok", "rt-2", 1, TokenAudienceInternal, 0)
	if err != nil {
		t.Fatalf("Issue() unexpected error: %v", err)
	}

	claims, err := signer.ValidateRuntimeToken(tokenStr, TokenAudienceInternal)
	if err != nil {
		t.Fatalf("ValidateRuntimeToken() unexpected error: %v", err)
	}
	if claims.OwnerRuntimeID != "rt-2" {
		t.Fatalf("OwnerRuntimeID = %q, want rt-2", claims.OwnerRuntimeID)
	}
}

func TestValidateRuntimeToken_rejects_zero_epoch(t *testing.T) {
	fixedNow := time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC)
	signer := NewHMACSigner("test-secret", 1*time.Hour)
	signer.SetNow(func() time.Time { return fixedNow })

	tokenStr, err := signer.Issue("sess-zero-epoch", "rt-0", 0, TokenAudienceInternal, 0)
	if err != nil {
		t.Fatalf("Issue() unexpected error: %v", err)
	}

	_, err = signer.ValidateRuntimeToken(tokenStr, TokenAudienceInternal)
	if err == nil {
		t.Fatal("ValidateRuntimeToken() expected error for zero epoch")
	}
}

func TestValidateRuntimeToken_rejects_wrong_audience(t *testing.T) {
	signer := NewHMACSigner("test-secret", 1*time.Hour)
	tokenStr, err := signer.Issue("sess-bad-aud", "rt-0", 1, "wrong-audience", 0)
	if err != nil {
		t.Fatalf("Issue() unexpected error: %v", err)
	}

	_, err = signer.ValidateRuntimeToken(tokenStr, TokenAudienceInternal)
	if err == nil {
		t.Fatal("ValidateRuntimeToken() expected error for wrong audience")
	}
}

func TestParser_ParseRoutingClaims(t *testing.T) {
	signer := NewHMACSigner("test-secret", 1*time.Hour)
	tokenStr, err := signer.Issue("sess-parser", "rt-3", 7, TokenAudienceInternal, 2)
	if err != nil {
		t.Fatalf("Issue() unexpected error: %v", err)
	}

	parser := NewParser()
	claims, err := parser.ParseRoutingClaims(tokenStr)
	if err != nil {
		t.Fatalf("Parser.ParseRoutingClaims() unexpected error: %v", err)
	}

	if claims.OwnerRuntimeID != "rt-3" {
		t.Fatalf("OwnerRuntimeID = %q, want rt-3", claims.OwnerRuntimeID)
	}
	if claims.OwnerEpoch != 7 {
		t.Fatalf("OwnerEpoch = %d, want 7", claims.OwnerEpoch)
	}
	if claims.SessionID != "sess-parser" {
		t.Fatalf("SessionID = %q, want sess-parser", claims.SessionID)
	}
}

func TestParser_ParseRoutingClaims_tampered_signature_accepted(t *testing.T) {
	signer := NewHMACSigner("test-secret", 1*time.Hour)
	tokenStr, err := signer.Issue("sess-tamper", "rt-4", 1, TokenAudienceInternal, 0)
	if err != nil {
		t.Fatalf("Issue() unexpected error: %v", err)
	}

	// Tamper the signature — Parser should still decode the payload
	parts := strings.SplitN(tokenStr, ".", 2)
	tamperedToken := parts[0] + "." + parts[1] + "tampered"

	parser := NewParser()
	claims, err := parser.ParseRoutingClaims(tamperedToken)
	if err != nil {
		t.Fatalf("Parser.ParseRoutingClaims() unexpected error with tampered signature: %v", err)
	}
	if claims.OwnerRuntimeID != "rt-4" {
		t.Fatalf("OwnerRuntimeID = %q, want rt-4", claims.OwnerRuntimeID)
	}
}

func TestParser_ParseRoutingClaims_expired_accepted(t *testing.T) {
	signer := NewHMACSigner("test-secret", -1*time.Second)
	tokenStr, err := signer.Issue("sess-exp-parser", "rt-8", 1, TokenAudienceInternal, 0)
	if err != nil {
		t.Fatalf("Issue() unexpected error: %v", err)
	}

	parser := NewParser()
	claims, err := parser.ParseRoutingClaims(tokenStr)
	if err != nil {
		t.Fatalf("Parser.ParseRoutingClaims() unexpected error for expired token: %v", err)
	}
	if claims.OwnerRuntimeID != "rt-8" {
		t.Fatalf("OwnerRuntimeID = %q, want rt-8", claims.OwnerRuntimeID)
	}
}
