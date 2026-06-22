package server

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

// ── Shell-agnostic remote execution middleware ────────────────────────────────
//
// Problem
//   When ttm connects to a host whose login shell is fish or a heavily
//   customized zsh (oh-my-zsh), the remote probe commands used by the
//   Ctrl+G double-tap flow (isRemoteIdle, cwd detection, tab completion,
//   stat, ...) used to be sent raw and parsed by that login shell. That
//   broke in two ways:
//
//     1. Syntax: fish < 3.0 has no `||`/`&&`; fish historically used `^`
//        for stderr instead of `2>`; quoting rules differ. POSIX shell
//        snippets like `pgrep -x vim >/dev/null 2>&1 || ... && echo found`
//        can fail to parse or behave wrongly.
//     2. Startup cost: a framework-loaded fish/oh-my-zsh invoked fresh for
//        every exec session (sshd runs `$SHELL -c <cmd>`) can take seconds
//        to start, or even hang on broken interactive config — which made
//        isRemoteIdle block forever so the Ctrl+G dialog never appeared
//        ("no response").
//
// Solution
//   Every remote probe is wrapped as `sh -c '<script>'`. sshd still invokes
//   the login shell, but the login shell only has to exec `sh` with a
//   literal argument — it never parses the script body. Because /bin/sh is
//   POSIX everywhere, the script runs identically whether the login shell
//   is bash, zsh+oh-my-zsh, or fish. A per-call timeout guarantees a slow
//   or hung remote shell can never block the interactive session.

// shQuote double-quote-escapes s so it is taken literally by a POSIX shell.
// Double quotes are used instead of single quotes because fish (unlike
// bash/zsh) does not support the '\” single-quote escape idiom.
// Escaped characters: \ " $ — the three characters special in double quotes
// across POSIX sh, bash, and fish.
func shQuote(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, `$`, `\$`)
	return `"` + s + `"`
}

// wrapShScript wraps a POSIX sh script so it can be handed to the remote
// login shell (fish/zsh/bash) and still execute under /bin/sh verbatim.
// Uses double-quote wrapping via shQuote so fish doesn't choke on the
// '\”' single-quote escape idiom.
func wrapShScript(script string) string {
	return "sh -c " + shQuote(script)
}

// remoteRunSh runs a POSIX sh script on the remote host with a timeout.
// stdout is returned (trimmed of surrounding whitespace). stderr is
// discarded to keep remote noise out of the terminal. On timeout the
// session is aborted and ctx.Err() (context.DeadlineExceeded) is returned.
func remoteRunSh(client *ssh.Client, script string, timeout time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return remoteRunShCtx(ctx, client, script)
}

// remoteRunShCtx is the context-aware variant of remoteRunSh.
func remoteRunShCtx(ctx context.Context, client *ssh.Client, script string) (string, error) {
	if client == nil {
		return "", errors.New("remoteRunSh: nil ssh client")
	}

	start := time.Now()
	session, err := client.NewSession()
	if err != nil {
		debugf("remote: NewSession err=%v", err)
		return "", err
	}

	var buf bytes.Buffer
	session.Stdout = &buf
	// Stderr left nil -> discarded by the ssh library (matches prior behavior).

	if err := session.Start(wrapShScript(script)); err != nil {
		debugf("remote: Start err=%v", err)
		_ = session.Close()
		return "", err
	}

	waitErr := make(chan error, 1)
	go func() { waitErr <- session.Wait() }()

	select {
	case err := <-waitErr:
		_ = session.Close()
		out := strings.TrimSpace(buf.String())
		debugf("remote: ok script=%q out=%q err=%v took=%v", script, out, err, time.Since(start))
		return out, err
	case <-ctx.Done():
		// Abort: closing the session unblocks Wait(). This is the same
		// cancel pattern used by the SCP upload/download paths.
		_ = session.Close()
		<-waitErr
		out := strings.TrimSpace(buf.String())
		debugf("remote: TIMEOUT script=%q out=%q took=%v", script, out, time.Since(start))
		return out, ctx.Err()
	}
}
