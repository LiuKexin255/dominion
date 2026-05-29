package domain

import (
	"context"
	"testing"
)

func TestCryptoIDGenerator_NewID(t *testing.T) {
	tests := []struct {
		name  string
		count int
	}{
		{name: "generate 100 IDs should all be unique and length 32", count: 100},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			gen := &CryptoIDGenerator{}
			ctx := context.Background()
			ids := make(map[string]bool, tt.count)

			// when
			for i := 0; i < tt.count; i++ {
				id, err := gen.NewID(ctx)

				// then
				if err != nil {
					t.Fatalf("NewID() unexpected error: %v", err)
				}
				if len(id) != 32 {
					t.Fatalf("NewID() returned id of length %d, want 32: %s", len(id), id)
				}
				if ids[id] {
					t.Fatalf("NewID() returned duplicate id: %s", id)
				}
				ids[id] = true
			}
		})
	}
}

func Test_newID_replaceable(t *testing.T) {
	tests := []struct {
		name     string
		fixedID  string
		fixedErr error
	}{
		{
			name:     "replace newID with fixed function should return fixed value",
			fixedID:  "fixedid0123456789abcdef012345678",
			fixedErr: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			original := newID
			newID = func() (string, error) {
				return tt.fixedID, tt.fixedErr
			}
			defer func() { newID = original }()

			gen := &CryptoIDGenerator{}
			ctx := context.Background()

			// when
			got, err := gen.NewID(ctx)

			// then
			if err != tt.fixedErr {
				t.Fatalf("NewID() error = %v, want %v", err, tt.fixedErr)
			}
			if got != tt.fixedID {
				t.Fatalf("NewID() = %s, want %s", got, tt.fixedID)
			}
		})
	}
}
