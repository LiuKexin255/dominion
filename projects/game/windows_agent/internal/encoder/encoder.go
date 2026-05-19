package encoder

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"

	"dominion/projects/game/windows_agent/internal/log"
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
	stopped bool
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
	if encoder.path == "" {
		return fmt.Errorf("ffmpeg path is empty")
	}
	args := BuildFFmpegArgs(config, encoder.path)
	cmd := exec.CommandContext(ctx, encoder.path, args[1:]...)
	setCmdHideWindow(cmd)
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
	encoder.stopped = false

	// Drain stderr to prevent pipe blocking.
	go func() {
		buf := make([]byte, 4096)
		for {
			_, readErr := stderr.Read(buf)
			if readErr != nil {
				return
			}
		}
	}()

	go func() {
		err := cmd.Wait()
		encoder.mu.Lock()
		wasStopped := encoder.stopped
		encoder.waitErr = err
		close(done)
		encoder.mu.Unlock()
		if err != nil && !wasStopped {
			log.Errorf("encoder", "ffmpeg exited with error: %v", err)
		}
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

	_ = cmd.Process.Signal(os.Interrupt)

	select {
	case <-done:
		return nil
	case <-time.After(stopTimeout):
		log.Warnf("encoder", "ffmpeg did not exit within %s, killing (pid=%d)", stopTimeout, cmd.Process.Pid)
		encoder.mu.Lock()
		encoder.stopped = true
		encoder.mu.Unlock()
		if err := cmd.Process.Kill(); err != nil {
			return fmt.Errorf("kill ffmpeg: %w", err)
		}
		<-done
		return nil
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
