package server

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"

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

func (c *defaultClient) Login() error {
	host := c.node.Host
	port := strconv.Itoa(c.node.Port)

	var client *ssh.Client
	client, err := ssh.Dial("tcp", net.JoinHostPort(host, port), c.clientConfig)
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
				client, err = ssh.Dial("tcp", net.JoinHostPort(host, port), c.clientConfig)
			}
		}
	}
	if err != nil {
		return err
	}
	defer client.Close()
	defer func() {
		// 清屏
		// fmt.Println("\033[2J")
		// 清屏且将光标置顶
		fmt.Println("\033[2J\033[0;0H")
		AM.View()
	}()

	session, err := client.NewSession()
	if err != nil {
		return err
	}
	defer session.Close()

	fd := int(os.Stdin.Fd())
	state, err := terminal.MakeRaw(fd)
	if err != nil {
		return err
	}
	defer terminal.Restore(fd, state)

	//changed fd to int(os.Stdout.Fd()) becaused terminal.GetSize(fd) doesn't work in Windows
	//refrence: https://github.com/golang/go/issues/20388
	w, h, err := terminal.GetSize(int(os.Stdout.Fd()))

	if err != nil {
		return err
	}

	modes := ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}
	err = session.RequestPty("xterm", h, w, modes)
	if err != nil {
		return err
	}

	session.Stdout = os.Stdout
	session.Stderr = os.Stderr
	stdinPipe, err := session.StdinPipe()
	if err != nil {
		return err
	}

	err = session.Shell()
	if err != nil {
		return err
	}

	// 清屏且将光标置顶
	fmt.Println("\033[2J\033[0;0H")

	// then callback
	for i := range c.node.CallbackShells {
		shell := c.node.CallbackShells[i]
		time.Sleep(shell.Delay * time.Millisecond)
		stdinPipe.Write([]byte(shell.Cmd + "\r"))
	}

	// change stdin to user
	go func() {
		_, err = io.Copy(stdinPipe, os.Stdin)
		session.Close()
	}()

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

	session.Wait()
	return nil
}

func genSSHConfig(node *SSHConfig) (*defaultClient, error) {
	var err error

	var authMethods []ssh.AuthMethod

	var pemBytes []byte
	if node.PrivateKey != "" {
		pemBytes = []byte(node.PrivateKey)
	}
	var signer ssh.Signer
	if node.Passphrase != "" {
		signer, err = ssh.ParsePrivateKeyWithPassphrase(pemBytes, []byte(node.Passphrase))
		if err != nil {
			return nil, err
		} else {
			authMethods = append(authMethods, ssh.PublicKeys(signer))
		}
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
