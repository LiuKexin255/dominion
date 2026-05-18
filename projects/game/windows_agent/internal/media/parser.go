// Package media provides fMP4 box parsing for the Windows Agent.
package media

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"

	"dominion/projects/game/gateway/domain"

	"github.com/Eyevinn/mp4ff/avc"
	"github.com/Eyevinn/mp4ff/mp4"
)

// Box types used by fragmented MP4 streams.
const (
	BoxTypeFTYP = "ftyp"
	BoxTypeMOOV = "moov"
	BoxTypeMOOF = "moof"
	BoxTypeMDAT = "mdat"
)

const boxHeaderSize = 8

// DefaultCodec is the codec identifier for H.264 AVC video.
const DefaultCodec = "h264-avc"

// InitSegment contains the fMP4 initialization data (ftyp + moov boxes).
type InitSegment struct {
	// StreamID identifies the media stream this init belongs to.
	StreamID string
	// InitID is the SHA-256 hex digest of the init data.
	InitID string
	// Codec identifies the codec (e.g. "h264-avc").
	Codec string
	// Data contains complete ftyp + moov bytes.
	Data []byte
}

// MediaSegment contains a single fMP4 media fragment (moof + mdat boxes).
type MediaSegment struct {
	// StreamID identifies the media stream this segment belongs to.
	StreamID string
	// InitID identifies the init segment this segment references.
	InitID string
	// Sequence is the monotonically increasing sequence number within the stream.
	Sequence uint64
	// Data contains complete moof + mdat bytes.
	Data []byte
	// RandomAccess indicates whether this segment starts from a random access point.
	RandomAccess bool
	// DurationMS is the duration of this segment in milliseconds.
	DurationMS int32
	// Discontinuity indicates a gap or discontinuity in the segment timeline.
	Discontinuity bool
}

// ParseResult holds the parsed fMP4 data.
type ParseResult struct {
	// InitSegment is the fMP4 initialization segment, if present.
	InitSegment *InitSegment
	// MediaSegs contains parsed media fragments in stream order.
	MediaSegs []*MediaSegment
}

// Parse reads an fMP4 byte stream and splits it into init and media segments.
// It reads boxes sequentially from the reader without buffering the entire
// stream. It returns an error if any segment exceeds domain.MaxSegmentSize.
func Parse(r io.Reader) (*ParseResult, error) {
	result := new(ParseResult)
	var initData []byte
	var currentMedia []byte
	seqNum := uint64(1)
	var trex *mp4.TrexBox

	for {
		box, err := readBox(r)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}

		switch box.typ {
		case BoxTypeFTYP, BoxTypeMOOV:
			if len(initData)+len(box.data) > domain.MaxSegmentSize {
				return nil, fmt.Errorf("init segment exceeds max size: %d > %d", len(initData)+len(box.data), domain.MaxSegmentSize)
			}
			initData = append(initData, box.data...)

			if box.typ == BoxTypeMOOV {
				parsedTrex := parseTrexFromInit(initData)
				trex = parsedTrex
				result.InitSegment = &InitSegment{
					Data:  append([]byte(nil), initData...),
					Codec: DefaultCodec,
				}
			} else {
				result.InitSegment = &InitSegment{Data: append([]byte(nil), initData...), Codec: DefaultCodec}
			}

		case BoxTypeMOOF:
			if len(box.data) > domain.MaxSegmentSize {
				return nil, fmt.Errorf("media segment exceeds max size: %d > %d", len(box.data), domain.MaxSegmentSize)
			}
			currentMedia = append(currentMedia[:0], box.data...)

		case BoxTypeMDAT:
			if len(currentMedia)+len(box.data) > domain.MaxSegmentSize {
				return nil, fmt.Errorf("media segment exceeds max size: %d > %d", len(currentMedia)+len(box.data), domain.MaxSegmentSize)
			}
			currentMedia = append(currentMedia, box.data...)
			segmentData := append([]byte(nil), currentMedia...)

			randomAccess, raErr := detectRandomAccess(segmentData, trex)
			if raErr != nil {
				return nil, fmt.Errorf("detect random access: %w", raErr)
			}

			result.MediaSegs = append(result.MediaSegs, &MediaSegment{
				Data:         segmentData,
				RandomAccess: randomAccess,
				Sequence:     seqNum,
			})
			seqNum++
			currentMedia = nil
		}
	}

	return result, nil
}

// ParseBytes is a convenience wrapper around Parse for byte slices.
func ParseBytes(data []byte) (*ParseResult, error) {
	return Parse(bytes.NewReader(data))
}

// ParseStreaming reads an fMP4 byte stream and invokes onInit when the init
// segment is complete (ftyp+moov) and onMedia for each media fragment
// (moof+mdat) as it arrives. It returns an error if any segment exceeds
// domain.MaxSegmentSize. Unlike Parse, it does not wait for EOF — each
// segment is delivered immediately after parsing.
func ParseStreaming(r io.Reader, onInit func(*InitSegment) error, onMedia func(*MediaSegment) error) error {
	var initData []byte
	var currentMedia []byte
	seqNum := uint64(1)
	var trex *mp4.TrexBox
	var initID string

	for {
		bx, err := readBox(r)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}

		switch bx.typ {
		case BoxTypeFTYP, BoxTypeMOOV:
			if len(initData)+len(bx.data) > domain.MaxSegmentSize {
				return fmt.Errorf("init segment exceeds max size: %d > %d", len(initData)+len(bx.data), domain.MaxSegmentSize)
			}
			initData = append(initData, bx.data...)
			if bx.typ == BoxTypeMOOV {
				parsedTrex := parseTrexFromInit(initData)
				trex = parsedTrex
				initID = computeInitID(initData)
				if err := onInit(&InitSegment{
					Data:   append([]byte(nil), initData...),
					Codec:  DefaultCodec,
					InitID: initID,
				}); err != nil {
					return err
				}
			}

		case BoxTypeMOOF:
			if len(bx.data) > domain.MaxSegmentSize {
				return fmt.Errorf("media segment exceeds max size: %d > %d", len(bx.data), domain.MaxSegmentSize)
			}
			currentMedia = append(currentMedia[:0], bx.data...)

		case BoxTypeMDAT:
			if len(currentMedia)+len(bx.data) > domain.MaxSegmentSize {
				return fmt.Errorf("media segment exceeds max size: %d > %d", len(currentMedia)+len(bx.data), domain.MaxSegmentSize)
			}
			currentMedia = append(currentMedia, bx.data...)
			segData := append([]byte(nil), currentMedia...)

			randomAccess, raErr := detectRandomAccess(segData, trex)
			if raErr != nil {
				return fmt.Errorf("detect random access: %w", raErr)
			}

			if err := onMedia(&MediaSegment{
				Data:         segData,
				RandomAccess: randomAccess,
				Sequence:     seqNum,
				InitID:       initID,
			}); err != nil {
				return err
			}
			seqNum++
			currentMedia = nil
		}
	}

	return nil
}

// ParseMediaInit creates an InitSegment from fMP4 init bytes by computing
// the SHA-256 InitID and setting the default codec.
func ParseMediaInit(initData []byte) (*InitSegment, error) {
	return &InitSegment{
		Data:   append([]byte(nil), initData...),
		InitID: computeInitID(initData),
		Codec:  DefaultCodec,
	}, nil
}

type box struct {
	typ  string
	data []byte
}

func readBox(r io.Reader) (*box, error) {
	header := make([]byte, boxHeaderSize)
	n, err := io.ReadFull(r, header)
	if errors.Is(err, io.EOF) && n == 0 {
		return nil, io.EOF
	}
	if err != nil {
		return nil, fmt.Errorf("read box header: %w", err)
	}

	size := binary.BigEndian.Uint32(header[0:4])
	if size < boxHeaderSize {
		return nil, fmt.Errorf("invalid box size %d for type %q", size, string(header[4:8]))
	}

	data := make([]byte, int(size))
	copy(data, header)
	if _, err := io.ReadFull(r, data[boxHeaderSize:]); err != nil {
		return nil, fmt.Errorf("read %s box body: %w", string(header[4:8]), err)
	}

	return &box{
		typ:  string(header[4:8]),
		data: data,
	}, nil
}

// parseTrexFromInit decodes the init bytes with mp4ff and extracts the trex
// box for default sample info. Returns nil if extraction fails.
func parseTrexFromInit(initData []byte) *mp4.TrexBox {
	file, err := mp4.DecodeFile(bytes.NewReader(initData))
	if err != nil {
		return nil
	}
	if file.Moov == nil || file.Moov.Mvex == nil {
		return nil
	}
	return file.Moov.Mvex.Trex
}

// detectRandomAccess parses the moof+mdat segment data with mp4ff and returns
// true if the first sample is a sync sample (random access point). It returns an
// error if the segment cannot be decoded or contains no samples.
func detectRandomAccess(segData []byte, trex *mp4.TrexBox) (bool, error) {
	file, err := mp4.DecodeFile(bytes.NewReader(segData))
	if err != nil {
		return false, fmt.Errorf("decode segment: %w", err)
	}
	if len(file.Segments) == 0 || len(file.Segments[0].Fragments) == 0 {
		return false, fmt.Errorf("segment contains no fragments")
	}
	frag := file.Segments[0].Fragments[0]
	samples, err := frag.GetFullSamples(trex)
	if err != nil {
		return false, fmt.Errorf("get full samples: %w", err)
	}
	if len(samples) == 0 {
		return false, fmt.Errorf("segment contains no samples")
	}
	if !samples[0].IsSync() {
		return false, nil
	}
	nalus, err := avc.GetNalusFromSample(samples[0].Data)
	if err != nil {
		return false, fmt.Errorf("get nalus from sample: %w", err)
	}
	return containsIDRNALU(nalus), nil
}

func containsIDRNALU(nalus [][]byte) bool {
	for _, nalu := range nalus {
		if len(nalu) > 0 && avc.GetNaluType(nalu[0]) == avc.NALU_IDR {
			return true
		}
	}
	return false
}

// computeInitID returns the hex-encoded SHA-256 digest of initData.
func computeInitID(initData []byte) string {
	h := sha256.Sum256(initData)
	return hex.EncodeToString(h[:])
}
