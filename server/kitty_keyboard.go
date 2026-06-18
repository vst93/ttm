package server

import "io"

// ── Kitty keyboard protocol local gating ──────────────────────────────────────
//
// When a remote shell/prompt (fish + starship, zsh + oh-my-zsh, ...) enables
// the kitty keyboard protocol (CSI > Ps u), capable local terminals (Ghostty,
// kitty, WezTerm, ...) stop sending legacy control bytes and instead send
// disambiguated CSI u sequences:
//
//   Esc     -> ESC[27u      (instead of 0x1b)
//   Ctrl+C  -> ESC[3;5u     (instead of 0x03)
//   Ctrl+G  -> ESC[103;5u   (instead of 0x07)
//
// The upload dialog reads input byte-by-byte and treats ESC as the start of a
// terminal response to drain — so under kitty mode the Esc/Ctrl+C cancel keys
// are drained and never reach the dialog, making cancel impossible. (Ctrl+G
// double-tap detection in the main loop already handles its kitty form.)
//
// pushKittyOff / popKitty temporarily switch the LOCAL terminal back to legacy
// encoding for the duration of the dialog, so Esc/Ctrl+C arrive as 0x1b/0x03
// and all existing input/cancel logic works unchanged. They use the kitty
// keyboard push/pop stack:
//
//   ESC[>0u  push current flags, set flags 0  (legacy mode)
//   ESC[<1u  pop 1 level                      (restore previous flags)
//
// This preserves the remote's exact flag state (e.g. CSI > 5 u) on restore.
// On terminals without kitty keyboard support both sequences are unknown CSI
// and silently ignored, so this is always safe.

// kittyPushOff disables kitty keyboard mode locally (push current state).
func kittyPushOff(w io.Writer) {
	if w == nil {
		return
	}
	// ESC [ > 0 u
	_, _ = w.Write([]byte{0x1b, 0x5b, 0x3e, 0x30, 0x75})
}

// kittyPop restores the previous kitty keyboard flag state (pop 1 level).
func kittyPop(w io.Writer) {
	if w == nil {
		return
	}
	// ESC [ < 1 u
	_, _ = w.Write([]byte{0x1b, 0x5b, 0x3c, 0x31, 0x75})
}
