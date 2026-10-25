package media

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"time"
)

var (
	ErrMalwareDetected    = errors.New("malware detected")
	ErrScannerUnavailable = errors.New("malware scanner unavailable")
)

// ClamAVScanner invokes clamdscan and streams the file to the local ClamAV
// daemon. The daemon should listen only on a Unix socket or loopback interface.
type ClamAVScanner struct {
	binary  string
	timeout time.Duration
}

func NewClamAVScanner(binary string, timeout time.Duration) *ClamAVScanner {
	if binary == "" {
		binary = "clamdscan"
	}
	if timeout <= 0 {
		timeout = 2 * time.Minute
	}
	return &ClamAVScanner{binary: binary, timeout: timeout}
}

func (s *ClamAVScanner) Scan(ctx context.Context, path string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return ErrScannerUnavailable
	}
	scanCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	command := exec.CommandContext(scanCtx, s.binary, "--no-summary", "--stream", path)
	err = command.Run()
	if err == nil {
		return nil
	}
	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		switch exitError.ExitCode() {
		case 1:
			return ErrMalwareDetected
		case 2:
			return ErrScannerUnavailable
		}
	}
	if scanCtx.Err() != nil {
		return ErrScannerUnavailable
	}
	return ErrScannerUnavailable
}
