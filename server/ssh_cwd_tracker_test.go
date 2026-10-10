package server

import (
	"bytes"
	"errors"
	"io"
	"strings"
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

// Include synchronized-output, cursor movement, hyperlinks and UTF-8. The
// observer must never modify or buffer terminal data, even across SSH reads.
func TestRemoteOutputCwdWriterChunkBoundaries(t *testing.T) {
	for _, terminator := range []string{"\x07", "\x1b\\"} {
		stream := "\x1b[?2026h\x1b[2;1H\x1b[38;2;100;200;255m消息\x1b[0m" +
			"\x1b]8;;https://example.com\x1b\\link\x1b]8;;\x1b\\" +
			"\x1b]7;file://host/home/user/my%20project" + terminator +
			"\x1b[?2026l"
		for split := 0; split <= len(stream); split++ {
			cache := newRemoteCwdCache()
			var dst bytes.Buffer
			w := newRemoteOutputCwdWriter(&dst, cache)
			for _, part := range []string{stream[:split], "", stream[split:]} {
				n, err := w.Write([]byte(part))
				if n != len(part) || err != nil {
					t.Fatalf("split %d: write = %d, %v", split, n, err)
				}
			}
			if got := dst.String(); got != stream {
				t.Fatalf("split %d: terminal output changed", split)
			}
			if dir, ok := cache.Get(); !ok || dir != "/home/user/my project" {
				t.Fatalf("split %d, terminator %q: cwd = %q, %v", split, terminator, dir, ok)
			}
		}
	}
}

func TestRemoteOutputCwdWriterByteAtATime(t *testing.T) {
	cache := newRemoteCwdCache()
	var dst bytes.Buffer
	w := newRemoteOutputCwdWriter(&dst, cache)
	stream := []byte("\x1b]0;title\x07\x1b]7;file://host/tmp/中文\x1b\\prompt")
	for i := range stream {
		if _, err := w.Write(stream[i : i+1]); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(dst.Bytes(), stream[:i+1]) {
			t.Fatalf("byte %d: output was buffered or changed", i)
		}
	}
	if dir, ok := cache.Get(); !ok || dir != "/tmp/中文" {
		t.Fatalf("cwd = %q, %v", dir, ok)
	}
}

func TestRemoteOutputCwdWriterIgnoresOtherOSC(t *testing.T) {
	cache := newRemoteCwdCache()
	cache.Set("/original")
	var dst bytes.Buffer
	w := newRemoteOutputCwdWriter(&dst, cache)
	stream := "\x1b]0;" + strings.Repeat("title", 2000) + "\x07" +
		"\x1b]52;c;clipboard\x1b\\" +
		"\x1b]8;;https://example.com\x1b\\link\x1b]8;;\x1b\\"
	if _, err := w.Write([]byte(stream)); err != nil {
		t.Fatal(err)
	}
	if dst.String() != stream {
		t.Fatal("non-CWD OSC output changed")
	}
	if dir, _ := cache.Get(); dir != "/original" {
		t.Fatalf("non-CWD OSC changed cwd to %q", dir)
	}
	if _, err := w.Write([]byte("\x1b]7;file://host/next\x07")); err != nil {
		t.Fatal(err)
	}
	if dir, _ := cache.Get(); dir != "/next" {
		t.Fatalf("failed to recover after non-CWD OSC: %q", dir)
	}
}

type shortOutputWriter struct {
	err error
}

func (w shortOutputWriter) Write(p []byte) (int, error) {
	return len(p) / 2, w.err
}

func TestRemoteOutputCwdWriterWithoutCacheIsDirect(t *testing.T) {
	var dst bytes.Buffer
	if w := newRemoteOutputCwdWriter(&dst, nil); w != &dst {
		t.Fatal("disabled tracking must return the destination without a wrapper")
	}
	if w := newRemoteOutputCwdWriter(nil, newRemoteCwdCache()); w != nil {
		t.Fatal("nil destination must remain nil")
	}
}

func TestRemoteOutputCwdWriterOversizedOSC7(t *testing.T) {
	for _, chunkSize := range []int{1, 8192} {
		cache := newRemoteCwdCache()
		cache.Set("/original")
		var dst bytes.Buffer
		w := newRemoteOutputCwdWriter(&dst, cache)
		stream := []byte("\x1b]7;file://host/" + strings.Repeat("a", 5000) + "\x1b\\")
		for offset := 0; offset < len(stream); offset += chunkSize {
			if _, err := w.Write(stream[offset:min(offset+chunkSize, len(stream))]); err != nil {
				t.Fatal(err)
			}
		}
		if !bytes.Equal(dst.Bytes(), stream) {
			t.Fatal("oversized OSC was not forwarded intact")
		}
		if dir, _ := cache.Get(); dir != "/original" {
			t.Fatalf("oversized OSC cached a truncated path: %q", dir)
		}
		if _, err := w.Write([]byte("\x1b]7;file://host/recovered\x07")); err != nil {
			t.Fatal(err)
		}
		if dir, _ := cache.Get(); dir != "/recovered" {
			t.Fatalf("parser did not recover after oversized OSC: %q", dir)
		}
	}
}

func TestRemoteOutputCwdWriterRepeatedEsc(t *testing.T) {
	stream := []byte("\x1b\x1b]7;file://host/tmp\x07")
	for split := 0; split <= len(stream); split++ {
		cache := newRemoteCwdCache()
		w := newRemoteOutputCwdWriter(io.Discard, cache)
		for _, part := range [][]byte{stream[:split], stream[split:]} {
			if _, err := w.Write(part); err != nil {
				t.Fatal(err)
			}
		}
		if dir, _ := cache.Get(); dir != "/tmp" {
			t.Fatalf("split %d: repeated ESC hid OSC 7: %q", split, dir)
		}
	}
}

func TestRemoteOutputCwdWriterPropagatesWriteResult(t *testing.T) {
	wantErr := errors.New("terminal write failed")
	w := newRemoteOutputCwdWriter(shortOutputWriter{err: wantErr}, newRemoteCwdCache())
	n, err := w.Write([]byte("output"))
	if n != 3 || !errors.Is(err, wantErr) {
		t.Fatalf("write = %d, %v; want 3, %v", n, err, wantErr)
	}
}

func BenchmarkRemoteOutputCwdWriter(b *testing.B) {
	for _, tc := range []struct {
		name string
		line string
	}{
		{"text", "Streaming response with ordinary text and UTF-8 中文.\r\n"},
		{"ansi", "\x1b[12;1H\x1b[38;2;100;200;255mStreaming response 中文\x1b[0m\x1b[K\r\n"},
		{"hyperlinks", "\x1b]8;;https://example.com/docs\x1b\\documentation\x1b]8;;\x1b\\\r\n"},
	} {
		b.Run(tc.name, func(b *testing.B) {
			data := bytes.Repeat([]byte(tc.line), (32*1024)/len(tc.line))
			w := newRemoteOutputCwdWriter(io.Discard, newRemoteCwdCache())
			b.SetBytes(int64(len(data)))
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := w.Write(data); err != nil {
					b.Fatal(err)
				}
			}
		})
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
