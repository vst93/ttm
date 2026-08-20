package server

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

// ── End-to-end SCP transfer tests ────────────────────────────────────────────
//
// These wire ttm's SCP client against the real scp(1) binary through an
// in-process SSH server, so the whole path is exercised: the remote command
// string built by remoteSCPCommand, the shell that parses it, and the SCP
// protocol over the session's stdin/stdout.
//
// This is the layer that regressed when the command was wrapped as
// `printf ... | sh`: the wrapper replaced the shell's stdin with its own pipe,
// so scp read EOF instead of protocol bytes and every transfer failed with
// "EOF" after 0 bytes.

// startLocalSSHServer starts an SSH server on localhost that executes each
// exec request with `sh -c`, wiring the session channel to the command's
// stdin/stdout. It returns a connected client.
func startLocalSSHServer(t *testing.T) *ssh.Client {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("test relies on a POSIX shell and scp(1)")
	}
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no POSIX shell available")
	}
	if _, err := exec.LookPath("scp"); err != nil {
		t.Skip("scp not available")
	}

	signer, err := ssh.ParsePrivateKey([]byte(generatePrivateKeyPEM(t)))
	if err != nil {
		t.Fatalf("parse host key: %v", err)
	}
	serverConfig := &ssh.ServerConfig{NoClientAuth: true}
	serverConfig.AddHostKey(signer)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	var serving sync.WaitGroup
	serving.Add(1)
	go func() {
		defer serving.Done()
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		sshConn, chans, reqs, hsErr := ssh.NewServerConn(conn, serverConfig)
		if hsErr != nil {
			_ = conn.Close()
			return
		}
		defer sshConn.Close()
		go ssh.DiscardRequests(reqs)
		for newChan := range chans {
			if newChan.ChannelType() != "session" {
				_ = newChan.Reject(ssh.UnknownChannelType, "unsupported")
				continue
			}
			channel, chanReqs, acceptChanErr := newChan.Accept()
			if acceptChanErr != nil {
				return
			}
			go serveExecSession(channel, chanReqs)
		}
	}()
	t.Cleanup(func() {
		_ = listener.Close()
		serving.Wait()
	})

	client, err := ssh.Dial("tcp", listener.Addr().String(), &ssh.ClientConfig{
		User:            "tester",
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	})
	if err != nil {
		t.Fatalf("dial local ssh server: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

// serveExecSession answers a single session channel: it runs the exec payload
// through `sh -c`, exactly as sshd hands the command to the user's login shell.
func serveExecSession(channel ssh.Channel, reqs <-chan *ssh.Request) {
	defer channel.Close()
	for req := range reqs {
		if req.Type != "exec" {
			if req.WantReply {
				_ = req.Reply(false, nil)
			}
			continue
		}
		if len(req.Payload) < 4 {
			if req.WantReply {
				_ = req.Reply(false, nil)
			}
			return
		}
		length := binary.BigEndian.Uint32(req.Payload[:4])
		if int(length)+4 > len(req.Payload) {
			if req.WantReply {
				_ = req.Reply(false, nil)
			}
			return
		}
		command := string(req.Payload[4 : 4+length])
		if req.WantReply {
			_ = req.Reply(true, nil)
		}

		cmd := exec.Command("sh", "-c", command)
		// stdin must be a real *os.File so exec does not wait for the channel to
		// reach EOF before Wait() returns. Real sshd closes the channel as soon
		// as the command exits, no matter what the client still has to send.
		pr, pw, err := os.Pipe()
		if err != nil {
			return
		}
		go func() {
			_, _ = io.Copy(pw, channel)
			_ = pw.Close()
		}()
		cmd.Stdin = pr
		cmd.Stdout = channel
		cmd.Stderr = channel.Stderr()
		status := 0
		if err := cmd.Run(); err != nil {
			status = 1
		}
		_ = pr.Close()
		var payload [4]byte
		binary.BigEndian.PutUint32(payload[:], uint32(status))
		_, _ = channel.SendRequest("exit-status", false, payload[:])
		return
	}
}

func TestSCPDownloadFileEndToEnd(t *testing.T) {
	client := startLocalSSHServer(t)

	remoteDir := t.TempDir()
	// A name that exercises quoting: spaces, CJK, and shell metacharacters.
	remoteName := "报表 $(id) 0727.xlsx"
	content := []byte("spreadsheet-bytes")
	if err := os.WriteFile(filepath.Join(remoteDir, remoteName), content, 0600); err != nil {
		t.Fatal(err)
	}

	localDir := t.TempDir()
	got, err := scpDownloadFile(context.Background(), client,
		filepath.Join(remoteDir, remoteName), filepath.Join(localDir, remoteName), nil)
	if err != nil {
		t.Fatalf("scpDownloadFile: %v", err)
	}
	data, err := os.ReadFile(got)
	if err != nil {
		t.Fatalf("read downloaded file: %v", err)
	}
	if string(data) != string(content) {
		t.Fatalf("downloaded content = %q, want %q", data, content)
	}
}

func TestSCPUploadFileEndToEnd(t *testing.T) {
	client := startLocalSSHServer(t)

	localDir := t.TempDir()
	name := "上传 'quoted' 文件.txt"
	content := []byte("payload-bytes")
	localPath := filepath.Join(localDir, name)
	if err := os.WriteFile(localPath, content, 0600); err != nil {
		t.Fatal(err)
	}

	remoteDir := t.TempDir()
	if err := scpUploadFile(context.Background(), client, remoteDir, localPath, nil); err != nil {
		t.Fatalf("scpUploadFile: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(remoteDir, name))
	if err != nil {
		t.Fatalf("read uploaded file: %v", err)
	}
	if string(data) != string(content) {
		t.Fatalf("uploaded content = %q, want %q", data, content)
	}
}

func TestSCPDownloadDirEndToEnd(t *testing.T) {
	client := startLocalSSHServer(t)

	remoteParent := t.TempDir()
	remoteDir := filepath.Join(remoteParent, "数据 目录")
	if err := os.MkdirAll(filepath.Join(remoteDir, "nested"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(remoteDir, "nested", "a.txt"), []byte("a"), 0600); err != nil {
		t.Fatal(err)
	}

	localDir := t.TempDir()
	progress := &dirProgress{}
	if err := scpDownloadDir(context.Background(), client, remoteDir, localDir, progress); err != nil {
		t.Fatalf("scpDownloadDir: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(localDir, "数据 目录", "nested", "a.txt"))
	if err != nil {
		t.Fatalf("read downloaded tree: %v", err)
	}
	if string(data) != "a" {
		t.Fatalf("downloaded content = %q, want %q", data, "a")
	}
}

// Probe commands go through the printf|sh wrapper, which deliberately feeds the
// script over stdin — fine, because probes read nothing from it. This checks the
// encoding survives a real shell round-trip, including quotes and UTF-8.
func TestRemoteRunShEndToEnd(t *testing.T) {
	client := startLocalSSHServer(t)

	for _, tt := range []struct{ script, want string }{
		{`printf '%s' "ok"`, "ok"},
		{`printf '%s' "it's 中文 \$HOME"`, "it's 中文 $HOME"},
		{"echo " + shQuote("`touch pwned`"), "`touch pwned`"},
	} {
		got, err := remoteRunSh(client, tt.script, 10*time.Second)
		if err != nil {
			t.Fatalf("remoteRunSh(%q): %v", tt.script, err)
		}
		if got != tt.want {
			t.Errorf("remoteRunSh(%q) = %q, want %q", tt.script, got, tt.want)
		}
	}
}
