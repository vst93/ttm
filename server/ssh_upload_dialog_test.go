package server

import (
	"bytes"
	"reflect"
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
