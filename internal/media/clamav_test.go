package media

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestClamAVScannerMapsCleanInfectedAndUnavailableResults(t *testing.T) {
	file := filepath.Join(t.TempDir(), "upload.bin")
	if err := os.WriteFile(file, []byte("test payload"), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		name     string
		exitCode int
		wantErr  error
	}{
		{name: "clean", exitCode: 0},
		{name: "infected", exitCode: 1, wantErr: ErrMalwareDetected},
		{name: "scanner error", exitCode: 2, wantErr: ErrScannerUnavailable},
	} {
		t.Run(test.name, func(t *testing.T) {
			binary := writeFakeClamAV(t, test.exitCode)
			scanner := NewClamAVScanner(binary, time.Second)
			err := scanner.Scan(context.Background(), file)
			if test.wantErr == nil && err != nil {
				t.Fatalf("Scan() error = %v, want nil", err)
			}
			if test.wantErr != nil && err != test.wantErr {
				t.Fatalf("Scan() error = %v, want %v", err, test.wantErr)
			}
		})
	}
}

func TestClamAVScannerRejectsMissingFileAndHonorsCancellation(t *testing.T) {
	binary := writeFakeClamAV(t, 0)
	scanner := NewClamAVScanner(binary, time.Second)
	if err := scanner.Scan(context.Background(), filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("Scan() accepted a missing file")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := scanner.Scan(ctx, filepath.Join(t.TempDir(), "anything")); err == nil {
		t.Fatal("Scan() ignored cancellation")
	}
}

func writeFakeClamAV(t *testing.T, exitCode int) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "clamdscan")
	contents := []byte("#!/bin/sh\nexit " + strconv.Itoa(exitCode) + "\n")
	if err := os.WriteFile(path, contents, 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}
