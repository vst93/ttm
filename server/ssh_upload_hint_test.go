package server

import (
	"bytes"
	"os"
	"testing"
	"time"
)

func TestBuildScpCommand(t *testing.T) {
	tests := []struct {
		name      string
		info      sshConnInfo
		remoteDir string
		localPath string
		wantCmd   string
	}{
		{
			name:      "basic",
			info:      sshConnInfo{User: "root", Host: "host", Port: 22},
			remoteDir: "/tmp",
			localPath: "/local/file.txt",
			wantCmd:   "scp -P 22 '/local/file.txt' 'root@host:/tmp/'",
		},
		{
			name:      "custom port",
			info:      sshConnInfo{User: "u", Host: "h", Port: 2222},
			remoteDir: "/home/u",
			localPath: "./file.tar.gz",
			wantCmd:   "scp -P 2222 './file.tar.gz' 'u@h:/home/u/'",
		},
		{
			name:      "defaults",
			info:      sshConnInfo{User: "", Host: "h", Port: 0},
			remoteDir: "~",
			localPath: "file",
			wantCmd:   "scp -P 22 'file' 'root@h:~/'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildScpCommand(tt.info, tt.remoteDir, tt.localPath)
			if got != tt.wantCmd {
				t.Errorf("buildScpCommand() = %q, want %q", got, tt.wantCmd)
			}
		})
	}
}

func TestBuildSSHConnInfo(t *testing.T) {
	node := &SSHConfig{
		Host: "server.example.com",
		Port: 2222,
		User: "admin",
	}
	info := buildSSHConnInfo(nil, node)
	if info.User != "admin" {
		t.Errorf("User = %q, want %q", info.User, "admin")
	}
	if info.Host != "server.example.com" {
		t.Errorf("Host = %q, want %q", info.Host, "server.example.com")
	}
	if info.Port != 2222 {
		t.Errorf("Port = %d, want %d", info.Port, 2222)
	}
}

func TestBuildSSHConnInfoDefaults(t *testing.T) {
	node := &SSHConfig{
		Host: "host",
		Port: 0,
		User: "",
	}
	info := buildSSHConnInfo(nil, node)
	if info.User != "root" {
		t.Errorf("User = %q, want %q", info.User, "root")
	}
	if info.Port != 22 {
		t.Errorf("Port = %d, want %d", info.Port, 22)
	}
}

func TestShouldTriggerUploadHint(t *testing.T) {
	uploadHintLastTrigger = time.Time{}

	if !shouldTriggerUploadHint() {
		t.Error("first call should return true")
	}

	if shouldTriggerUploadHint() {
		t.Error("immediate second call should return false (debounced)")
	}

	uploadHintLastTrigger = time.Now().Add(-uploadHintDebounceDuration - time.Millisecond)
	if !shouldTriggerUploadHint() {
		t.Error("call after debounce period should return true")
	}
}

func TestLocaleHelper(t *testing.T) {
	if got := localeT(localeEN, "hello", "你好"); got != "hello" {
		t.Errorf("EN: got %q, want %q", got, "hello")
	}
	if got := localeT(localeZH, "hello", "你好"); got != "你好" {
		t.Errorf("ZH: got %q, want %q", got, "你好")
	}
}

func TestCtrlGByteConstant(t *testing.T) {
	if ctrlGByte != 0x07 {
		t.Errorf("ctrlGByte = 0x%02x, want 0x07", ctrlGByte)
	}
}

func TestFormatFileSize(t *testing.T) {
	tests := []struct {
		bytes int64
		want  string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1048576, "1.0 MB"},
		{1073741824, "1.0 GB"},
		{5368709120, "5.0 GB"},
	}
	for _, tt := range tests {
		got := formatFileSize(tt.bytes)
		if got != tt.want {
			t.Errorf("formatFileSize(%d) = %q, want %q", tt.bytes, got, tt.want)
		}
	}
}

func TestCountFiles(t *testing.T) {
	// Create a temp directory structure.
	dir := t.TempDir()
	os.MkdirAll(dir+"/sub", 0755)
	os.WriteFile(dir+"/a.txt", []byte("hello"), 0644)
	os.WriteFile(dir+"/b.txt", []byte("world"), 0644)
	os.WriteFile(dir+"/sub/c.txt", []byte("!"), 0644)

	count, size, err := countFiles(dir)
	if err != nil {
		t.Fatalf("countFiles: %v", err)
	}
	if count != 3 {
		t.Errorf("count = %d, want 3", count)
	}
	if size != 11 { // 5+5+1
		t.Errorf("size = %d, want 11", size)
	}
}

func TestCountFilesEmpty(t *testing.T) {
	dir := t.TempDir()
	count, size, err := countFiles(dir)
	if err != nil {
		t.Fatalf("countFiles: %v", err)
	}
	if count != 0 || size != 0 {
		t.Errorf("count=%d size=%d, want 0,0", count, size)
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{500 * time.Millisecond, "<1s"},
		{5 * time.Second, "5s"},
		{65 * time.Second, "1m05s"},
		{125 * time.Second, "2m05s"},
	}
	for _, tt := range tests {
		got := formatDuration(tt.d)
		if got != tt.want {
			t.Errorf("formatDuration(%v) = %q, want %q", tt.d, got, tt.want)
		}
	}
}

func TestTtyProgressThrottling(t *testing.T) {
	var buf bytes.Buffer
	p := &ttyProgress{tty: &buf, total: 1000, loc: localeEN}

	// First write should render.
	p.Write(make([]byte, 100))
	if buf.Len() == 0 {
		t.Error("expected first write to render")
	}

	// Immediate second write should be throttled (no new output).
	prevLen := buf.Len()
	p.Write(make([]byte, 100))
	if buf.Len() != prevLen {
		t.Error("expected second write to be throttled")
	}
}

func TestParseRemoteStatOutput(t *testing.T) {
	cases := []struct {
		name    string
		out     string
		isDir   bool
		size    int64
		wantErr bool
	}{
		{name: "dir", out: "d", isDir: true},
		{name: "file", out: "f 123", size: 123},
		{name: "missing", out: "", wantErr: true},
		{name: "bad-size", out: "f nope", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			isDir, size, err := parseRemoteStatOutput(tc.out, "/tmp/x")
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error for %q", tc.out)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if isDir != tc.isDir || size != tc.size {
				t.Fatalf("got (%v,%d), want (%v,%d)", isDir, size, tc.isDir, tc.size)
			}
		})
	}
}
