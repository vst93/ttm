package server

import (
	"bytes"
	"testing"
)

func TestParseOSC7Dir(t *testing.T) {
	dir, ok := parseOSC7Dir("7;file://host/home/tar/project")
	if !ok {
		t.Fatal("expected OSC 7 dir")
	}
	if dir != "/home/tar/project" {
		t.Fatalf("got %q", dir)
	}
}

func TestRemoteOutputCwdWriterTracksOSC7(t *testing.T) {
	cache := newRemoteCwdCache()
	var dst bytes.Buffer
	w := newRemoteOutputCwdWriter(&dst, cache)

	_, err := w.Write([]byte("\x1b]7;file://host/home/tar/work\x07prompt"))
	if err != nil {
		t.Fatalf("write err: %v", err)
	}
	dir, ok := cache.Get()
	if !ok || dir != "/home/tar/work" {
		t.Fatalf("cache = %q ok=%v", dir, ok)
	}
}

func TestRemoteShellInputTrackerTracksCd(t *testing.T) {
	cache := newRemoteCwdCache()
	cache.SetHome("/home/tar")
	tracker := newRemoteShellInputTracker(cache)

	tracker.Observe([]byte("cd project\r"))
	dir, ok := cache.Get()
	if !ok || dir != "/home/tar/project" {
		t.Fatalf("after relative cd: %q ok=%v", dir, ok)
	}

	tracker.Observe([]byte("cd /tmp\r"))
	dir, ok = cache.Get()
	if !ok || dir != "/tmp" {
		t.Fatalf("after absolute cd: %q ok=%v", dir, ok)
	}
}
