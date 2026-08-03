package chatstream

import (
	"encoding/json"
	"sort"
	"strings"
	"testing"
	"unicode/utf8"

	"dominion/projects/game"
)

// reassemble concatenates fragment strings in Index order, mirroring the
// frontend's chunk-group reassembly (contracts/chat-stream.md §4.2). It
// sorts a copy so the assertion does not depend on the caller's slice
// ordering.
func reassemble(pieces []ChunkPiece) string {
	sorted := make([]ChunkPiece, len(pieces))
	copy(sorted, pieces)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Index < sorted[j].Index })
	var b strings.Builder
	for _, p := range sorted {
		b.WriteString(p.Fragment)
	}
	return b.String()
}

// envelopeBytes marshals a ChunkPiece to its SSE wire envelope JSON —
// the exact bytes that would follow "data: " on the wire. Used to
// assert each piece's on-wire size stays within the budget.
func envelopeBytes(p ChunkPiece) []byte {
	b, _ := json.Marshal(p)
	return b
}

// asciiBytes returns a deterministic ASCII byte slice of length n
// (cycling through printable ASCII) so test inputs are reproducible and
// do not depend on external state.
func asciiBytes(n int) []byte {
	const printable = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, n)
	for i := 0; i < n; i++ {
		b[i] = printable[i%len(printable)]
	}
	return b
}

// cjkBytes returns a byte slice of at least n bytes composed entirely of
// the 3-byte UTF-8 encoding of U+4E00 (一), so the multibyte rune
// boundaries stress R9 (fragments must not split inside a rune).
func cjkBytes(n int) []byte {
	const cjkRune = "一" // U+4E00, 3 bytes in UTF-8
	var b strings.Builder
	for b.Len() < n {
		b.WriteString(cjkRune)
	}
	return []byte(b.String())
}

func TestSerializeFrame(t *testing.T) {
	// given: a minimal content AgentFrame.
	frame := &game.AgentFrame{
		SessionId: "test-session",
		FrameId:   "test-frame",
		Sender:    game.FrameSender_FRAME_SENDER_AGENT,
		Payload: &game.AgentFrame_MessageParts{
			MessageParts: &game.MessageParts{
				Parts: []*game.MessagePart{
					{Kind: &game.MessagePart_Text{Text: &game.TextPart{Content: "hello"}}},
				},
			},
		},
	}
	// when
	got := SerializeFrame(frame)
	// then: non-empty, valid UTF-8, camelCase field names, flattened oneof.
	if len(got) == 0 {
		t.Fatal("SerializeFrame returned empty bytes for a valid frame")
	}
	if !utf8.Valid(got) {
		t.Fatalf("SerializeFrame produced invalid UTF-8: %v", got)
	}
	if !strings.Contains(string(got), `"sessionId":"test-session"`) {
		t.Errorf("expected camelCase sessionId field; got: %s", got)
	}
	if !strings.Contains(string(got), `"content":`) {
		t.Errorf("expected flattened oneof content field; got: %s", got)
	}
}

// TestChunkPayload_FitsWithinBudget covers inputs that fit a single
// event: a small payload and one whose length is exactly
// maxFragmentBytes. Both must return nil (single-event path).
func TestChunkPayload_FitsWithinBudget(t *testing.T) {
	tests := []struct {
		name  string
		bytes []byte
	}{
		{name: "small frame", bytes: []byte(`{"sessionId":"s","content":{"parts":[]}}`)},
		{name: "exactly maxFragmentBytes", bytes: asciiBytes(maxFragmentBytes)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given / when
			pieces := ChunkPayload(tt.bytes)
			// then
			if pieces != nil {
				t.Fatalf("ChunkPayload returned %d pieces; want nil (len=%d fits single event)", len(pieces), len(tt.bytes))
			}
		})
	}
}

// TestChunkPayload_FragmentsLargePayload covers inputs that exceed the
// single-event ceiling: a large ASCII payload, a CJK/multibyte payload
// (R9 UTF-8 safety), and a payload one byte over the threshold. Each
// must fragment into ≥2 pieces whose envelopes each fit the budget,
// reassemble exactly to the original, and (R9) consist of valid UTF-8.
func TestChunkPayload_FragmentsLargePayload(t *testing.T) {
	tests := []struct {
		name            string
		bytes           []byte
		wantExactPieces int // 0 means do not assert the exact count, only ≥ 2
	}{
		{name: "large ASCII frame", bytes: asciiBytes(maxFragmentBytes * 5)},
		{name: "CJK multibyte frame", bytes: cjkBytes(maxFragmentBytes * 3)},
		{name: "one byte over threshold", bytes: asciiBytes(maxFragmentBytes + 1), wantExactPieces: 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given / when
			pieces := ChunkPayload(tt.bytes)
			// then
			if pieces == nil {
				t.Fatal("ChunkPayload returned nil; want fragmented pieces")
			}
			if len(pieces) < 2 {
				t.Fatalf("got %d pieces; want ≥ 2", len(pieces))
			}
			if tt.wantExactPieces != 0 && len(pieces) != tt.wantExactPieces {
				t.Fatalf("got %d pieces; want exactly %d", len(pieces), tt.wantExactPieces)
			}

			// Every envelope must fit the budget (the on-wire size ceiling).
			for i, p := range pieces {
				if got := len(envelopeBytes(p)); got > maxFragmentBytes {
					t.Fatalf("piece %d envelope = %d bytes; want ≤ %d", i, got, maxFragmentBytes)
				}
			}

			// Structural invariants: shared GroupID, contiguous 0-based
			// indices, consistent Total equal to the piece count.
			groupID := pieces[0].GroupID
			total := pieces[0].Total
			if len(groupID) == 0 {
				t.Fatal("empty GroupID")
			}
			if total != len(pieces) {
				t.Fatalf("Total = %d; want %d", total, len(pieces))
			}
			seen := make(map[int]bool, len(pieces))
			for i, p := range pieces {
				if p.GroupID != groupID {
					t.Fatalf("piece %d GroupID = %q; want %q", i, p.GroupID, groupID)
				}
				if p.Total != total {
					t.Fatalf("piece %d Total = %d; want %d", i, p.Total, total)
				}
				if p.Index < 0 || p.Index >= total {
					t.Fatalf("piece %d Index = %d out of [0,%d)", i, p.Index, total)
				}
				if seen[p.Index] {
					t.Fatalf("piece %d duplicates Index %d", i, p.Index)
				}
				seen[p.Index] = true
			}

			// R9: every fragment must be valid UTF-8 on its own.
			for i, p := range pieces {
				if !utf8.ValidString(p.Fragment) {
					t.Fatalf("piece %d Fragment is not valid UTF-8", i)
				}
			}

			// Round-trip: reassembly reproduces the original exactly.
			if got := reassemble(pieces); got != string(tt.bytes) {
				t.Fatalf("reassembled bytes != original (got len %d; want %d)", len(got), len(tt.bytes))
			}
		})
	}
}

// TestChunkPayload_GroupIDUniquePerCall verifies two independent
// ChunkPayload calls produce distinct GroupIDs (each backed by a fresh
// crypto/rand draw) while pieces within one call share a single ID.
func TestChunkPayload_GroupIDUniquePerCall(t *testing.T) {
	// given
	data := asciiBytes(maxFragmentBytes * 2)
	// when
	p1 := ChunkPayload(data)
	p2 := ChunkPayload(data)
	// then
	if p1 == nil || p2 == nil {
		t.Fatal("expected fragmented pieces")
	}
	if p1[0].GroupID == p2[0].GroupID {
		t.Errorf("two ChunkPayload calls produced the same GroupID %q; want distinct", p1[0].GroupID)
	}
	for i := 1; i < len(p1); i++ {
		if p1[i].GroupID != p1[0].GroupID {
			t.Errorf("piece %d GroupID = %q; want %q (shared within a call)", i, p1[i].GroupID, p1[0].GroupID)
		}
	}
}
