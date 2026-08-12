package server

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"io"
	"os"
	"strings"
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

func withTMUXEnv(t *testing.T, value string, fn func()) {
	t.Helper()
	orig, had := os.LookupEnv("TMUX")
	if had {
		defer os.Setenv("TMUX", orig)
	} else {
		defer os.Unsetenv("TMUX")
	}
	if value == "" {
		os.Unsetenv("TMUX")
	} else {
		os.Setenv("TMUX", value)
	}
	fn()
}

func TestTmuxPassthroughNoTmuxEnv(t *testing.T) {
	withTMUXEnv(t, "", func() {
		seq := "\x1b[?1007l"
		if got := tmuxPassthrough(seq); got != seq {
			t.Fatalf("expected sequence unchanged outside tmux, got %q", got)
		}
	})
}

func TestTmuxPassthroughWrapsAndDoublesESC(t *testing.T) {
	withTMUXEnv(t, "tmux 3.3a", func() {
		seq := "\x1b[?1007l"
		want := "\x1bPtmux;\x1b\x1b[?1007l\x1b\\"
		if got := tmuxPassthrough(seq); got != want {
			t.Fatalf("expected wrapped passthrough %q, got %q", want, got)
		}
	})
}

func TestTmuxPassthroughDoublesEveryESC(t *testing.T) {
	withTMUXEnv(t, "tmux 3.3a", func() {
		seq := "\x1b[2J\x1b[0;0H\x1b[?25h"
		got := tmuxPassthrough(seq)
		if strings.Count(got, "\x1b\x1b") != 3 {
			t.Fatalf("expected all three ESC bytes doubled, got %q", got)
		}
		if !strings.HasPrefix(got, "\x1bPtmux;") || !strings.HasSuffix(got, "\x1b\\") {
			t.Fatalf("expected tmux passthrough wrapper, got %q", got)
		}
	})
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
