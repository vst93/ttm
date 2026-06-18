package server

import (
	"bytes"
	"testing"
)

func TestKittyPushOff(t *testing.T) {
	var buf bytes.Buffer
	kittyPushOff(&buf)
	// ESC [ > 0 u
	want := []byte{0x1b, 0x5b, 0x3e, 0x30, 0x75}
	if !bytes.Equal(buf.Bytes(), want) {
		t.Errorf("pushOff = % x, want % x", buf.Bytes(), want)
	}
}

func TestKittyPop(t *testing.T) {
	var buf bytes.Buffer
	kittyPop(&buf)
	// ESC [ < 1 u
	want := []byte{0x1b, 0x5b, 0x3c, 0x31, 0x75}
	if !bytes.Equal(buf.Bytes(), want) {
		t.Errorf("pop = % x, want % x", buf.Bytes(), want)
	}
}

func TestKittyHelpersNilSafe(t *testing.T) {
	// Must not panic on nil writer.
	kittyPushOff(nil)
	kittyPop(nil)
}
