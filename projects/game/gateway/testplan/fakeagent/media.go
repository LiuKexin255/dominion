package fakeagent

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"net/url"
	"os"
	"strings"
	"time"

	"dominion/common/gopkg/s3"

	"github.com/Eyevinn/mp4ff/avc"
	"github.com/Eyevinn/mp4ff/mp4"
	"github.com/minio/minio-go/v7"
)

const (
	s3DownloadTimeout = 30 * time.Second
)

// mediaData holds parsed video segments ready for streaming.
type mediaData struct {
	initSegment      []byte
	mediaSegs        [][]byte
	randomAccessMask []bool
	initID           string // sha256 hex of initSegment
}

// prepareMediaData selects the media source by priority:
// 1. S3 download (videoURL)
// 2. Local file (videoFile)
// 3. Synthetic generation
func prepareMediaData(videoFile, videoURL string) *mediaData {
	if videoURL != "" {
		return downloadAndParseVideo(videoURL)
	}
	if videoFile != "" {
		return parseVideoFile(videoFile)
	}
	return generateFakeMedia()
}

func downloadAndParseVideo(s3URL string) *mediaData {
	bucket, key, err := parseS3URL(s3URL)
	if err != nil {
		log.Fatalf("parse S3 URL %q: %v", s3URL, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), s3DownloadTimeout)
	defer cancel()

	client, err := s3.NewS3Client(
		s3.WithAccessKey("readonly"),
		s3.WithSecretKey("Kx7mNpQ3sT6vW9yB2dF5gH8jL0rU4XcZ"),
	)
	if err != nil {
		log.Fatalf("create S3 client: %v", err)
	}

	obj, err := client.GetObject(ctx, bucket, key, minio.GetObjectOptions{})
	if err != nil {
		log.Fatalf("download s3://%s/%s: %v", bucket, key, err)
	}
	defer obj.Close()

	data, err := io.ReadAll(obj)
	if err != nil {
		log.Fatalf("read video data: %v", err)
	}

	log.Printf("downloaded video from s3://%s/%s (%d bytes)", bucket, key, len(data))
	return parseVideoBytes(data)
}

func parseS3URL(rawURL string) (bucket, key string, err error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", "", fmt.Errorf("parse URL: %w", err)
	}
	if u.Scheme != "s3" {
		return "", "", fmt.Errorf("expected s3:// scheme, got %s://", u.Scheme)
	}
	path := strings.TrimPrefix(u.Path, "/")
	if path == "" {
		return "", "", fmt.Errorf("S3 URL must contain bucket and key")
	}
	// Strip the "buckets/" path prefix used by SeaweedFS S3 gateway.
	path = strings.TrimPrefix(path, "buckets/")
	if path == "" {
		return "", "", fmt.Errorf("S3 URL must contain bucket name after buckets/")
	}
	idx := strings.Index(path, "/")
	if idx < 0 {
		return "", "", fmt.Errorf("S3 URL must contain object key after bucket")
	}
	return path[:idx], path[idx+1:], nil
}

func parseVideoFile(path string) *mediaData {
	f, err := os.Open(path)
	if err != nil {
		log.Fatalf("open video file: %v", err)
	}
	defer f.Close()

	data, err := io.ReadAll(f)
	if err != nil {
		log.Fatalf("read video file: %v", err)
	}

	return parseVideoBytes(data)
}

func parseVideoBytes(data []byte) *mediaData {
	reader := bytes.NewReader(data)
	file, err := mp4.DecodeFile(reader)
	if err != nil {
		log.Fatalf("decode video data: %v", err)
	}

	if !file.IsFragmented() {
		log.Fatalf("video is not fragmented MP4; expected fMP4 format")
	}
	return parseFragmented(file)
}

func parseFragmented(file *mp4.File) *mediaData {
	result := &mediaData{}

	if file.Ftyp != nil {
		var buf bytes.Buffer
		if err := file.Ftyp.Encode(&buf); err != nil {
			log.Fatalf("encode ftyp: %v", err)
		}
		result.initSegment = buf.Bytes()
	}
	if file.Moov != nil {
		var buf bytes.Buffer
		if err := file.Moov.Encode(&buf); err != nil {
			log.Fatalf("encode moov: %v", err)
		}
		result.initSegment = append(result.initSegment, buf.Bytes()...)
	}

	var trex *mp4.TrexBox
	if file.Moov != nil && file.Moov.Mvex != nil {
		trex = file.Moov.Mvex.Trex
	}

	for _, seg := range file.Segments {
		for _, frag := range seg.Fragments {
			var segBuf []byte

			if frag.Moof != nil {
				var moofBuf bytes.Buffer
				if err := frag.Moof.Encode(&moofBuf); err != nil {
					log.Fatalf("encode moof: %v", err)
				}
				segBuf = append(segBuf, moofBuf.Bytes()...)
			}
			if frag.Mdat != nil {
				var mdatBuf bytes.Buffer
				if err := frag.Mdat.Encode(&mdatBuf); err != nil {
					log.Fatalf("encode mdat: %v", err)
				}
				segBuf = append(segBuf, mdatBuf.Bytes()...)
			}

			if len(segBuf) > 0 {
				isRandomAccess := detectRandomAccess(trex, frag)
				result.mediaSegs = append(result.mediaSegs, segBuf)
				result.randomAccessMask = append(result.randomAccessMask, isRandomAccess)
			}
		}
	}

	if len(result.mediaSegs) == 0 {
		log.Fatalf("no media segments found in fragmented video data")
	}

	result.initID = fmt.Sprintf("%x", sha256.Sum256(result.initSegment))

	return result
}

// detectRandomAccess determines whether the first sample in a fragment is a sync
// sample (random access point) by inspecting the sample flags from the fMP4 trun box.
// The sample is random-access only when mp4ff resolves it as a sync sample
// after applying trun/tfhd/trex default sample flags.
func detectRandomAccess(trex *mp4.TrexBox, frag *mp4.Fragment) bool {
	if trex == nil {
		return false
	}
	samples, err := frag.GetFullSamples(trex)
	if err != nil || len(samples) == 0 {
		return false
	}
	if !samples[0].IsSync() {
		return false
	}
	nalus, err := avc.GetNalusFromSample(samples[0].Data)
	if err != nil {
		return false
	}
	return containsIDRNALU(nalus)
}

func containsIDRNALU(nalus [][]byte) bool {
	for _, nalu := range nalus {
		if len(nalu) > 0 && avc.GetNaluType(nalu[0]) == avc.NALU_IDR {
			return true
		}
	}
	return false
}

func generateFakeMedia() *mediaData {
	initSeg := generateFakeInitSegment()
	return &mediaData{
		initSegment: initSeg,
		mediaSegs: [][]byte{
			generateFakeMediaSegment(0),
			generateFakeMediaSegment(1),
			generateFakeMediaSegment(2),
		},
		randomAccessMask: []bool{true, false, false},
		initID:           fmt.Sprintf("%x", sha256.Sum256(initSeg)),
	}
}

func generateFakeInitSegment() []byte {
	ftyp := make([]byte, 20)
	binary.BigEndian.PutUint32(ftyp[0:4], 20)
	copy(ftyp[4:8], "ftyp")
	copy(ftyp[8:12], "isom")
	binary.BigEndian.PutUint32(ftyp[12:16], 0x200)
	copy(ftyp[16:20], "isom")

	moov := make([]byte, 108)
	binary.BigEndian.PutUint32(moov[0:4], 108)
	copy(moov[4:8], "moov")
	binary.BigEndian.PutUint32(moov[8:12], 100)
	copy(moov[12:16], "mvhd")

	return append(ftyp, moov...)
}

func generateFakeMediaSegment(seqNum int) []byte {
	moof := make([]byte, 32)
	binary.BigEndian.PutUint32(moof[0:4], 32)
	copy(moof[4:8], "moof")
	binary.BigEndian.PutUint32(moof[8:12], 16)
	copy(moof[12:16], "mfhd")
	binary.BigEndian.PutUint32(moof[24:28], uint32(seqNum))

	payload := []byte(fmt.Sprintf("fake-video-frame-%d", seqNum))
	mdat := make([]byte, 8+len(payload))
	binary.BigEndian.PutUint32(mdat[0:4], uint32(8+len(payload)))
	copy(mdat[4:8], "mdat")
	copy(mdat[8:], payload)

	return append(moof, mdat...)
}
