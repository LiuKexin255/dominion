package mediacache

import (
	"bytes"
	"errors"
	"fmt"

	"dominion/projects/game/gateway/domain"

	"github.com/Eyevinn/hi264/pkg/decoder"
	"github.com/Eyevinn/hi264/pkg/frame"
	"github.com/Eyevinn/hi264/pkg/yuv"
	"github.com/Eyevinn/mp4ff/avc"
	"github.com/Eyevinn/mp4ff/mp4"
)

// JPEGQuality controls the output quality of extracted JPEG snapshots.
const JPEGQuality = 85

// ExtractJPEGFromSegment decodes a JPEG image from an fMP4 random access segment.
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

	nalus, err := extractIDRSampleNALUs(parsed)
	if err != nil {
		return nil, err
	}

	fr, err := decodeH264(nalus)
	if err != nil {
		return nil, fmt.Errorf("h264 decode: %w", err)
	}

	return encodeJPEG(fr)
}

func extractIDRSampleNALUs(f *mp4.File) ([][]byte, error) {
	avcC, ok := avcCBox(f)
	if !ok {
		return nil, errors.New("no AVC decoder config found")
	}

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
			for _, sample := range samples {
				nalus, err := avc.GetNalusFromSample(sample.Data)
				if err != nil {
					return nil, fmt.Errorf("get nalus from sample: %w", err)
				}
				if containsIDRNALU(nalus) {
					all := make([][]byte, 0, len(avcC.SPSnalus)+len(avcC.PPSnalus)+len(nalus))
					all = append(all, avcC.SPSnalus...)
					all = append(all, avcC.PPSnalus...)
					all = append(all, nalus...)

					return all, nil
				}
			}
		}
	}
	return nil, errors.New("no IDR sample found in fMP4 segment")
}

func avcCBox(f *mp4.File) (*mp4.AvcCBox, bool) {
	if f.Moov == nil {
		return nil, false
	}
	for _, trak := range f.Moov.Traks {
		if trak.Mdia == nil || trak.Mdia.Minf == nil || trak.Mdia.Minf.Stbl == nil || trak.Mdia.Minf.Stbl.Stsd == nil {
			continue
		}
		stsd := trak.Mdia.Minf.Stbl.Stsd
		if stsd.AvcX != nil && stsd.AvcX.AvcC != nil {
			return stsd.AvcX.AvcC, true
		}
	}
	return nil, false
}

func containsIDRNALU(nalus [][]byte) bool {
	for _, nalu := range nalus {
		if len(nalu) > 0 && avc.GetNaluType(nalu[0]) == avc.NALU_IDR {
			return true
		}
	}
	return false
}

// decodeH264 decodes one H.264 access unit into a YUV frame.
func decodeH264(nalus [][]byte) (*frame.Frame, error) {
	dec := decoder.New()
	dec.SkipDeblock = true

	return dec.DecodeNALUs(nalus)
}

// encodeJPEG encodes a decoded YUV frame to JPEG bytes.
func encodeJPEG(f *frame.Frame) ([]byte, error) {
	var buf bytes.Buffer
	if err := yuv.WriteJPEG(&buf, f, JPEGQuality); err != nil {
		return nil, fmt.Errorf("jpeg encode: %w", err)
	}
	return buf.Bytes(), nil
}
