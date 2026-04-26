// Package media provides fMP4 box parsing for the Windows Agent.
package media

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"dominion/projects/game/gateway/domain"
)

// Box types used by fragmented MP4 streams.
const (
	BoxTypeFTYP = "ftyp"
	BoxTypeMOOV = "moov"
	BoxTypeMOOF = "moof"
	BoxTypeMDAT = "mdat"
)

const boxHeaderSize = 8

// InitSegment contains the fMP4 initialization data (ftyp + moov boxes).
type InitSegment struct {
	// Data contains complete ftyp + moov bytes.
	Data []byte
}

// MediaSegment contains a single fMP4 media fragment (moof + mdat boxes).
type MediaSegment struct {
	// Data contains complete moof + mdat bytes.
	Data []byte
	// KeyFrame indicates whether this segment starts with a keyframe.
	KeyFrame bool
	// SeqNum is the sequence number within the stream.
	SeqNum int
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
	seqNum := 0

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
			result.InitSegment = &InitSegment{Data: initData}

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
			result.MediaSegs = append(result.MediaSegs, &MediaSegment{
				Data:     segmentData,
				KeyFrame: true,
				SeqNum:   seqNum,
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
