package server

import (
	"runtime"
	"strings"
	"testing"
)

func TestNormalizeRemotePathInput(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{`/home/tar/dir/`, `/home/tar/dir`},
		{`"/home/tar/my dir"`, `/home/tar/my dir`},
		{`/home/tar/my\ dir/file.txt`, `/home/tar/my dir/file.txt`},
	}
	for _, tc := range cases {
		if got := normalizeRemotePathInput(tc.in); got != tc.want {
			t.Fatalf("normalizeRemotePathInput(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestNormalizeLocalPathInputUnixEscapedSpace(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix-style path test")
	}
	got := normalizeLocalPathInput(`/Users/v/Downloads/截屏2026-06-22\ 11.03.17.png`)
	want := `/Users/v/Downloads/截屏2026-06-22 11.03.17.png`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestNormalizeLocalPathInputQuotesAndTilde(t *testing.T) {
	got := normalizeLocalPathInput(`"~/Downloads/demo file.txt"`)
	if strings.HasPrefix(got, `"`) || strings.HasPrefix(got, `~/`) {
		t.Fatalf("expected quotes/tilde normalization, got %q", got)
	}
}

func TestNormalizeLocalPathInputWindowsStylePath(t *testing.T) {
	in := `"C:\\Users\\v\\Downloads\\demo file.txt"`
	got := normalizeLocalPathInput(in)
	if runtime.GOOS == "windows" {
		if got != `C:\Users\v\Downloads\demo file.txt` {
			t.Fatalf("windows path: got %q", got)
		}
		return
	}
	if got != `C:\Users\v\Downloads\demo file.txt` {
		t.Fatalf("non-windows path should preserve backslashes, got %q", got)
	}
}

func TestTrimMatchingQuotes(t *testing.T) {
	if got := trimMatchingQuotes(`"abc"`); got != `abc` {
		t.Fatalf("double quotes: got %q", got)
	}
	if got := trimMatchingQuotes(`'abc'`); got != `abc` {
		t.Fatalf("single quotes: got %q", got)
	}
}
