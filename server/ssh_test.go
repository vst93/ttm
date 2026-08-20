package server

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/muesli/cancelreader"
	"golang.org/x/crypto/ssh"
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

func TestTOFUHostKeyCallbackRecordsAndRejectsChanges(t *testing.T) {
	originalAppDir := APP_DIR
	APP_DIR = t.TempDir()
	defer func() { APP_DIR = originalAppDir }()

	makeKey := func() ssh.PublicKey {
		privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			t.Fatal(err)
		}
		key, err := ssh.NewPublicKey(&privateKey.PublicKey)
		if err != nil {
			t.Fatal(err)
		}
		return key
	}

	hostname := "tofu-test.invalid:2222"
	remote := &net.TCPAddr{IP: net.ParseIP("192.0.2.10"), Port: 2222}
	key := makeKey()
	callback := tofuHostKeyCallback()
	if err := callback(hostname, remote, key); err != nil {
		t.Fatalf("first-use host key: %v", err)
	}
	if err := callback(hostname, remote, key); err != nil {
		t.Fatalf("recorded host key: %v", err)
	}
	if err := callback(hostname, remote, makeKey()); err == nil {
		t.Fatal("changed host key was accepted")
	}

	data, err := os.ReadFile(filepath.Join(APP_DIR, "known_hosts"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "[tofu-test.invalid]:2222") {
		t.Fatalf("known_hosts did not contain normalized host: %q", data)
	}
}

func TestLegacyCiphersAreOptIn(t *testing.T) {
	original, had := os.LookupEnv("TTM_LEGACY_SSH")
	defer func() {
		if had {
			_ = os.Setenv("TTM_LEGACY_SSH", original)
		} else {
			_ = os.Unsetenv("TTM_LEGACY_SSH")
		}
	}()

	containsLegacy := func(ciphers []string) bool {
		for _, configured := range ciphers {
			for _, legacy := range legacyCiphers {
				if configured == legacy {
					return true
				}
			}
		}
		return false
	}

	_ = os.Unsetenv("TTM_LEGACY_SSH")
	secure, err := genSSHConfig(&SSHConfig{Host: "host", User: "user", Port: 22})
	if err != nil {
		t.Fatal(err)
	}
	if containsLegacy(secure.clientConfig.Ciphers) {
		t.Fatalf("legacy cipher enabled by default: %v", secure.clientConfig.Ciphers)
	}

	_ = os.Setenv("TTM_LEGACY_SSH", "1")
	legacy, err := genSSHConfig(&SSHConfig{Host: "host", User: "user", Port: 22})
	if err != nil {
		t.Fatal(err)
	}
	if !containsLegacy(legacy.clientConfig.Ciphers) {
		t.Fatal("legacy cipher opt-in did not take effect")
	}
}
