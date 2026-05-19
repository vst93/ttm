package server

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"io"
	"os"
	"testing"

	"github.com/muesli/cancelreader"
)

func generatePrivateKeyPEM(t *testing.T) string {
	t.Helper()

	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate rsa key: %v", err)
	}

	pemBlock := &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(privateKey),
	}

	return string(pem.EncodeToMemory(pemBlock))
}

func TestGenSSHConfig_WithPrivateKeyNoPassphrase_UsesPublicKeyAuth(t *testing.T) {
	keyPEM := generatePrivateKeyPEM(t)
	client, err := genSSHConfig(&SSHConfig{
		Host:       "127.0.0.1",
		User:       "root",
		Port:       22,
		PrivateKey: keyPEM,
	})
	if err != nil {
		t.Fatalf("expected private key config to succeed, got error: %v", err)
	}

	if got := len(client.clientConfig.Auth); got < 2 {
		t.Fatalf("expected public key auth + keyboard-interactive, got %d auth method(s)", got)
	}
}

func TestIsStdinCopyErrBenign(t *testing.T) {
	if !isStdinCopyErrBenign(nil) {
		t.Fatalf("expected nil error to be benign")
	}
	if !isStdinCopyErrBenign(io.EOF) {
		t.Fatalf("expected EOF to be benign")
	}
	if !isStdinCopyErrBenign(cancelreader.ErrCanceled) {
		t.Fatalf("expected cancelreader.ErrCanceled to be benign")
	}
	if isStdinCopyErrBenign(errors.New("boom")) {
		t.Fatalf("expected generic error to be non-benign")
	}
}

func TestSSHTerm(t *testing.T) {
	origTerm, hadTerm := os.LookupEnv("TERM")
	if hadTerm {
		defer os.Setenv("TERM", origTerm)
	} else {
		defer os.Unsetenv("TERM")
	}

	os.Setenv("TERM", "xterm-kitty")
	if got := sshTerm(); got != "xterm-kitty" {
		t.Fatalf("expected TERM to be forwarded, got %q", got)
	}

	os.Setenv("TERM", "dumb")
	if got := sshTerm(); got != "xterm-256color" {
		t.Fatalf("expected dumb TERM to fall back to xterm-256color, got %q", got)
	}

	os.Unsetenv("TERM")
	if got := sshTerm(); got != "xterm-256color" {
		t.Fatalf("expected empty TERM to fall back to xterm-256color, got %q", got)
	}
}
