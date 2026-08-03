package chatstream

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"unicode/utf8"

	"dominion/projects/game"

	protojson "google.golang.org/protobuf/encoding/protojson"
)

// maxFragmentBytes is the per-SSE-event byte ceiling. Chromium buffers
// each SSE event before dispatch and silently drops events that exceed
// its internal buffer (research.md R-002), so a serialized TeamFrame
// JSON larger than this MUST be split into a chunk group
// (contracts/chat-stream.md §4.2).
const maxFragmentBytes = 48 * 1024

// chunkEnvelopeProbe is a synthetic Index/Total value whose decimal
// digit count is no smaller than any real fragment's. Probing the
// envelope with it sizes the fragment budget pessimistically: a
// fragment that fits the probe envelope also fits the final, smaller
// one assigned later (the real Index/Total are at most three digits
// for a 6.7 MiB payload → ~145 fragments).
const chunkEnvelopeProbe = 9999999999

// ChunkPiece is one fragment of a chunked TeamFrame event. The SSE
// handler emits each piece as a separate "event: chunk" line; the
// frontend concatenates Fragment in Index order within a GroupID to
// rebuild the original serialized JSON (contracts/chat-stream.md §4.2).
// Every piece returned by a single ChunkPayload call shares one
// GroupID and carries a unique Index in [0, Total).
//
// Fragment is a string (C1): Go's encoding/json base64-encodes []byte,
// which would force the frontend to base64-decode before concatenating.
// A string emits a JSON string the frontend can concat and JSON.parse
// directly. The json tags produce the wire shape:
//
//	{"groupId":"...","index":0,"total":3,"fragment":"..."}
type ChunkPiece struct {
	GroupID  string `json:"groupId"`
	Index    int    `json:"index"`
	Total    int    `json:"total"`
	Fragment string `json:"fragment"`
}

// SerializeFrame serializes frame to the same camelCase JSON the
// frontend decodes — the output of protojson with EmitUnpopulated
// disabled, mirroring app.go's frameToMap and internal/api/client.go's
// serializer. Returns the raw JSON bytes, or nil if protojson fails (a
// well-formed TeamFrame never fails). The caller emits the bytes as a
// single "chat" event or, when they exceed maxFragmentBytes, fragments
// them via ChunkPayload.
func SerializeFrame(frame *game.TeamFrame) []byte {
	jsonBytes, err := (protojson.MarshalOptions{EmitUnpopulated: false}).Marshal(frame)
	if err != nil {
		return nil
	}
	return jsonBytes
}

// ChunkPayload splits serialized TeamFrame JSON into UTF-8-safe
// fragments whose chunk envelopes each marshal to at most
// maxFragmentBytes. If jsonBytes fits within maxFragmentBytes it
// returns nil — the caller emits it as a single "chat" event
// (contracts/chat-stream.md §4.1). Otherwise the bytes are split into
// a chunk group: each fragment starts and ends on a complete UTF-8
// rune boundary (R9 — never mid-multibyte), and every fragment's
// envelope (groupId, index, total, fragment) marshals to at most
// maxFragmentBytes. JSON string escaping is accounted for exactly by
// marshaling the real envelope and shrinking rune-by-rune until it
// fits — a fragment full of quotes/backslashes/control characters can
// expand up to 6× under json.Marshal, which no static budget can
// predict.
func ChunkPayload(jsonBytes []byte) []ChunkPiece {
	if len(jsonBytes) <= maxFragmentBytes {
		return nil
	}
	groupID, err := newGroupID()
	if err != nil {
		// crypto/rand should not fail on a healthy host; rather than
		// drop the event entirely, give up on chunking. The caller will
		// emit an oversized event, which is degraded but not lossy.
		return nil
	}

	// Total is unknown until slicing completes, so the fit check probes
	// with worst-case Index/Total digits (chunkEnvelopeProbe): any
	// fragment that fits the probe also fits the final, smaller envelope.
	var fragments []string
	for start := 0; start < len(jsonBytes); {
		frag, next := takeFragment(jsonBytes, start, groupID)
		fragments = append(fragments, frag)
		if next <= start {
			break // defensive: takeFragment always advances, so unreachable
		}
		start = next
	}

	total := len(fragments)
	pieces := make([]ChunkPiece, total)
	for i, frag := range fragments {
		pieces[i] = ChunkPiece{
			GroupID:  groupID,
			Index:    i,
			Total:    total,
			Fragment: frag,
		}
	}
	return pieces
}

// takeFragment returns the largest rune-safe prefix of jsonBytes[start:]
// whose chunk envelope marshals to at most maxFragmentBytes. The
// envelope is probed with worst-case Index/Total digit counts so the
// caller's later assignment of real (smaller) indices never grows the
// envelope past the budget. Returns (fragment, nextStart) where
// nextStart is the byte offset immediately after the fragment; it is
// always greater than start whenever start < len(jsonBytes).
func takeFragment(jsonBytes []byte, start int, groupID string) (string, int) {
	// Probe the per-fragment overhead (the whole envelope minus the
	// fragment content) using worst-case digits and an empty fragment.
	// A non-empty fragment of N escaped bytes makes the envelope exactly
	// len(probe) + N longer, so the byte budget for raw fragment bytes is
	// maxFragmentBytes - len(probe).
	probe, _ := json.Marshal(ChunkPiece{
		GroupID: groupID, Index: chunkEnvelopeProbe, Total: chunkEnvelopeProbe,
	})
	budget := maxFragmentBytes - len(probe)
	if budget < 1 {
		budget = 1
	}

	end := start + budget
	if end >= len(jsonBytes) {
		end = len(jsonBytes)
	}
	// R9 UTF-8 safety: walk end back to the nearest rune boundary so the
	// fragment never starts or ends inside a multibyte sequence.
	for end < len(jsonBytes) && !utf8.RuneStart(jsonBytes[end]) {
		end--
	}
	// Guarantee forward progress: if the budget is too tight to clear
	// even one rune (impossible at 48 KiB), advance by exactly one rune.
	if end == start && start < len(jsonBytes) {
		_, size := utf8.DecodeRune(jsonBytes[start:])
		end = start + size
	}

	// Shrink rune-by-rune until the marshaled envelope fits. This is
	// where JSON string escaping is accounted for exactly: content that
	// expands under escaping is detected by the real marshal and trimmed
	// one rune at a time. The loop stops at one rune to guarantee forward
	// progress (R9: a fragment may exceed the budget only when a single
	// rune's envelope does, which cannot happen at 48 KiB).
	for {
		frag := string(jsonBytes[start:end])
		envBytes, _ := json.Marshal(ChunkPiece{
			GroupID:  groupID,
			Index:    chunkEnvelopeProbe,
			Total:    chunkEnvelopeProbe,
			Fragment: frag,
		})
		if len(envBytes) <= maxFragmentBytes {
			return frag, end
		}
		_, size := utf8.DecodeLastRune(jsonBytes[start:end])
		if size == 0 || end-size <= start {
			return frag, end
		}
		end -= size
	}
}

// newGroupID returns a 16-character hex string from 8 random bytes,
// unique per chunk group and opaque to the frontend.
func newGroupID() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
