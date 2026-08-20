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
