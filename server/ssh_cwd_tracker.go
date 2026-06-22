package server

import (
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
	dst    io.Writer
	cache  *remoteCwdCache
	mu     sync.Mutex
	state  int
	oscBuf []byte
}

const (
	oscStateNormal = iota
	oscStateEsc
	oscStateBody
	oscStateBodyEsc
)

func newRemoteOutputCwdWriter(dst io.Writer, cache *remoteCwdCache) io.Writer {
	if dst == nil {
		return nil
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

	for _, b := range p {
		switch w.state {
		case oscStateNormal:
			if b == 0x1b {
				w.state = oscStateEsc
			}
		case oscStateEsc:
			if b == ']' {
				w.state = oscStateBody
				w.oscBuf = w.oscBuf[:0]
			} else {
				w.state = oscStateNormal
			}
		case oscStateBody:
			switch b {
			case 0x07:
				w.consumeOSC()
				w.state = oscStateNormal
			case 0x1b:
				w.state = oscStateBodyEsc
			default:
				if len(w.oscBuf) < 4096 {
					w.oscBuf = append(w.oscBuf, b)
				}
			}
		case oscStateBodyEsc:
			if b == '\\' {
				w.consumeOSC()
				w.state = oscStateNormal
				continue
			}
			if len(w.oscBuf) < 4096 {
				w.oscBuf = append(w.oscBuf, 0x1b)
				if b != 0x07 {
					w.oscBuf = append(w.oscBuf, b)
				}
			}
			if b == 0x07 {
				w.consumeOSC()
				w.state = oscStateNormal
			} else {
				w.state = oscStateBody
			}
		}
	}
}

func (w *remoteOutputCwdWriter) consumeOSC() {
	if dir, ok := parseOSC7Dir(string(w.oscBuf)); ok {
		w.cache.Set(dir)
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
