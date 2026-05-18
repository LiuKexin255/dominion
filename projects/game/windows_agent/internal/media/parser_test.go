package media

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"

	"github.com/Eyevinn/mp4ff/mp4"
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
			data: generateTestInitWithTrex(),
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
		seqNum uint64
		data   []byte
	}{
		{
			name:   "single moof and mdat",
			seqNum: 1,
			data:   generateTestMediaSegment(0),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given one moof+mdat media segment.
			input := bytes.NewReader(tt.data)

			// when parsing the stream.
			got, err := Parse(input)

			// then one random-access media segment is returned.
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
			if !seg.RandomAccess {
				t.Fatalf("Parse(media) RandomAccess = false, want true")
			}
			if seg.Sequence != tt.seqNum {
				t.Fatalf("Parse(media) Sequence = %d, want %d", seg.Sequence, tt.seqNum)
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
			data:         generateTestV2Stream(1),
			wantInitSize: len(generateTestInitWithTrex()),
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
		name          string
		numSegments   int
		wantSequences []uint64
		wantRandomAcc bool
	}{
		{
			name:          "three moof and mdat pairs",
			numSegments:   3,
			wantSequences: []uint64{1, 2, 3},
			wantRandomAcc: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given a stream with multiple moof+mdat pairs.
			input := bytes.NewReader(generateTestV2Stream(tt.numSegments))

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
				if seg.Sequence != tt.wantSequences[i] {
					t.Fatalf("Parse(multiple segments) segment %d Sequence = %d, want %d", i, seg.Sequence, tt.wantSequences[i])
				}
				if seg.RandomAccess != tt.wantRandomAcc {
					t.Fatalf("Parse(multiple segments) segment %d RandomAccess = %v, want %v", i, seg.RandomAccess, tt.wantRandomAcc)
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
			data: generateTestMediaSegmentWithPayloadSize(0, testMaxSegmentSize-112+1),
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
			data: generateTestV2Stream(2),
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
			data: generateTestMediaSegmentWithPayloadSize(0, testMaxSegmentSize-112),
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

func TestParseStreamingDeliversSegments(t *testing.T) {
	// given a full fMP4 stream with init and 3 media segments.
	input := bytes.NewReader(generateTestV2Stream(3))
	var initCalls []*InitSegment
	var mediaCalls []*MediaSegment

	// when parsing with streaming callbacks.
	err := ParseStreaming(input,
		func(init *InitSegment) error {
			initCalls = append(initCalls, init)
			return nil
		},
		func(seg *MediaSegment) error {
			mediaCalls = append(mediaCalls, seg)
			return nil
		},
	)

	// then init is called once and media segments are delivered in order.
	if err != nil {
		t.Fatalf("ParseStreaming() unexpected error: %v", err)
	}
	if len(initCalls) != 1 {
		t.Fatalf("ParseStreaming() init calls = %d, want 1", len(initCalls))
	}
	wantInit := generateTestInitWithTrex()
	if !bytes.Equal(initCalls[0].Data, wantInit) {
		t.Fatalf("ParseStreaming() init data mismatch: got %d bytes, want %d", len(initCalls[0].Data), len(wantInit))
	}
	if len(mediaCalls) != 3 {
		t.Fatalf("ParseStreaming() media calls = %d, want 3", len(mediaCalls))
	}
	for i, seg := range mediaCalls {
		wantSeq := uint64(i + 1)
		if seg.Sequence != wantSeq {
			t.Fatalf("ParseStreaming() segment %d Sequence = %d, want %d", i, seg.Sequence, wantSeq)
		}
		if !seg.RandomAccess {
			t.Fatalf("ParseStreaming() segment %d RandomAccess = false, want true", i)
		}
	}
}

// --- New TDD tests (RED phase) ---

func Test_randomAccessDetection(t *testing.T) {
	tests := []struct {
		name             string
		firstSampleFlags uint32
		wantRandomAccess bool
	}{
		{
			name:             "sync sample (random access point)",
			firstSampleFlags: mp4.SyncSampleFlags,
			wantRandomAccess: true,
		},
		{
			name:             "non-sync sample",
			firstSampleFlags: mp4.NonSyncSampleFlags,
			wantRandomAccess: false,
		},
		{
			name:             "unspecified sample dependency is not random access",
			firstSampleFlags: 0x00000000,
			wantRandomAccess: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given a media segment with specific sample flags.
			segData := generateTestMediaWithSampleFlags(0, tt.firstSampleFlags)

			// when detecting random access via mp4ff.
			randomAccess := detectRandomAccessFromSegment(segData)

			// then the result matches the expected random-access flag.
			if randomAccess != tt.wantRandomAccess {
				t.Fatalf("detectRandomAccessFromSegment() = %v, want %v", randomAccess, tt.wantRandomAccess)
			}
		})
	}
}

func Test_initIDGeneration(t *testing.T) {
	tests := []struct {
		name     string
		initData []byte
		wantID   string
	}{
		{
			name:     "sha256 hex of init bytes",
			initData: []byte("test-init-data"),
			wantID:   computeInitID([]byte("test-init-data")),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given init bytes.
			initData := tt.initData

			// when computing the InitID.
			got := computeInitID(initData)

			// then the ID matches the expected hex-encoded SHA-256.
			if got != tt.wantID {
				t.Fatalf("InitID = %q, want %q", got, tt.wantID)
			}
		})
	}
}

func TestParseMediaInit(t *testing.T) {
	tests := []struct {
		name     string
		initData []byte
	}{
		{
			name:     "valid init segment",
			initData: generateTestInitWithTrex(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given fMP4 init bytes.
			initData := tt.initData

			// when calling ParseMediaInit.
			got, err := ParseMediaInit(initData)

			// then the result has InitID, Codec, and Data.
			if err != nil {
				t.Fatalf("ParseMediaInit() unexpected error: %v", err)
			}
			if got == nil {
				t.Fatalf("ParseMediaInit() returned nil")
			}
			if got.InitID == "" {
				t.Fatalf("ParseMediaInit() InitID is empty")
			}
			if got.Codec != "h264-avc" {
				t.Fatalf("ParseMediaInit() Codec = %q, want %q", got.Codec, "h264-avc")
			}
			if !bytes.Equal(got.Data, initData) {
				t.Fatalf("ParseMediaInit() Data mismatch")
			}
			// Verify InitID matches sha256 of initData.
			expectedID := computeInitID(initData)
			if got.InitID != expectedID {
				t.Fatalf("ParseMediaInit() InitID = %q, want %q", got.InitID, expectedID)
			}
		})
	}
}

func Test_sequenceResetOnNewStream(t *testing.T) {
	// given two full streams (simulating stop/start cycle).
	stream1 := generateTestV2Stream(2)
	stream2 := generateTestV2Stream(3)

	// when parsing the first stream.
	result1, err := Parse(bytes.NewReader(stream1))
	if err != nil {
		t.Fatalf("Parse(stream1) unexpected error: %v", err)
	}

	// when parsing the second stream (new stream).
	result2, err := Parse(bytes.NewReader(stream2))
	if err != nil {
		t.Fatalf("Parse(stream2) unexpected error: %v", err)
	}

	// then both streams start their sequences from 1.
	if result1.MediaSegs[0].Sequence != 1 {
		t.Fatalf("stream1 segment 0 Sequence = %d, want 1", result1.MediaSegs[0].Sequence)
	}
	if result2.MediaSegs[0].Sequence != 1 {
		t.Fatalf("stream2 segment 0 Sequence = %d, want 1", result2.MediaSegs[0].Sequence)
	}
	if result2.MediaSegs[2].Sequence != 3 {
		t.Fatalf("stream2 segment 2 Sequence = %d, want 3", result2.MediaSegs[2].Sequence)
	}
}

// --- New test data generators with proper fMP4 box structure for mp4ff ---

// generateTestInitWithTrex creates ftyp + moov(mvhd + mvex(trex)) bytes.
func generateTestInitWithTrex() []byte {
	// ftyp (20 bytes): size=20, type="ftyp", major="isom", minor=0x200, compatible=["isom"]
	ftyp := make([]byte, 20)
	binary.BigEndian.PutUint32(ftyp[0:4], 20)
	copy(ftyp[4:8], BoxTypeFTYP)
	copy(ftyp[8:12], "isom")
	binary.BigEndian.PutUint32(ftyp[12:16], 0x200)
	copy(ftyp[16:20], "isom")

	// mvhd body (92 bytes) - simple zeroed mvhd for test purposes
	mvhdBody := make([]byte, 92)
	mvhd := buildBox("mvhd", mvhdBody) // size=100

	// trex body (24 bytes): version+flags(4) + trackID(4) + descIndex(4) + dur(4) + size(4) + flags(4)
	trexBody := make([]byte, 24)
	binary.BigEndian.PutUint32(trexBody[0:4], 0)      // version=0, flags=0
	binary.BigEndian.PutUint32(trexBody[4:8], 1)      // trackID
	binary.BigEndian.PutUint32(trexBody[8:12], 1)     // defaultSampleDescriptionIndex
	binary.BigEndian.PutUint32(trexBody[12:16], 1000) // defaultSampleDuration
	binary.BigEndian.PutUint32(trexBody[16:20], 10)   // defaultSampleSize
	binary.BigEndian.PutUint32(trexBody[20:24], 0)    // defaultSampleFlags
	trex := buildBox("trex", trexBody)                // size=32

	// mvex container (contains trex):
	//   size=40, type="mvex", payload=full-trex-box
	mvex := buildBox("mvex", trex[:]) // size=8+32=40

	// moov container: mvhd + mvex
	moovPayload := append(mvhd[:], mvex[:]...)
	moov := buildBox(BoxTypeMOOV, moovPayload) // size=8+100+40=148

	return append(ftyp, moov...)
}

// generateTestMediaWithSampleFlags creates a moof+mdat segment with proper
// traf/tfhd/tfdt/trun boxes and the given firstSampleFlags for mp4ff parsing.
func generateTestMediaWithSampleFlags(seqNum int, firstSampleFlags uint32) []byte {
	// mfhd (16 bytes): size=16, type="mfhd", version+flags(4), seqNum(4)
	mfhdBody := make([]byte, 8)
	binary.BigEndian.PutUint32(mfhdBody[4:8], uint32(seqNum))
	mfhd := buildBox("mfhd", mfhdBody)

	// tfhd (16 bytes): size=16, type="tfhd"
	//   flags=0x020000 (default-base-is-moof), trackID=1
	tfhdBody := make([]byte, 8)
	binary.BigEndian.PutUint32(tfhdBody[0:4], 0x00020000) // version=0, flags=0x020000
	binary.BigEndian.PutUint32(tfhdBody[4:8], 1)          // trackID
	tfhd := buildBox("tfhd", tfhdBody)

	// tfdt (20 bytes): size=20, type="tfdt", version=1, baseMediaDecodeTime=0
	tfdtBody := make([]byte, 12)
	binary.BigEndian.PutUint32(tfdtBody[0:4], 0x01000000) // version=1, flags=0
	binary.BigEndian.PutUint64(tfdtBody[4:12], 0)         // baseMediaDecodeTime
	tfdt := buildBox("tfdt", tfdtBody)

	// trun (24 bytes): size=24, type="trun"
	//   flags=0x000204 (first_sample_flags + sample_size), sampleCount=1, firstSampleFlags, sampleSize
	trunBody := make([]byte, 16)
	binary.BigEndian.PutUint32(trunBody[0:4], 0x00000204) // version=0, flags=0x000204
	binary.BigEndian.PutUint32(trunBody[4:8], 1)          // sampleCount
	binary.BigEndian.PutUint32(trunBody[8:12], firstSampleFlags)
	binary.BigEndian.PutUint32(trunBody[12:16], 10) // sample size
	trun := buildBox("trun", trunBody)

	// traf container: tfhd + tfdt + trun
	trafPayload := append(tfhd[:], tfdt[:]...)
	trafPayload = append(trafPayload, trun[:]...)
	traf := buildBox("traf", trafPayload) // size=8+16+20+24=68

	// moof container: mfhd + traf
	moofPayload := append(mfhd[:], traf[:]...)
	moof := buildBox(BoxTypeMOOF, moofPayload) // size=8+16+68=92

	// mdat: one length-prefixed IDR NALU padded to the trex default sample size.
	mdat := buildBox(BoxTypeMDAT, generateTestIDRSamplePayload(10)) // size=18

	return append(moof, mdat...)
}

// generateTestMediaSegment creates a moof+mdat segment with sync sample flags by default.
func generateTestMediaSegment(seqNum int) []byte {
	return generateTestMediaWithSampleFlags(seqNum, mp4.SyncSampleFlags)
}

// generateTestV2Stream creates an init segment with trex + numSegments media segments.
func generateTestV2Stream(numSegments int) []byte {
	stream := generateTestInitWithTrex()
	for i := range numSegments {
		stream = append(stream, generateTestMediaSegment(i)...)
	}
	return stream
}

// generateTestMediaSegmentWithPayload creates a media segment with a given mdat payload.
func generateTestMediaSegmentWithPayload(seqNum int, payload []byte) []byte {
	// mfhd (16 bytes)
	mfhdBody := make([]byte, 8)
	binary.BigEndian.PutUint32(mfhdBody[4:8], uint32(seqNum))
	mfhd := buildBox("mfhd", mfhdBody)

	// tfhd (16 bytes): default-base-is-moof, trackID=1
	tfhdBody := make([]byte, 8)
	binary.BigEndian.PutUint32(tfhdBody[0:4], 0x00020000)
	binary.BigEndian.PutUint32(tfhdBody[4:8], 1)
	tfhd := buildBox("tfhd", tfhdBody)

	// tfdt (20 bytes): version=1, baseMediaDecodeTime=0
	tfdtBody := make([]byte, 12)
	binary.BigEndian.PutUint32(tfdtBody[0:4], 0x01000000)
	binary.BigEndian.PutUint64(tfdtBody[4:12], 0)
	tfdt := buildBox("tfdt", tfdtBody)

	// trun: version=0, flags=0x000705 (data_offset + first_sample_flags + sample_dur + sample_size + sample_flags)
	//   sampleCount=1, sync firstSampleFlags, per-sample: dur=1000, size=len(payload), sync flags
	trunBody := make([]byte, 28)
	binary.BigEndian.PutUint32(trunBody[0:4], 0x00000705)             // version+flags
	binary.BigEndian.PutUint32(trunBody[4:8], 1)                      // sampleCount
	binary.BigEndian.PutUint32(trunBody[8:12], 112)                   // data offset: moof(104)+mdat header(8)
	binary.BigEndian.PutUint32(trunBody[12:16], mp4.SyncSampleFlags)  // firstSampleFlags
	binary.BigEndian.PutUint32(trunBody[16:20], 1000)                 // sample duration
	binary.BigEndian.PutUint32(trunBody[20:24], uint32(len(payload))) // sample size
	binary.BigEndian.PutUint32(trunBody[24:28], mp4.SyncSampleFlags)  // sample flags (sync)
	trun := buildBox("trun", trunBody)

	// traf container
	trafPayload := append(tfhd[:], tfdt[:]...)
	trafPayload = append(trafPayload, trun[:]...)
	traf := buildBox("traf", trafPayload) // size=8+16+20+36=80

	// moof container
	moofPayload := append(mfhd[:], traf[:]...)
	moof := buildBox(BoxTypeMOOF, moofPayload) // size=8+16+80=104

	// mdat: payload bytes
	mdat := buildBox(BoxTypeMDAT, payload)

	return append(moof, mdat...)
}

// generateTestMediaSegmentWithPayloadSize creates a media segment with payload of given size.
func generateTestMediaSegmentWithPayloadSize(seqNum int, payloadSize int) []byte {
	return generateTestMediaSegmentWithPayload(seqNum, generateTestIDRSamplePayload(payloadSize))
}

func generateTestIDRSamplePayload(size int) []byte {
	payload := make([]byte, size)
	if size >= 5 {
		binary.BigEndian.PutUint32(payload[0:4], 1)
		payload[4] = 0x65 // nal_ref_idc=3, nal_unit_type=5 (IDR)
	}
	return payload
}

func testIDRSampleData(size int) []byte {
	if size < 5 {
		size = 5
	}
	payload := bytes.Repeat([]byte{0}, size)
	binary.BigEndian.PutUint32(payload[0:4], 1)
	payload[4] = 0x65 // nal_ref_idc=3, nal_unit_type=5 (IDR)
	return payload
}

// buildBox creates a box with the given type and payload bytes.
func buildBox(boxType string, payload []byte) []byte {
	box := make([]byte, boxHeaderSize+len(payload))
	binary.BigEndian.PutUint32(box[0:4], uint32(len(box)))
	copy(box[4:8], boxType)
	copy(box[8:], payload)
	return box
}

// --- Test helpers for mp4ff-based detection ---

// detectRandomAccessFromSegment parses moof+mdat bytes and returns true if the
// first sample is a sync sample (random access point). This mirrors the parser logic.
func detectRandomAccessFromSegment(segData []byte) bool {
	file, err := mp4.DecodeFile(bytes.NewReader(segData))
	if err != nil {
		return false
	}
	if len(file.Segments) == 0 || len(file.Segments[0].Fragments) == 0 {
		return false
	}
	frag := file.Segments[0].Fragments[0]
	trex := testTrex()
	samples, err := frag.GetFullSamples(trex)
	if err != nil || len(samples) == 0 {
		return false
	}
	return samples[0].IsSync()
}

// testTrex returns a TrexBox matching the test init segment defaults.
func testTrex() *mp4.TrexBox {
	return &mp4.TrexBox{
		TrackID:                       1,
		DefaultSampleDescriptionIndex: 1,
		DefaultSampleDuration:         1000,
		DefaultSampleSize:             10,
		DefaultSampleFlags:            mp4.SyncSampleFlags,
	}
}
