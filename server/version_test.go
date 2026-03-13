package server

import (
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
