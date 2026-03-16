package server

import (
	"errors"
	"strings"
	"testing"
)

func TestWriteTextToClipboardUsesPrimaryWriter(t *testing.T) {
	originalWrite := clipboardWriteAll
	originalOSC52 := clipboardOSC52Write
	originalShouldFallback := shouldAttemptOSC52FallbackFn
	originalShouldPrefer := shouldPreferOSC52Fn
	defer func() {
		clipboardWriteAll = originalWrite
		clipboardOSC52Write = originalOSC52
		shouldAttemptOSC52FallbackFn = originalShouldFallback
		shouldPreferOSC52Fn = originalShouldPrefer
	}()

	calledPrimary := false
	calledOSC52 := false
	clipboardWriteAll = func(text string) error {
		calledPrimary = true
		if text != "abc" {
			t.Fatalf("expected primary writer text abc, got %q", text)
		}
		return nil
	}
	clipboardOSC52Write = func(text string) error {
		calledOSC52 = true
		return nil
	}
	shouldAttemptOSC52FallbackFn = func() bool { return false }
	shouldPreferOSC52Fn = func() bool { return false }

	usedOSC52Only, err := writeTextToClipboard("abc")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if usedOSC52Only {
		t.Fatalf("expected primary clipboard to be used")
	}
	if !calledPrimary {
		t.Fatalf("expected primary writer called")
	}
	if calledOSC52 {
		t.Fatalf("expected no osc52 write when dual-write disabled")
	}
}

func TestWriteTextToClipboardFallsBackToOSC52(t *testing.T) {
	originalWrite := clipboardWriteAll
	originalOSC52 := clipboardOSC52Write
	originalShouldFallback := shouldAttemptOSC52FallbackFn
	originalShouldPrefer := shouldPreferOSC52Fn
	defer func() {
		clipboardWriteAll = originalWrite
		clipboardOSC52Write = originalOSC52
		shouldAttemptOSC52FallbackFn = originalShouldFallback
		shouldPreferOSC52Fn = originalShouldPrefer
	}()

	clipboardWriteAll = func(text string) error { return errors.New("primary failed") }
	clipboardOSC52Write = func(text string) error {
		if text != "abc" {
			t.Fatalf("expected osc52 writer text abc, got %q", text)
		}
		return nil
	}
	shouldAttemptOSC52FallbackFn = func() bool { return true }
	shouldPreferOSC52Fn = func() bool { return false }

	usedOSC52Only, err := writeTextToClipboard("abc")
	if err != nil {
		t.Fatalf("expected fallback success, got %v", err)
	}
	if !usedOSC52Only {
		t.Fatalf("expected osc52-only result")
	}
}

func TestWriteTextToClipboardReturnsErrorWhenAllBackendsFail(t *testing.T) {
	originalWrite := clipboardWriteAll
	originalOSC52 := clipboardOSC52Write
	originalShouldFallback := shouldAttemptOSC52FallbackFn
	originalShouldPrefer := shouldPreferOSC52Fn
	defer func() {
		clipboardWriteAll = originalWrite
		clipboardOSC52Write = originalOSC52
		shouldAttemptOSC52FallbackFn = originalShouldFallback
		shouldPreferOSC52Fn = originalShouldPrefer
	}()

	clipboardWriteAll = func(text string) error { return errors.New("primary failed") }
	clipboardOSC52Write = func(text string) error { return errors.New("osc52 failed") }
	shouldAttemptOSC52FallbackFn = func() bool { return true }
	shouldPreferOSC52Fn = func() bool { return false }

	usedOSC52Only, err := writeTextToClipboard("abc")
	if err == nil {
		t.Fatalf("expected error when all backends fail")
	}
	if usedOSC52Only {
		t.Fatalf("expected osc52-only to be false on failure")
	}
	if err.Error() != "primary failed" {
		t.Fatalf("expected primary error to be returned, got %v", err)
	}
}

func TestWriteTextToClipboardSkipsOSC52FallbackWhenDisabled(t *testing.T) {
	originalWrite := clipboardWriteAll
	originalOSC52 := clipboardOSC52Write
	originalShouldFallback := shouldAttemptOSC52FallbackFn
	originalShouldPrefer := shouldPreferOSC52Fn
	defer func() {
		clipboardWriteAll = originalWrite
		clipboardOSC52Write = originalOSC52
		shouldAttemptOSC52FallbackFn = originalShouldFallback
		shouldPreferOSC52Fn = originalShouldPrefer
	}()

	clipboardWriteAll = func(text string) error { return errors.New("primary failed") }
	calledOSC52 := false
	clipboardOSC52Write = func(text string) error {
		calledOSC52 = true
		return nil
	}
	shouldAttemptOSC52FallbackFn = func() bool { return false }
	shouldPreferOSC52Fn = func() bool { return false }

	usedOSC52Only, err := writeTextToClipboard("abc")
	if err == nil {
		t.Fatalf("expected error when fallback disabled and primary fails")
	}
	if usedOSC52Only {
		t.Fatalf("expected osc52-only false when fallback disabled")
	}
	if calledOSC52 {
		t.Fatalf("expected osc52 not called when fallback disabled")
	}
}

func TestWriteTextToClipboardDoesNotUseOSC52WhenPrimarySucceeds(t *testing.T) {
	originalWrite := clipboardWriteAll
	originalOSC52 := clipboardOSC52Write
	originalShouldFallback := shouldAttemptOSC52FallbackFn
	originalShouldPrefer := shouldPreferOSC52Fn
	defer func() {
		clipboardWriteAll = originalWrite
		clipboardOSC52Write = originalOSC52
		shouldAttemptOSC52FallbackFn = originalShouldFallback
		shouldPreferOSC52Fn = originalShouldPrefer
	}()

	calledOSC52 := false
	clipboardWriteAll = func(text string) error { return nil }
	clipboardOSC52Write = func(text string) error {
		calledOSC52 = true
		return nil
	}
	shouldAttemptOSC52FallbackFn = func() bool { return true }
	shouldPreferOSC52Fn = func() bool { return false }

	usedOSC52Only, err := writeTextToClipboard("abc")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if usedOSC52Only {
		t.Fatalf("expected primary clipboard to be used")
	}
	if calledOSC52 {
		t.Fatalf("expected no osc52 write when primary clipboard succeeds")
	}
}

func TestWriteTextToClipboardSkipsOSC52WhenTextTooLong(t *testing.T) {
	originalWrite := clipboardWriteAll
	originalOSC52 := clipboardOSC52Write
	originalShouldFallback := shouldAttemptOSC52FallbackFn
	originalShouldPrefer := shouldPreferOSC52Fn
	defer func() {
		clipboardWriteAll = originalWrite
		clipboardOSC52Write = originalOSC52
		shouldAttemptOSC52FallbackFn = originalShouldFallback
		shouldPreferOSC52Fn = originalShouldPrefer
	}()

	clipboardWriteAll = func(text string) error { return errors.New("primary failed") }
	calledOSC52 := false
	clipboardOSC52Write = func(text string) error {
		calledOSC52 = true
		return nil
	}
	shouldAttemptOSC52FallbackFn = func() bool { return true }
	shouldPreferOSC52Fn = func() bool { return false }

	longText := strings.Repeat("x", maxOSC52TextBytes+1)
	usedOSC52Only, err := writeTextToClipboard(longText)
	if err == nil {
		t.Fatalf("expected error for too long terminal clipboard text")
	}
	if usedOSC52Only {
		t.Fatalf("expected osc52-only false for too long text")
	}
	if calledOSC52 {
		t.Fatalf("expected osc52 not called for too long text")
	}
}

func TestWriteTextToClipboardPrefersOSC52InRemoteLikeEnv(t *testing.T) {
	originalWrite := clipboardWriteAll
	originalOSC52 := clipboardOSC52Write
	originalShouldFallback := shouldAttemptOSC52FallbackFn
	originalShouldPrefer := shouldPreferOSC52Fn
	defer func() {
		clipboardWriteAll = originalWrite
		clipboardOSC52Write = originalOSC52
		shouldAttemptOSC52FallbackFn = originalShouldFallback
		shouldPreferOSC52Fn = originalShouldPrefer
	}()

	calledPrimary := false
	clipboardWriteAll = func(text string) error {
		calledPrimary = true
		return nil
	}
	clipboardOSC52Write = func(text string) error { return nil }
	shouldAttemptOSC52FallbackFn = func() bool { return true }
	shouldPreferOSC52Fn = func() bool { return true }

	usedOSC52Only, err := writeTextToClipboard("abc")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !usedOSC52Only {
		t.Fatalf("expected osc52 preferred path to be used")
	}
	if calledPrimary {
		t.Fatalf("expected primary writer skipped when osc52 preferred succeeds")
	}
}

func TestWriteTextToClipboardPreferOSC52FailureDoesNotFallbackToPrimary(t *testing.T) {
	originalWrite := clipboardWriteAll
	originalOSC52 := clipboardOSC52Write
	originalShouldFallback := shouldAttemptOSC52FallbackFn
	originalShouldPrefer := shouldPreferOSC52Fn
	defer func() {
		clipboardWriteAll = originalWrite
		clipboardOSC52Write = originalOSC52
		shouldAttemptOSC52FallbackFn = originalShouldFallback
		shouldPreferOSC52Fn = originalShouldPrefer
	}()

	calledPrimary := false
	clipboardWriteAll = func(text string) error {
		calledPrimary = true
		return nil
	}
	clipboardOSC52Write = func(text string) error { return errors.New("osc52 failed") }
	shouldAttemptOSC52FallbackFn = func() bool { return true }
	shouldPreferOSC52Fn = func() bool { return true }

	usedOSC52Only, err := writeTextToClipboard("abc\nxyz")
	if err == nil {
		t.Fatalf("expected error when preferred osc52 path fails")
	}
	if usedOSC52Only {
		t.Fatalf("expected osc52-only false on failure")
	}
	if calledPrimary {
		t.Fatalf("expected primary writer not called when osc52 is preferred")
	}
}
