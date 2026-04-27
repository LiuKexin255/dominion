package encoder

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"
)

const stopTimeout = 3 * time.Second

// Encoder manages an ffmpeg process that streams H.264 fragmented MP4 to stdout.
type Encoder interface {
	Start(ctx context.Context, config EncoderConfig) error
	StdoutPipe() io.Reader
	StderrPipe() io.Reader
	Stop() error
	Wait() error
	Running() bool
}

// ffmpegEncoder starts and supervises a single ffmpeg process.
type ffmpegEncoder struct {
	cmd     *exec.Cmd
	stdout  io.Reader
	stderr  io.Reader
	mu      sync.Mutex
	path    string
	done    chan struct{}
	waitErr error
}

// NewEncoder creates an ffmpeg-backed encoder using the provided resolved ffmpeg path.
func NewEncoder(ffmpegPath string) *ffmpegEncoder {
	return &ffmpegEncoder{path: ffmpegPath}
}

// Start launches ffmpeg with gdigrab input and stdout fragmented MP4 output.
func (encoder *ffmpegEncoder) Start(ctx context.Context, config EncoderConfig) error {
	encoder.mu.Lock()
	defer encoder.mu.Unlock()

	if encoder.isRunningLocked() {
		return fmt.Errorf("encoder is already running")
	}
	if err := validateConfig(config); err != nil {
		return err
	}
	args := BuildFFmpegArgs(config, encoder.path)
	cmd := exec.CommandContext(ctx, encoder.path, args[1:]...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("create ffmpeg stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("create ffmpeg stderr pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start ffmpeg: %w", err)
	}

	done := make(chan struct{})
	encoder.cmd = cmd
	encoder.stdout = stdout
	encoder.stderr = stderr
	encoder.done = done
	encoder.waitErr = nil
	go func() {
		err := cmd.Wait()
		encoder.mu.Lock()
		encoder.waitErr = err
		close(done)
		encoder.mu.Unlock()
	}()
	return nil
}

// StdoutPipe returns ffmpeg stdout reader, or nil before Start succeeds.
func (encoder *ffmpegEncoder) StdoutPipe() io.Reader {
	encoder.mu.Lock()
	defer encoder.mu.Unlock()
	return encoder.stdout
}

// StderrPipe returns ffmpeg stderr reader, or nil before Start succeeds.
func (encoder *ffmpegEncoder) StderrPipe() io.Reader {
	encoder.mu.Lock()
	defer encoder.mu.Unlock()
	return encoder.stderr
}

// Stop asks ffmpeg to exit, then kills it if it does not stop within the timeout.
func (encoder *ffmpegEncoder) Stop() error {
	encoder.mu.Lock()
	cmd := encoder.cmd
	done := encoder.done
	if cmd == nil || done == nil || !encoder.isRunningLocked() {
		encoder.mu.Unlock()
		return nil
	}
	encoder.mu.Unlock()

	// Windows may not support os.Interrupt for child processes; keep the graceful
	// path for platforms that do, then fall back to Kill after the timeout.
	_ = cmd.Process.Signal(os.Interrupt)

	select {
	case <-done:
		encoder.mu.Lock()
		defer encoder.mu.Unlock()
		return encoder.waitErr
	case <-time.After(stopTimeout):
		if err := cmd.Process.Kill(); err != nil {
			return fmt.Errorf("kill ffmpeg: %w", err)
		}
		<-done
		encoder.mu.Lock()
		defer encoder.mu.Unlock()
		return encoder.waitErr
	}
}

// Wait blocks until the ffmpeg process exits.
func (encoder *ffmpegEncoder) Wait() error {
	encoder.mu.Lock()
	done := encoder.done
	encoder.mu.Unlock()
	if done == nil {
		return nil
	}
	<-done
	encoder.mu.Lock()
	defer encoder.mu.Unlock()
	return encoder.waitErr
}

// Running reports whether the ffmpeg process is still active.
func (encoder *ffmpegEncoder) Running() bool {
	encoder.mu.Lock()
	defer encoder.mu.Unlock()
	return encoder.isRunningLocked()
}

func (encoder *ffmpegEncoder) isRunningLocked() bool {
	if encoder.cmd == nil || encoder.done == nil {
		return false
	}
	select {
	case <-encoder.done:
		return false
	default:
		return true
	}
}

func validateConfig(config EncoderConfig) error {
	config = normalizeConfig(config)
	if config.FrameRate <= 0 {
		return fmt.Errorf("frame rate must be positive")
	}
	if config.MaxWidth <= 0 {
		return fmt.Errorf("max width must be positive")
	}
	if config.MaxHeight <= 0 {
		return fmt.Errorf("max height must be positive")
	}
	return nil
}
