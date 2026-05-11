package mediacache

import (
	"bytes"
	"errors"
	"fmt"

	"dominion/projects/game/gateway/domain"

	"github.com/Eyevinn/hi264/pkg/decoder"
	"github.com/Eyevinn/hi264/pkg/frame"
	"github.com/Eyevinn/hi264/pkg/yuv"
	"github.com/Eyevinn/mp4ff/mp4"
)

// JPEGQuality controls the output quality of extracted JPEG snapshots.
const JPEGQuality = 85

// ExtractJPEGFromSegment decodes a JPEG image from an fMP4 key frame segment.
// initData is the optional fMP4 init segment (ftyp+moov) which contains codec
// configuration (SPS/PPS) required for H.264 decoding. When provided, the init
// segment is prepended to the media segment so the parser has full context.
func ExtractJPEGFromSegment(initData []byte, seg *domain.SegmentRef) ([]byte, error) {
	var combined []byte
	if len(initData) > 0 {
		combined = make([]byte, 0, len(initData)+len(seg.Data))
		combined = append(combined, initData...)
		combined = append(combined, seg.Data...)
	} else {
		combined = seg.Data
	}

	parsed, err := mp4.DecodeFile(bytes.NewReader(combined))
	if err != nil {
		return nil, fmt.Errorf("mp4ff decode: %w", err)
	}

	sampleData, err := extractFirstSample(parsed)
	if err != nil {
		return nil, err
	}

	nalData := prependSPSPPS(parsed, sampleData)

	fr, err := decodeH264(nalData)
	if err != nil {
		return nil, fmt.Errorf("h264 decode: %w", err)
	}

	return encodeJPEG(fr)
}

// prependSPSPPS extracts SPS and PPS NAL units from the fMP4 moov box and
// prepends them (in AVC length-prefixed format) to the sample data so the
// H.264 decoder has the parameter sets needed to initialise.
func prependSPSPPS(f *mp4.File, sampleData []byte) []byte {
	if f.Moov == nil {
		return sampleData
	}
	for _, trak := range f.Moov.Traks {
		stsd := trak.Mdia.Minf.Stbl.Stsd
		if stsd.AvcX == nil || stsd.AvcX.AvcC == nil {
			continue
		}
		avcC := stsd.AvcX.AvcC
		buf := make([]byte, 0, len(sampleData)+len(avcC.SPSnalus)*32+len(avcC.PPSnalus)*32)
		for _, sps := range avcC.SPSnalus {
			buf = append(buf, avcLengthPrefix(uint32(len(sps)))...)
			buf = append(buf, sps...)
		}
		for _, pps := range avcC.PPSnalus {
			buf = append(buf, avcLengthPrefix(uint32(len(pps)))...)
			buf = append(buf, pps...)
		}
		buf = append(buf, sampleData...)
		return buf
	}
	return sampleData
}

func avcLengthPrefix(size uint32) []byte {
	return []byte{byte(size >> 24), byte(size >> 16), byte(size >> 8), byte(size)}
}

// extractFirstSample returns the raw bytes of the first video sample from a
// parsed fMP4 file.
func extractFirstSample(f *mp4.File) ([]byte, error) {
	var trex *mp4.TrexBox
	if f.Moov != nil && f.Moov.Mvex != nil {
		trex = f.Moov.Mvex.Trex
	}
	for _, seg := range f.Segments {
		for _, frag := range seg.Fragments {
			samples, err := frag.GetFullSamples(trex)
			if err != nil {
				return nil, fmt.Errorf("get samples: %w", err)
			}
			for _, s := range samples {
				if len(s.Data) > 0 {
					return s.Data, nil
				}
			}
		}
	}
	return nil, errors.New("no samples found in fMP4 segment")
}

// decodeH264 decodes raw H.264 NAL unit data into a YUV frame.
func decodeH264(data []byte) (*frame.Frame, error) {
	dec := decoder.New()
	dec.SkipDeblock = true

	fr, err := dec.DecodeAVC(data)
	if err != nil {
		fr, err = dec.DecodeAnnexB(data)
		if err != nil {
			return nil, fmt.Errorf("decode avc/annexb: %w", err)
		}
	}
	return fr, nil
}

// encodeJPEG encodes a decoded YUV frame to JPEG bytes.
func encodeJPEG(f *frame.Frame) ([]byte, error) {
	var buf bytes.Buffer
	if err := yuv.WriteJPEG(&buf, f, JPEGQuality); err != nil {
		return nil, fmt.Errorf("jpeg encode: %w", err)
	}
	return buf.Bytes(), nil
}
