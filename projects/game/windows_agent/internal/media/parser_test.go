package media

import (
	"bytes"
	"encoding/binary"
	"strconv"
	"strings"
	"testing"
)

const testMaxSegmentSize = 1 << 20

func TestParseEmpty(t *testing.T) {
	// given an empty reader.
	input := bytes.NewReader(nil)

	// when parsing the stream.
	got, err := Parse(input)

	// then the result is empty and parsing succeeds.
	if err != nil {
		t.Fatalf("Parse(empty) unexpected error: %v", err)
	}
	if got == nil {
		t.Fatalf("Parse(empty) returned nil result")
	}
	if got.InitSegment != nil {
		t.Fatalf("Parse(empty) init segment = %v, want nil", got.InitSegment)
	}
	if len(got.MediaSegs) != 0 {
		t.Fatalf("Parse(empty) media segments = %d, want 0", len(got.MediaSegs))
	}
}

func TestParseInitOnly(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{
			name: "ftyp and moov",
			data: generateTestInitSegment(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given ftyp+moov initialization data.
			input := bytes.NewReader(tt.data)

			// when parsing the stream.
			got, err := Parse(input)

			// then InitSegment is populated with no media segments.
			if err != nil {
				t.Fatalf("Parse(init) unexpected error: %v", err)
			}
			if got.InitSegment == nil {
				t.Fatalf("Parse(init) init segment is nil")
			}
			if !bytes.Equal(got.InitSegment.Data, tt.data) {
				t.Fatalf("Parse(init) init data mismatch: got %d bytes, want %d", len(got.InitSegment.Data), len(tt.data))
			}
			if len(got.MediaSegs) != 0 {
				t.Fatalf("Parse(init) media segments = %d, want 0", len(got.MediaSegs))
			}
		})
	}
}

func TestParseMediaSegment(t *testing.T) {
	tests := []struct {
		name   string
		seqNum int
		data   []byte
	}{
		{
			name:   "single moof and mdat",
			seqNum: 0,
			data:   generateTestMediaSegment(0),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given one moof+mdat media segment.
			input := bytes.NewReader(tt.data)

			// when parsing the stream.
			got, err := Parse(input)

			// then one keyframe media segment is returned.
			if err != nil {
				t.Fatalf("Parse(media) unexpected error: %v", err)
			}
			if len(got.MediaSegs) != 1 {
				t.Fatalf("Parse(media) media segments = %d, want 1", len(got.MediaSegs))
			}
			seg := got.MediaSegs[0]
			if !bytes.Equal(seg.Data, tt.data) {
				t.Fatalf("Parse(media) data mismatch: got %d bytes, want %d", len(seg.Data), len(tt.data))
			}
			if !seg.KeyFrame {
				t.Fatalf("Parse(media) KeyFrame = false, want true")
			}
			if seg.SeqNum != tt.seqNum {
				t.Fatalf("Parse(media) SeqNum = %d, want %d", seg.SeqNum, tt.seqNum)
			}
		})
	}
}

func TestParseFullStream(t *testing.T) {
	tests := []struct {
		name         string
		data         []byte
		wantInitSize int
		wantSegments int
	}{
		{
			name:         "init and one media segment",
			data:         generateTestStream(1),
			wantInitSize: len(generateTestInitSegment()),
			wantSegments: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given a full fMP4 stream with init and media data.
			input := bytes.NewReader(tt.data)

			// when parsing the stream.
			got, err := Parse(input)

			// then both init and media segment data are returned.
			if err != nil {
				t.Fatalf("Parse(full stream) unexpected error: %v", err)
			}
			if got.InitSegment == nil {
				t.Fatalf("Parse(full stream) init segment is nil")
			}
			if len(got.InitSegment.Data) != tt.wantInitSize {
				t.Fatalf("Parse(full stream) init size = %d, want %d", len(got.InitSegment.Data), tt.wantInitSize)
			}
			if len(got.MediaSegs) != tt.wantSegments {
				t.Fatalf("Parse(full stream) media segments = %d, want %d", len(got.MediaSegs), tt.wantSegments)
			}
		})
	}
}

func TestParseMultipleSegments(t *testing.T) {
	tests := []struct {
		name         string
		numSegments  int
		wantSeqNums  []int
		wantKeyFrame bool
	}{
		{
			name:         "three moof and mdat pairs",
			numSegments:  3,
			wantSeqNums:  []int{0, 1, 2},
			wantKeyFrame: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given a stream with multiple moof+mdat pairs.
			input := bytes.NewReader(generateTestStream(tt.numSegments))

			// when parsing the stream.
			got, err := Parse(input)

			// then all media segments are returned with stream sequence numbers.
			if err != nil {
				t.Fatalf("Parse(multiple segments) unexpected error: %v", err)
			}
			if len(got.MediaSegs) != tt.numSegments {
				t.Fatalf("Parse(multiple segments) media segments = %d, want %d", len(got.MediaSegs), tt.numSegments)
			}
			for i, seg := range got.MediaSegs {
				if seg.SeqNum != tt.wantSeqNums[i] {
					t.Fatalf("Parse(multiple segments) segment %d SeqNum = %d, want %d", i, seg.SeqNum, tt.wantSeqNums[i])
				}
				if seg.KeyFrame != tt.wantKeyFrame {
					t.Fatalf("Parse(multiple segments) segment %d KeyFrame = %v, want %v", i, seg.KeyFrame, tt.wantKeyFrame)
				}
			}
		})
	}
}

func TestParseOversizeSegment(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{
			name: "media segment over one MiB",
			data: generateTestMediaSegmentWithPayloadSize(0, testMaxSegmentSize-boxHeaderSize-32+1),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given a segment larger than domain.MaxSegmentSize.
			input := bytes.NewReader(tt.data)

			// when parsing the stream.
			_, err := Parse(input)

			// then parsing fails with a size error.
			if err == nil {
				t.Fatalf("Parse(oversize) expected error")
			}
			if !strings.Contains(err.Error(), "exceeds max size") {
				t.Fatalf("Parse(oversize) error = %v, want size error", err)
			}
		})
	}
}

func TestParseTruncated(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{
			name: "truncated box header",
			data: []byte{0, 0, 0, 20},
		},
		{
			name: "truncated box body",
			data: buildBox(BoxTypeFTYP, []byte("short"))[:10],
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given truncated box bytes.
			input := bytes.NewReader(tt.data)

			// when parsing the stream.
			_, err := Parse(input)

			// then parsing fails.
			if err == nil {
				t.Fatalf("Parse(truncated) expected error")
			}
		})
	}
}

func TestParseBytes(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{
			name: "full stream bytes",
			data: generateTestStream(2),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given an fMP4 byte slice.
			input := bytes.NewReader(tt.data)

			// when parsing through Parse and ParseBytes.
			want, err := Parse(input)
			if err != nil {
				t.Fatalf("Parse(bytes) unexpected error: %v", err)
			}
			got, err := ParseBytes(tt.data)

			// then ParseBytes returns the same result shape and data.
			if err != nil {
				t.Fatalf("ParseBytes(bytes) unexpected error: %v", err)
			}
			if !bytes.Equal(got.InitSegment.Data, want.InitSegment.Data) {
				t.Fatalf("ParseBytes(bytes) init data mismatch")
			}
			if len(got.MediaSegs) != len(want.MediaSegs) {
				t.Fatalf("ParseBytes(bytes) media segments = %d, want %d", len(got.MediaSegs), len(want.MediaSegs))
			}
			for i, seg := range got.MediaSegs {
				if !bytes.Equal(seg.Data, want.MediaSegs[i].Data) {
					t.Fatalf("ParseBytes(bytes) segment %d data mismatch", i)
				}
			}
		})
	}
}

func TestParseSegmentSizeAtLimit(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{
			name: "media segment exactly one MiB",
			data: generateTestMediaSegmentWithPayloadSize(0, testMaxSegmentSize-boxHeaderSize-32),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given a media segment exactly at domain.MaxSegmentSize.
			input := bytes.NewReader(tt.data)

			// when parsing the stream.
			got, err := Parse(input)

			// then parsing succeeds and preserves the full segment.
			if err != nil {
				t.Fatalf("Parse(size at limit) unexpected error: %v", err)
			}
			if len(got.MediaSegs) != 1 {
				t.Fatalf("Parse(size at limit) media segments = %d, want 1", len(got.MediaSegs))
			}
			if len(got.MediaSegs[0].Data) != testMaxSegmentSize {
				t.Fatalf("Parse(size at limit) segment size = %d, want %d", len(got.MediaSegs[0].Data), testMaxSegmentSize)
			}
		})
	}
}

func generateTestInitSegment() []byte {
	ftyp := make([]byte, 20)
	binary.BigEndian.PutUint32(ftyp[0:4], 20)
	copy(ftyp[4:8], BoxTypeFTYP)
	copy(ftyp[8:12], "isom")
	binary.BigEndian.PutUint32(ftyp[12:16], 0x200)
	copy(ftyp[16:20], "isom")

	moov := make([]byte, 108)
	binary.BigEndian.PutUint32(moov[0:4], 108)
	copy(moov[4:8], BoxTypeMOOV)
	binary.BigEndian.PutUint32(moov[8:12], 100)
	copy(moov[12:16], "mvhd")

	return append(ftyp, moov...)
}

func generateTestMediaSegment(seqNum int) []byte {
	payload := []byte("fake-video-frame-" + strconv.Itoa(seqNum))
	return generateTestMediaSegmentWithPayload(seqNum, payload)
}

func generateTestStream(numSegments int) []byte {
	stream := generateTestInitSegment()
	for i := range numSegments {
		stream = append(stream, generateTestMediaSegment(i)...)
	}
	return stream
}

func generateTestMediaSegmentWithPayloadSize(seqNum int, payloadSize int) []byte {
	return generateTestMediaSegmentWithPayload(seqNum, bytes.Repeat([]byte{'x'}, payloadSize))
}

func generateTestMediaSegmentWithPayload(seqNum int, payload []byte) []byte {
	moof := make([]byte, 32)
	binary.BigEndian.PutUint32(moof[0:4], 32)
	copy(moof[4:8], BoxTypeMOOF)
	binary.BigEndian.PutUint32(moof[8:12], 16)
	copy(moof[12:16], "mfhd")
	binary.BigEndian.PutUint32(moof[24:28], uint32(seqNum))

	mdat := buildBox(BoxTypeMDAT, payload)

	return append(moof, mdat...)
}

func buildBox(boxType string, payload []byte) []byte {
	box := make([]byte, boxHeaderSize+len(payload))
	binary.BigEndian.PutUint32(box[0:4], uint32(len(box)))
	copy(box[4:8], boxType)
	copy(box[8:], payload)
	return box
}
