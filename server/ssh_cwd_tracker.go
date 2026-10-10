package server

import (
	"bytes"
	"io"
	"net/url"
	"path"
	"strings"
	"sync"
)

type remoteCwdCache struct {
	mu   sync.RWMutex
	dir  string
	prev string
	home string
}

func newRemoteCwdCache() *remoteCwdCache {
	return &remoteCwdCache{}
}

func (c *remoteCwdCache) Get() (string, bool) {
	if c == nil {
		return "", false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.dir == "" {
		return "", false
	}
	return c.dir, true
}

func (c *remoteCwdCache) Set(dir string) {
	if c == nil {
		return
	}
	dir = normalizeRemoteDir(dir)
	if dir == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.dir != "" && c.dir != dir {
		c.prev = c.dir
	}
	c.dir = dir
}

func (c *remoteCwdCache) SetHome(dir string) {
	if c == nil {
		return
	}
	dir = normalizeRemoteDir(dir)
	if dir == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.home == "" {
		c.home = dir
	}
	if c.dir == "" {
		c.dir = dir
	}
}

func (c *remoteCwdCache) ObserveCommand(line string) {
	if c == nil {
		return
	}
	line = strings.TrimSpace(line)
	if line == "" || strings.ContainsAny(line, "|;&<>()`") {
		return
	}
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return
	}

	switch fields[0] {
	case "cd":
		target := "~"
		if len(fields) >= 2 {
			if fields[1] == "--" {
				if len(fields) < 3 {
					return
				}
				target = fields[2]
			} else {
				target = fields[1]
			}
		}
		c.applyCd(target)
	case "pushd":
		if len(fields) < 2 {
			return
		}
		c.applyCd(fields[1])
	case "popd":
		c.mu.Lock()
		defer c.mu.Unlock()
		if c.prev == "" {
			return
		}
		c.dir, c.prev = c.prev, c.dir
	}
}

func (c *remoteCwdCache) applyCd(target string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	next := c.resolveLocked(target)
	if next == "" || next == c.dir {
		return
	}
	if c.dir != "" {
		c.prev = c.dir
	}
	c.dir = next
}

func (c *remoteCwdCache) resolveLocked(target string) string {
	target = strings.TrimSpace(target)
	if target == "" || target == "~" {
		return c.home
	}
	if target == "-" {
		return c.prev
	}
	if strings.HasPrefix(target, "~/") {
		if c.home == "" {
			return ""
		}
		return normalizeRemoteDir(path.Join(c.home, strings.TrimPrefix(target, "~/")))
	}
	if strings.HasPrefix(target, "/") {
		return normalizeRemoteDir(target)
	}
	if c.dir == "" {
		return ""
	}
	return normalizeRemoteDir(path.Join(c.dir, target))
}

func normalizeRemoteDir(dir string) string {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return ""
	}
	if !strings.HasPrefix(dir, "/") {
		return ""
	}
	return path.Clean(dir)
}

type remoteOutputCwdWriter struct {
	dst       io.Writer
	cache     *remoteCwdCache
	mu        sync.Mutex
	state     int
	oscBuf    []byte
	ignoreOSC bool
}

const (
	oscStateNormal = iota
	oscStateEsc
	oscStateBody
	oscStateBodyEsc
)

func newRemoteOutputCwdWriter(dst io.Writer, cache *remoteCwdCache) io.Writer {
	// Without tracking, return the destination itself: no wrapper, parser or
	// lock on the output path. Used by the SSH passthrough mode.
	if dst == nil || cache == nil {
		return dst
	}
	return &remoteOutputCwdWriter{dst: dst, cache: cache}
}

func (w *remoteOutputCwdWriter) Write(p []byte) (int, error) {
	if w == nil || w.dst == nil {
		return len(p), nil
	}
	w.parse(p)
	return w.dst.Write(p)
}

func (w *remoteOutputCwdWriter) parse(p []byte) {
	if w == nil || w.cache == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()

	for len(p) > 0 {
		switch w.state {
		case oscStateNormal:
			// Most TUI traffic is text and CSI (colors/cursor movement), not
			// OSC. Search whole spans instead of running a state machine on
			// every output byte. Preserve a trailing ESC across SSH reads.
			i := bytes.Index(p, []byte("\x1b]"))
			if i < 0 {
				if p[len(p)-1] == 0x1b {
					w.state = oscStateEsc
				}
				return
			}
			p = p[i+2:]
			w.state = oscStateBody
			w.oscBuf = w.oscBuf[:0]
			w.ignoreOSC = false
		case oscStateEsc:
			b := p[0]
			p = p[1:]
			if b == ']' {
				w.state = oscStateBody
				w.oscBuf = w.oscBuf[:0]
				w.ignoreOSC = false
			} else if b != 0x1b {
				w.state = oscStateNormal
			}
		case oscStateBody:
			// IndexByte uses the optimized byte-search implementation. Limit
			// the BEL search to the span before ESC, whichever comes first.
			i := bytes.IndexByte(p, 0x1b)
			end := len(p)
			if i >= 0 {
				end = i
			}
			if bell := bytes.IndexByte(p[:end], 0x07); bell >= 0 {
				i = bell
			}
			if i < 0 {
				w.appendOSC(p)
				return
			}
			w.appendOSC(p[:i])
			if p[i] == 0x07 {
				w.consumeOSC()
				w.state = oscStateNormal
			} else {
				w.state = oscStateBodyEsc
			}
			p = p[i+1:]
		case oscStateBodyEsc:
			b := p[0]
			p = p[1:]
			if b == '\\' {
				w.consumeOSC()
				w.state = oscStateNormal
				continue
			}
			w.appendOSC([]byte{0x1b})
			if b == 0x07 {
				w.consumeOSC()
				w.state = oscStateNormal
			} else {
				w.appendOSC([]byte{b})
				w.state = oscStateBody
			}
		}
	}
}

// Only retain OSC 7 file URLs. Titles, hyperlinks and clipboard/image payloads
// are forwarded untouched but need no allocation or string conversion here.
func (w *remoteOutputCwdWriter) appendOSC(p []byte) {
	if w.ignoreOSC {
		return
	}
	const prefix = "7;file://"
	if len(w.oscBuf) < len(prefix) {
		n := min(len(p), len(prefix)-len(w.oscBuf))
		if !bytes.Equal(p[:n], []byte(prefix[len(w.oscBuf):len(w.oscBuf)+n])) {
			w.ignoreOSC = true
		}
	}
	// Reject oversized OSCs rather than caching a silently truncated path.
	if w.ignoreOSC || len(w.oscBuf)+len(p) > 4096 {
		w.ignoreOSC = true
		w.oscBuf = w.oscBuf[:0]
		return
	}
	w.oscBuf = append(w.oscBuf, p...)
}

func (w *remoteOutputCwdWriter) consumeOSC() {
	if !w.ignoreOSC {
		if dir, ok := parseOSC7Dir(string(w.oscBuf)); ok {
			w.cache.Set(dir)
		}
	}
	w.oscBuf = w.oscBuf[:0]
}

func parseOSC7Dir(body string) (string, bool) {
	if !strings.HasPrefix(body, "7;file://") {
		return "", false
	}
	rawURL := strings.TrimPrefix(body, "7;")
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme != "file" {
		return "", false
	}
	dir := u.Path
	if dir == "" {
		return "", false
	}
	if decoded, err := url.PathUnescape(dir); err == nil {
		dir = decoded
	}
	dir = normalizeRemoteDir(dir)
	if dir == "" {
		return "", false
	}
	return dir, true
}

type remoteShellInputTracker struct {
	cache *remoteCwdCache
	mu    sync.Mutex
	line  []byte
}

func newRemoteShellInputTracker(cache *remoteCwdCache) *remoteShellInputTracker {
	return &remoteShellInputTracker{cache: cache}
}

func (t *remoteShellInputTracker) Observe(data []byte) {
	if t == nil || t.cache == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()

	for _, b := range data {
		switch b {
		case '\r', '\n':
			t.commitLocked()
			t.line = t.line[:0]
		case 0x08, 0x7f:
			if len(t.line) > 0 {
				t.line = t.line[:len(t.line)-1]
			}
		case 0x03, 0x15, 0x1b:
			t.line = t.line[:0]
		default:
			if b >= 0x20 && len(t.line) < 4096 {
				t.line = append(t.line, b)
			}
		}
	}
}

func (t *remoteShellInputTracker) commitLocked() {
	line := strings.TrimSpace(string(t.line))
	if line == "" {
		return
	}
	t.cache.ObserveCommand(line)
}
