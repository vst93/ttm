package server

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/muesli/cancelreader"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/terminal"
)

var (
	DefaultCiphers = []string{
		"aes128-ctr",
		"aes192-ctr",
		"aes256-ctr",
		"aes128-gcm@openssh.com",
		"chacha20-poly1305@openssh.com",
		"arcfour256",
		"arcfour128",
		"arcfour",
		"aes128-cbc",
		"3des-cbc",
		"blowfish-cbc",
		"cast128-cbc",
		"aes192-cbc",
		"aes256-cbc",
	}
)

// dialWithTrace performs a TCP dial + SSH handshake with automatic retry on
// EOF (handles transient FRP tunnel instability). Every call creates a fresh
// TCP + SSH connection — no reuse.
func dialWithTrace(ctx context.Context, addr string, config *ssh.ClientConfig) (*ssh.Client, error) {
	dialer := &net.Dialer{
		Timeout: config.Timeout,
	}

	// --- SSH Handshake with FRP EOF retry ---
	// FRP tunnel can be momentarily unstable during establishment, causing
	// the banner read to return EOF. A single retry with a fresh TCP
	// connection resolves this.
	var (
		tc      net.Conn
		sshConn ssh.Conn
		chans   <-chan ssh.NewChannel
		reqs    <-chan *ssh.Request
		err     error
	)

	for attempt := 0; attempt < 2; attempt++ {
		// Create a per-attempt context so that if the first handshake
		// consumed most of the parent deadline, the retry still gets a
		// reasonable window.
		attemptCtx, attemptCancel := context.WithCancel(ctx)

		// TCP dial — use a per-attempt deadline from the remaining time.
		if attempt > 0 {
			// Wait briefly for FRP tunnel to re-establish
			select {
			case <-time.After(500 * time.Millisecond):
			case <-attemptCtx.Done():
				attemptCancel()
				return nil, fmt.Errorf("retry cancelled: %w", attemptCtx.Err())
			}
		}

		var dialErr error
		tc, dialErr = dialer.DialContext(attemptCtx, "tcp", addr)
		if dialErr != nil {
			attemptCancel()
			if attempt == 0 {
				return nil, fmt.Errorf("dial TCP %s: %w", addr, dialErr)
			}
			return nil, fmt.Errorf("dial TCP retry %s: %w", addr, dialErr)
		}

		// Set a per-attempt deadline so the SSH handshake doesn't
		// overrun the remaining context time.  We do NOT use a parallel
		// goroutine to call tc.Close() — closing the connection from
		// another goroutine while ssh.NewClientConn is reading it causes
		// a TOCTOU race that produces a spurious EOF error, making it
		// indistinguishable from a genuine FRP-level EOF.
		if d, ok := attemptCtx.Deadline(); ok {
			_ = tc.SetDeadline(d)
		} else if config.Timeout > 0 {
			_ = tc.SetDeadline(time.Now().Add(config.Timeout))
		}

		sshConn, chans, reqs, err = ssh.NewClientConn(tc, addr, config)
		attemptCancel() // release context resources (does NOT close tc)

		if err != nil {
			tc.Close()

			if attempt == 0 && strings.Contains(err.Error(), "EOF") {
				// Transient FRP EOF — retry with fresh TCP connection
				continue
			}
			return nil, fmt.Errorf("SSH handshake %s: %w", addr, err)
		}

		// Success — clear deadline so that subsequent operations
		// (session creation, shell start) are not time-restricted.
		_ = tc.SetDeadline(time.Time{})
		break
	}

	return ssh.NewClient(sshConn, chans, reqs), nil
}

type defaultClient struct {
	clientConfig *ssh.ClientConfig
	node         *SSHConfig
}

type CallbackShell struct {
	Cmd   string        `yaml:"cmd"`
	Delay time.Duration `yaml:"delay"`
}
type SSHConfig struct {
	Host           string           `yaml:"host"`
	User           string           `yaml:"user"`
	Port           int              `yaml:"port"`
	PrivateKey     string           `yaml:"privatekey"`
	Passphrase     string           `yaml:"passphrase"`
	Password       string           `yaml:"password"`
	CallbackShells []*CallbackShell `yaml:"callback-shells"`
}

func isStdinCopyErrBenign(err error) bool {
	if err == nil {
		return true
	}
	if errors.Is(err, io.EOF) {
		return true
	}
	return errors.Is(err, cancelreader.ErrCanceled)
}

// tmuxPassthrough wraps a terminal escape sequence in the tmux passthrough
// wrapper (\x1bPtmux;...\x1b\) when ttm itself runs inside a tmux session
// (TMUX env var set). Without the wrapper, tmux would swallow the sequence
// instead of forwarding it to the outer terminal. Inside the wrapper, every
// ESC byte must be doubled (\x1b\x1b). When not running under tmux, the
// sequence is returned unchanged.
func tmuxPassthrough(seq string) string {
	if os.Getenv("TMUX") == "" {
		return seq
	}
	escaped := strings.ReplaceAll(seq, "\x1b", "\x1b\x1b")
	return "\x1bPtmux;" + escaped + "\x1b\\"
}

func (c *defaultClient) ProbeConnection(timeout time.Duration) error {
	host := c.node.Host
	port := strconv.Itoa(c.node.Port)

	cfg := *c.clientConfig
	if timeout > 0 {
		cfg.Timeout = timeout
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	client, err := dialWithTrace(ctx, net.JoinHostPort(host, port), &cfg)
	if err != nil {
		return fmt.Errorf("dial SSH server: %w", err)
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("open SSH session: %w", err)
	}
	defer session.Close()

	return nil
}

func sshTerm() string {
	term := strings.TrimSpace(os.Getenv("TERM"))
	if term == "" || term == "dumb" {
		return "xterm-256color"
	}
	return term
}

func setSessionEnv(session *ssh.Session) {
	// TERM is forwarded via RequestPty's term parameter, which is the
	// standard SSH mechanism. Setting it again via Setenv is redundant
	// and can cause some SSH servers to close the session if they reject
	// env requests or don't support them.
	//
	// Locale vars (LANG, LC_ALL, LC_CTYPE) and program-specific vars
	// (TERM_PROGRAM, COLORTERM) are intentionally skipped because:
	//   1. The remote server may not have those locales installed,
	//      causing the shell to crash immediately on startup.
	//   2. Non-standard vars can confuse remote tools.
	//
	// Users can set environment variables in their remote shell profile
	// (~/.bashrc, ~/.zshrc, etc.) instead.
}

func (c *defaultClient) Login() error {
	host := c.node.Host
	port := strconv.Itoa(c.node.Port)
	fmt.Printf("%s\n", fmt.Sprintf(AM.t("connecting %s@%s:%s ...", "正在连接 %s@%s:%s ..."), c.clientConfig.User, host, port))

	ctx, cancel := context.WithTimeout(context.Background(), c.clientConfig.Timeout)
	defer cancel()

	client, err := dialWithTrace(ctx, net.JoinHostPort(host, port), c.clientConfig)
	if err != nil {
		msg := err.Error()
		// use terminal password retry
		if strings.Contains(msg, "no supported methods remain") && !strings.Contains(msg, "password") {
			fmt.Printf("%s@%s's password:", c.clientConfig.User, host)
			var b []byte
			b, err = terminal.ReadPassword(int(syscall.Stdin))
			if err == nil {
				p := string(b)
				if p != "" {
					c.clientConfig.Auth = append(c.clientConfig.Auth, ssh.Password(p))
				}
				fmt.Println()

				ctx2, cancel2 := context.WithTimeout(context.Background(), c.clientConfig.Timeout)
				defer cancel2()
				client, err = dialWithTrace(ctx2, net.JoinHostPort(host, port), c.clientConfig)
			}
		}
	}
	if err != nil {
		return fmt.Errorf("dial SSH server: %w", err)
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("open SSH session: %w", err)
	}
	defer session.Close()

	// Disable alternate scroll mode (DEC 1007) before entering raw mode.
	// BubbleTea's WithAltScreen() enables this implicitly when entering the alt
	// screen, but exiting the alt screen (via tea.Exec -> ReleaseTerminal) does
	// not always disable it. If it leaks into the raw SSH session, mouse wheel
	// and touchpad scroll events are converted to Up/Down arrow key sequences,
	// which get forwarded to the remote shell and interfere with TUI programs.
	// We re-enable it after the session ends so BubbleTea's alt screen works
	// correctly when it re-enters.
	fmt.Print(tmuxPassthrough("\x1b[?1007l"))

	fd := int(os.Stdin.Fd())
	state, err := terminal.MakeRaw(fd)
	if err != nil {
		return fmt.Errorf("enable local raw terminal mode: %w", err)
	}

	// Defer terminal cleanup: restore termios, re-enable alt scroll, clear screen.
	// This runs on ALL exit paths (normal, error, panic) ensuring the terminal
	// is always left in a consistent state for BubbleTea's RestoreTerminal().
	defer func() {
		terminal.Restore(fd, state)
		fmt.Print(tmuxPassthrough("\x1b[?1007h"))
		fmt.Print(tmuxPassthrough("\033[2J\033[0;0H\033[?25h"))
	}()

	// Read the local VERASE character before MakeRaw overrides it,
	// so we can forward it to the remote PTY. This ensures Backspace/Delete
	// works correctly on servers whose termios expects a different erase
	// character than what MakeRaw sets locally (e.g. ^H vs ^?).
	eraseChar := getLocalEraseChar(fd)

	//changed fd to int(os.Stdout.Fd()) becaused terminal.GetSize(fd) doesn't work in Windows
	//refrence: https://github.com/golang/go/issues/20388
	w, h, err := terminal.GetSize(int(os.Stdout.Fd()))

	if err != nil {
		return fmt.Errorf("read local terminal size: %w", err)
	}

	setSessionEnv(session)
	modes := ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 38400,
		ssh.TTY_OP_OSPEED: 38400,
	}
	if eraseChar != 0 {
		modes[ssh.VERASE] = uint32(eraseChar)
	}
	err = session.RequestPty(sshTerm(), h, w, modes)
	if err != nil {
		return fmt.Errorf("request remote PTY: %w", err)
	}

	cwdCache := newRemoteCwdCache()
	stdoutWriter := newRemoteOutputCwdWriter(os.Stdout, cwdCache)
	session.Stdout = stdoutWriter
	session.Stderr = stdoutWriter
	stdinPipe, err := session.StdinPipe()
	if err != nil {
		return fmt.Errorf("open remote stdin pipe: %w", err)
	}

	err = session.Shell()
	if err != nil {
		return fmt.Errorf("start remote shell: %w", err)
	}

	// then callback
	if c.node.CallbackShells != nil {
		for i := range c.node.CallbackShells {
			shell := c.node.CallbackShells[i]
			time.Sleep(shell.Delay * time.Millisecond)
			stdinPipe.Write([]byte(shell.Cmd + "\r"))
		}
	}

	connInfo := buildSSHConnInfo(client, c.node)
	currentLocale := AM.locale

	handleUploadTrigger := func(reader io.Reader, idleChecked bool) {
		debugf("trigger: double-tap detected")
		if !shouldTriggerUploadHint() {
			debugf("trigger: suppressed by debounce")
			return
		}

		// Open the dialog TTY up front (with a stdout fallback) and show
		// instant feedback, so the user sees the double-tap registered even
		// while remote probes — which can be slow on fish / oh-my-zsh — run.
		tty, closeTTY := openDialogTTY()
		defer closeTTY()
		debugf("trigger: tty opened")

		// Disable kitty keyboard mode locally for the whole dialog+upload so
		// Esc/Ctrl+C arrive as legacy 0x1b/0x03 (cancel works) instead of
		// kitty CSI-u sequences. Restored on exit — preserves the remote's
		// flag state via the push/pop stack.
		kittyPushOff(tty)
		defer kittyPop(tty)
		debugf("trigger: kitty keyboard disabled for dialog")

		openingMsg := localeT(currentLocale, "opening transfer...", "打开传输菜单...")
		fmt.Fprintf(tty, "\r\n\x1b[2m⋯ TTM: %s\x1b[0m\r\n", openingMsg)

		// Best-effort idle check. In the normal double-press flow this was
		// already done by the interceptor; avoid probing twice because some
		// fish / oh-my-zsh setups make each exec-session probe noticeably slow.
		if !idleChecked {
			idle := isRemoteIdle(client)
			debugf("trigger: isRemoteIdle=%v", idle)
			if !idle {
				busyMsg := localeT(currentLocale, "remote busy (fullscreen program detected), aborted", "远程忙碌（检测到全屏程序），已中止")
				fmt.Fprintf(tty, "\x1b[33m%s\x1b[0m\r\n", busyMsg)
				printEndBanner(tty, currentLocale)
				return
			}
		}

		uploadWithDialog(reader, cwdCache, client, connInfo, currentLocale, tty)
	}

	stdinCopyDone, cancelStdinCopy, err := startStdinCopyWithIntercept(stdinPipe, client, cwdCache, handleUploadTrigger)
	if err != nil {
		return fmt.Errorf("open local stdin reader: %w", err)
	}
	defer cancelStdinCopy()

	// interval get terminal size
	// fix resize issue
	go func() {
		var (
			ow = w
			oh = h
		)
		for {
			cw, ch, err := terminal.GetSize(fd)
			if err != nil {
				break
			}

			if cw != ow || ch != oh {
				err = session.WindowChange(ch, cw)
				if err != nil {
					break
				}
				ow = cw
				oh = ch
			}
			time.Sleep(time.Second)
		}
	}()

	// send keepalive
	go func() {
		for {
			time.Sleep(time.Second * 10)
			client.SendRequest("keepalive@openssh.com", false, nil)
		}
	}()

	waitErr := session.Wait()
	cancelStdinCopy()
	copyErr := <-stdinCopyDone
	if !isStdinCopyErrBenign(copyErr) {
		return copyErr
	}
	if waitErr != nil {
		return fmt.Errorf("remote shell exited: %w", waitErr)
	}

	return nil
}

func genSSHConfig(node *SSHConfig) (*defaultClient, error) {
	var err error

	var authMethods []ssh.AuthMethod

	var pemBytes []byte
	if node.PrivateKey != "" {
		pemBytes = []byte(node.PrivateKey)
	}
	if len(pemBytes) > 0 {
		var signer ssh.Signer
		if node.Passphrase != "" {
			signer, err = ssh.ParsePrivateKeyWithPassphrase(pemBytes, []byte(node.Passphrase))
		} else {
			signer, err = ssh.ParsePrivateKey(pemBytes)
		}
		if err != nil {
			return nil, err
		}
		authMethods = append(authMethods, ssh.PublicKeys(signer))
	}

	password := node.Password
	if password != "" {
		authMethods = append(authMethods, ssh.Password(password))
	}

	authMethods = append(authMethods, ssh.KeyboardInteractive(func(user, instruction string, questions []string, echos []bool) ([]string, error) {
		answers := make([]string, 0, len(questions))
		for i, q := range questions {
			fmt.Print(q)
			if echos[i] {
				scan := bufio.NewScanner(os.Stdin)
				if scan.Scan() {
					answers = append(answers, scan.Text())
				}
				err := scan.Err()
				if err != nil {
					return nil, err
				}
			} else {
				b, err := terminal.ReadPassword(int(syscall.Stdin))
				if err != nil {
					return nil, err
				}
				fmt.Println()
				answers = append(answers, string(b))
			}
		}
		return answers, nil
	}))

	config := &ssh.ClientConfig{
		User:            node.User,
		Auth:            authMethods,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         time.Second * 10,
	}

	config.SetDefaults()
	config.Ciphers = append(config.Ciphers, DefaultCiphers...)

	return &defaultClient{
		clientConfig: config,
		node:         node,
	}, nil
}
