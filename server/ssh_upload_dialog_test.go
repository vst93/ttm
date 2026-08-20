package server

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestUploadWithDialogCancelDoesNotProbeRemote(t *testing.T) {
	stdin := &bytesReader{b: []byte{0x1b}}
	var tty bytes.Buffer

	uploadWithDialog(stdin, nil, nil, sshConnInfo{}, localeEN, &tty)

	out := tty.String()
	if out == "" {
		t.Fatal("expected transfer menu output")
	}
}

func TestParseRemoteEntries(t *testing.T) {
	got := parseRemoteEntries("alpha\tf\nbeta\td\n spaced name\tf\n")
	want := []remoteEntry{
		{name: "alpha"},
		{name: "beta", isDir: true},
		{name: "spaced name"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseRemoteEntries() = %#v, want %#v", got, want)
	}
}

func TestParseSCPHeaderPreservesUTF8AndSpaces(t *testing.T) {
	mode, size, name, err := parseSCPHeader("C0644 123 中文 文件 '$(id)'.txt", 'C')
	if err != nil {
		t.Fatalf("parseSCPHeader: %v", err)
	}
	if mode != "0644" || size != 123 || name != "中文 文件 '$(id)'.txt" {
		t.Fatalf("unexpected parsed header: mode=%q size=%d name=%q", mode, size, name)
	}
}

func TestParseSCPHeaderRejectsInvalidNamesAndSizes(t *testing.T) {
	for _, line := range []string{
		"C0644 -1 file",
		"C0644 nope file",
		"C0644 1 ..",
		"C0644 1 bad\rname",
	} {
		if _, _, _, err := parseSCPHeader(line, 'C'); err == nil {
			t.Errorf("expected %q to be rejected", line)
		}
	}
}

func TestSafeSCPChildPathRejectsTraversalAndSymlink(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"..", "../outside", "/absolute", "child/name"} {
		if _, err := safeSCPChildPath(dir, name); err == nil {
			t.Errorf("expected unsafe name %q to be rejected", name)
		}
	}

	if err := os.Symlink(t.TempDir(), filepath.Join(dir, "linked")); err == nil {
		if _, err := safeSCPChildPath(dir, "linked"); err == nil {
			t.Error("expected symlink destination to be rejected")
		}
	}

	got, err := safeSCPChildPath(dir, "中文 空格.txt")
	if err != nil || got != filepath.Join(dir, "中文 空格.txt") {
		t.Fatalf("safe UTF-8 child: got=%q err=%v", got, err)
	}
}

func TestRecvDirRecursiveRejectsTraversal(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(filepath.Dir(dir), "outside.txt")
	stdout := strings.NewReader("C0644 4 ../outside.txt\nDATA\x00")
	var stdin bytes.Buffer
	err := recvDirRecursive(&stdin, stdout, dir, nil, context.Background())
	if err == nil {
		t.Fatal("expected traversal entry to fail")
	}
	if _, statErr := os.Stat(outside); !os.IsNotExist(statErr) {
		t.Fatalf("outside file was created: %v", statErr)
	}
}

func TestRecvDirRecursiveAcknowledgesTopLevelEnd(t *testing.T) {
	var stdin bytes.Buffer
	if err := recvDirRecursive(&stdin, strings.NewReader("E\n"), t.TempDir(), nil, context.Background()); err != nil {
		t.Fatalf("recvDirRecursive: %v", err)
	}
	if got := stdin.Bytes(); !bytes.Equal(got, []byte{0, 0}) {
		t.Fatalf("protocol acknowledgements = %v, want initial ready and end ACK", got)
	}
}

func TestRecvDirRecursiveRejectsPrematureEOF(t *testing.T) {
	var stdin bytes.Buffer
	if err := recvDirRecursive(&stdin, strings.NewReader(""), t.TempDir(), nil, context.Background()); err == nil {
		t.Fatal("expected premature SCP EOF to fail")
	}
}

func TestRecvDirRecursiveAcceptsEOFOnlyAfterCompleteTopLevelEntry(t *testing.T) {
	dir := t.TempDir()
	protocol := "D0755 0 中文目录\nE\n"
	var stdin bytes.Buffer
	if err := recvDirRecursive(&stdin, strings.NewReader(protocol), dir, nil, context.Background()); err != nil {
		t.Fatalf("complete top-level directory followed by EOF: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "中文目录")); err != nil {
		t.Fatalf("top-level directory was not created: %v", err)
	}
	if got := stdin.Bytes(); !bytes.Equal(got, []byte{0, 0, 0}) {
		t.Fatalf("protocol acknowledgements = %v, want top ready, directory ready, and E ACK", got)
	}
}

func TestRecvDirRecursiveRejectsNestedEOFWithoutEndMarker(t *testing.T) {
	protocol := "D0755 0 incomplete\n"
	var stdin bytes.Buffer
	if err := recvDirRecursive(&stdin, strings.NewReader(protocol), t.TempDir(), nil, context.Background()); err == nil {
		t.Fatal("expected nested EOF without E marker to fail")
	}
}

func TestReadInputLineTabCompletesFromPrefilledDefault(t *testing.T) {
	stdin := &bytesReader{b: []byte("\t\r")}
	var tty bytes.Buffer
	seen := ""
	got := readInputLine(&tty, stdin, "/Users/v/Downloads", func(in string) (string, []string) {
		seen = in
		return in + "/sub", nil
	})
	if seen != "/Users/v/Downloads" {
		t.Fatalf("Tab completed from %q, want the pre-filled default", seen)
	}
	if got != "/Users/v/Downloads/sub" {
		t.Fatalf("readInputLine = %q, want the completed default", got)
	}
}

func TestReadInputLineTypingAfterTabAppends(t *testing.T) {
	stdin := &bytesReader{b: []byte("\tx\r")}
	var tty bytes.Buffer
	got := readInputLine(&tty, stdin, "/tmp/base", func(in string) (string, []string) {
		return in + "/", nil
	})
	if got != "/tmp/base/x" {
		t.Fatalf("readInputLine = %q, want typing after Tab to append", got)
	}
}

// The pre-filled path must survive the first keystroke: typing "/" used to wipe
// the whole default, which is exactly what a user continuing the path does.
func TestReadInputLineTypingKeepsPrefilledDefault(t *testing.T) {
	stdin := &bytesReader{b: []byte("/sub/报表.xlsx\r")}
	var tty bytes.Buffer
	if got := readInputLine(&tty, stdin, "/root/temp", nil); got != "/root/temp/sub/报表.xlsx" {
		t.Fatalf("readInputLine = %q, want the typed text appended to the default", got)
	}
}

func TestReadInputLineCtrlUClearsPrefilledDefault(t *testing.T) {
	stdin := &bytesReader{b: []byte("\x15/etc\r")}
	var tty bytes.Buffer
	if got := readInputLine(&tty, stdin, "/root/temp", nil); got != "/etc" {
		t.Fatalf("readInputLine = %q, want Ctrl+U to clear the default first", got)
	}
}

func TestReadRemotePathTypingKeepsPrefilledDefault(t *testing.T) {
	stdin := &bytesReader{b: []byte("/报表.xlsx\r")}
	var tty bytes.Buffer
	if got := readRemotePath(&tty, stdin, nil, "/root/temp"); got != "/root/temp/报表.xlsx" {
		t.Fatalf("readRemotePath = %q, want the typed text appended to the default", got)
	}
}

func TestLocalDefaultsUseLaunchDirectory(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if got := localDefaultDir(); got != wd {
		t.Fatalf("localDefaultDir() = %q, want the launch directory %q", got, wd)
	}
	prefix := localDefaultFilePrefix()
	if !strings.HasPrefix(prefix, wd) || !strings.HasSuffix(prefix, string(filepath.Separator)) {
		t.Fatalf("localDefaultFilePrefix() = %q, want %q plus a trailing separator", prefix, wd)
	}
}

func TestReadRemotePathTabKeepsDefaultWhenCompletionUnavailable(t *testing.T) {
	stdin := &bytesReader{b: []byte("\t\r")}
	var tty bytes.Buffer
	// nil client -> remoteTabComplete cannot advance; the pre-filled remote
	// path must survive instead of being wiped (transfer would then fail).
	if got := readRemotePath(&tty, stdin, nil, "/root/temp/报表.xlsx"); got != "/root/temp/报表.xlsx" {
		t.Fatalf("readRemotePath = %q, want the pre-filled default preserved", got)
	}
}

func TestCompletePathFromEntries(t *testing.T) {
	entries := []completionCandidate{
		{name: "报表-0727.xlsx"},
		{name: "报表-0728.xlsx"},
		{name: "logs", isDir: true},
		{name: "."},
	}
	tests := []struct {
		prefix    string
		wantName  string
		wantIsDir bool
		wantCount int
	}{
		{prefix: "log", wantName: "logs", wantIsDir: true, wantCount: 1},
		{prefix: "报表", wantName: "报表-072", wantCount: 2},
		{prefix: "报表-0727", wantName: "报表-0727.xlsx", wantCount: 1},
		{prefix: "nope", wantName: "", wantCount: 0},
		{prefix: "", wantName: "", wantCount: 3},
	}
	for _, tt := range tests {
		name, isDir, matches := completePathFromEntries(entries, tt.prefix)
		if name != tt.wantName || isDir != tt.wantIsDir || len(matches) != tt.wantCount {
			t.Errorf("completePathFromEntries(prefix=%q) = (%q, %v, %d matches), want (%q, %v, %d)",
				tt.prefix, name, isDir, len(matches), tt.wantName, tt.wantIsDir, tt.wantCount)
		}
	}
}

func TestLocalTabCompleteStopsAtCommonPrefix(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"报表-0727.xlsx", "报表-0728.xlsx"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	got, matches := localTabComplete(filepath.Join(dir, "报表"))
	if want := filepath.Join(dir, "报表-072"); got != want {
		t.Fatalf("localTabComplete = %q, want the common prefix %q", got, want)
	}
	if len(matches) != 2 {
		t.Fatalf("matches = %v, want both candidates", matches)
	}
}

func TestLocalTabCompleteKeepsTrailingSlashWhenAmbiguous(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"alpha", "beta"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	input := dir + string(filepath.Separator)
	if got, _ := localTabComplete(input); got != input {
		t.Fatalf("localTabComplete(%q) = %q, want the input unchanged", input, got)
	}
}

func TestSendDirRecursiveWaitsForEndAck(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "中文 文件.txt"), []byte("data"), 0600); err != nil {
		t.Fatal(err)
	}
	var protocol bytes.Buffer
	acks := 0
	err := sendDirRecursive(&protocol, func() error {
		acks++
		return nil
	}, dir, dir, nil, context.Background())
	if err != nil {
		t.Fatalf("sendDirRecursive: %v", err)
	}
	if acks != 3 {
		t.Fatalf("expected directory, file, and end ACKs, got %d", acks)
	}
	if !strings.Contains(protocol.String(), "C0600 4 中文 文件.txt\n") {
		t.Fatalf("UTF-8 SCP filename was not preserved: %q", protocol.String())
	}
}

func TestCompletionHintListsAmbiguousCandidates(t *testing.T) {
	hint := completionHint([]string{"报表-0727.xlsx", "报表-0728.xlsx"})
	if !strings.HasPrefix(hint, "  2: ") || !strings.Contains(hint, "报表-0727.xlsx") {
		t.Fatalf("completionHint = %q, want a count and the candidates", hint)
	}
	if w := displayWidth(hint); w > 48 {
		t.Fatalf("completionHint width = %d, want it to stay on one line", w)
	}

	many := make([]string, 40)
	for i := range many {
		many[i] = "candidate-with-a-long-name"
	}
	if hint := completionHint(many); !strings.HasSuffix(hint, "…") || displayWidth(hint) > 48 {
		t.Fatalf("completionHint(40 matches) = %q, want a truncated one-line hint", hint)
	}
}

func TestReadInputLineShowsCandidatesWhenTabCannotAdvance(t *testing.T) {
	stdin := &bytesReader{b: []byte("\t\r")}
	var tty bytes.Buffer
	readInputLine(&tty, stdin, "/tmp/", func(in string) (string, []string) {
		return in, []string{"alpha", "beta"}
	})
	out := tty.String()
	if !strings.Contains(out, "2: alpha beta") {
		t.Fatalf("tty output %q, want the candidate hint", out)
	}
	if !strings.Contains(out, "\x1b7") || !strings.Contains(out, "\x1b8") || !strings.Contains(out, "\x1b[K") {
		t.Fatalf("tty output %q, want the hint saved, restored and erased", out)
	}
}
