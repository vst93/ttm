package server

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestIsNewer(t *testing.T) {
	tests := []struct {
		current, latest string
		want            bool
	}{
		{"v1.0.0", "v1.0.1", true},
		{"v1.0.0", "v1.0.0", false},
		{"dev", "v1.0.0", true},
		{"", "v1.0.0", true},
		{"v1.0.0", "v1.0.0", false},
		{"1.0.0", "v1.0.0", false},
	}
	for _, tt := range tests {
		got := isNewer(tt.current, tt.latest)
		if got != tt.want {
			t.Errorf("isNewer(%q, %q) = %v, want %v", tt.current, tt.latest, got, tt.want)
		}
	}
}

func TestCleanVersion(t *testing.T) {
	if cleanVersion("v1.2.3") != "1.2.3" {
		t.Fatal("expected v prefix stripped")
	}
	if cleanVersion("1.2.3") != "1.2.3" {
		t.Fatal("expected no change without v prefix")
	}
}

func TestProgressReadCloserReportsBytesAndPercentage(t *testing.T) {
	payload := bytes.Repeat([]byte("x"), 300*1024)
	var reports []updateProgressMsg
	reader := &progressReadCloser{
		ReadCloser: io.NopCloser(bytes.NewReader(payload)),
		total:      int64(len(payload)),
		onProgress: func(msg updateProgressMsg) {
			reports = append(reports, msg)
		},
	}

	if _, err := io.Copy(io.Discard, reader); err != nil {
		t.Fatalf("copy failed: %v", err)
	}
	if len(reports) == 0 {
		t.Fatal("expected at least one progress report")
	}
	last := reports[len(reports)-1]
	if last.Downloaded != int64(len(payload)) || last.Total != int64(len(payload)) || last.Progress != 1 {
		t.Fatalf("unexpected final progress: %+v", last)
	}
}

func TestProgressReadCloserReportsUnknownTotal(t *testing.T) {
	payload := bytes.Repeat([]byte("x"), 16*1024)
	var got updateProgressMsg
	reader := &progressReadCloser{
		ReadCloser: io.NopCloser(bytes.NewReader(payload)),
		onProgress: func(msg updateProgressMsg) {
			got = msg
		},
	}

	if _, err := io.Copy(io.Discard, reader); err != nil {
		t.Fatalf("copy failed: %v", err)
	}
	if got.Downloaded == 0 || got.Total != 0 {
		t.Fatalf("expected byte progress without a total, got %+v", got)
	}
}

func TestInstallUnixBinaryReplacesExecutableAtomically(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "downloaded-ttm")
	dst := filepath.Join(dir, "ttm")
	if err := os.WriteFile(src, []byte("new binary"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, []byte("old binary"), 0755); err != nil {
		t.Fatal(err)
	}

	if err := installUnixBinary(src, dst); err != nil {
		t.Fatalf("install failed: %v", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new binary" {
		t.Fatalf("installed content = %q", got)
	}
	info, err := os.Stat(dst)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0755 {
		t.Fatalf("installed mode = %o, want 755", info.Mode().Perm())
	}
}

func TestIsPermissionErrorRecognizesWindowsAccessDenied(t *testing.T) {
	if !isPermissionError(errors.New("Access is denied.")) {
		t.Fatal("expected Windows access-denied error to be recognized")
	}
}
