package esxi

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// SSHClient runs commands on an ESXi host over SSH (esxcli / vim-cmd).
type SSHClient struct {
	addr       string
	port       int
	user       string
	pass       string
	knownHosts string
}

func NewSSHClient(addr string, port int, user, pass, knownHosts string) *SSHClient {
	if port == 0 {
		port = 22
	}
	return &SSHClient{addr: addr, port: port, user: user, pass: pass, knownHosts: knownHosts}
}

func (s *SSHClient) config() (*ssh.ClientConfig, string, error) {
	var hostKeyCallback ssh.HostKeyCallback
	if s.knownHosts != "" {
		if _, err := os.Stat(s.knownHosts); err == nil {
			// trust on first use against the persisted known_hosts
			cb, err := knownHostsCallback(s.knownHosts)
			if err != nil {
				return nil, "", err
			}
			hostKeyCallback = cb
		}
	}
	if hostKeyCallback == nil {
		hostKeyCallback = ssh.InsecureIgnoreHostKey()
	}
	auth := []ssh.AuthMethod{
		ssh.Password(s.pass),
		ssh.KeyboardInteractive(func(user, instruction string, questions []string, echos []bool) ([]string, error) {
			answers := make([]string, len(questions))
			for i := range questions {
				answers[i] = s.pass
			}
			return answers, nil
		}),
	}
	cfg := &ssh.ClientConfig{
		User:            s.user,
		Auth:            auth,
		HostKeyCallback: hostKeyCallback,
		Timeout:         15 * time.Second,
	}
	return cfg, net.JoinHostPort(s.addr, fmt.Sprintf("%d", s.port)), nil
}

func knownHostsCallback(path string) (ssh.HostKeyCallback, error) {
	return knownhosts.New(path)
}

// HostKeyLine connects to the host and returns its host key as an OpenSSH
// known_hosts line plus the key algorithm, so that another machine can be told
// to trust exactly the key this server sees.
func (s *SSHClient) HostKeyLine(ctx context.Context) (line, algorithm string, err error) {
	cfg, addr, err := s.config()
	if err != nil {
		return "", "", err
	}
	verify := cfg.HostKeyCallback
	var seen ssh.PublicKey
	cfg.HostKeyCallback = func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		seen = key
		return verify(hostname, remote, key)
	}
	// ESXi runs OpenSSH in FIPS mode: stick to algorithms its client accepts too.
	cfg.HostKeyAlgorithms = []string{ssh.KeyAlgoECDSA256, ssh.KeyAlgoECDSA384, ssh.KeyAlgoECDSA521, ssh.KeyAlgoRSASHA512, ssh.KeyAlgoRSASHA256}
	d := net.Dialer{Timeout: cfg.Timeout}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return "", "", fmt.Errorf("ssh dial %s: %w", addr, err)
	}
	defer conn.Close()
	sc, _, _, err := ssh.NewClientConn(conn, addr, cfg)
	if err != nil {
		return "", "", fmt.Errorf("ssh handshake %s: %w", addr, err)
	}
	defer sc.Close()
	return knownhosts.Line([]string{knownhosts.Normalize(addr)}, seen), seen.Type(), nil
}

// Run executes a command and returns combined output.
func (s *SSHClient) Run(ctx context.Context, cmd string) (string, error) {
	cfg, addr, err := s.config()
	if err != nil {
		return "", err
	}
	d := net.Dialer{Timeout: cfg.Timeout}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return "", fmt.Errorf("ssh dial %s: %w", addr, err)
	}
	defer conn.Close()
	sc, chans, reqs, err := ssh.NewClientConn(conn, addr, cfg)
	if err != nil {
		return "", fmt.Errorf("ssh handshake %s: %w", addr, err)
	}
	client := ssh.NewClient(sc, chans, reqs)
	defer client.Close()

	sess, err := client.NewSession()
	if err != nil {
		return "", err
	}
	defer sess.Close()
	done := make(chan struct{})
	if deadline, ok := ctx.Deadline(); ok {
		go func() {
			select {
			case <-time.After(time.Until(deadline)):
				sess.Close()
			case <-done:
			}
		}()
	}
	out, err := sess.CombinedOutput(cmd)
	close(done)
	if err != nil {
		return string(out), fmt.Errorf("ssh command %q: %w", cmd, err)
	}
	return string(out), nil
}

// RunWithStdin executes a command with the given stdin and returns combined output.
func (s *SSHClient) RunWithStdin(ctx context.Context, cmd string, r io.Reader) (string, error) {
	cfg, addr, err := s.config()
	if err != nil {
		return "", err
	}
	d := net.Dialer{Timeout: cfg.Timeout}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return "", fmt.Errorf("ssh dial %s: %w", addr, err)
	}
	defer conn.Close()
	sc, chans, reqs, err := ssh.NewClientConn(conn, addr, cfg)
	if err != nil {
		return "", fmt.Errorf("ssh handshake %s: %w", addr, err)
	}
	client := ssh.NewClient(sc, chans, reqs)
	defer client.Close()

	sess, err := client.NewSession()
	if err != nil {
		return "", err
	}
	defer sess.Close()
	done := make(chan struct{})
	if deadline, ok := ctx.Deadline(); ok {
		go func() {
			select {
			case <-time.After(time.Until(deadline)):
				sess.Close()
			case <-done:
			}
		}()
	}
	sess.Stdin = r
	var buf bytes.Buffer
	sess.Stdout = &buf
	sess.Stderr = &buf
	err = sess.Run(cmd)
	close(done)
	if err != nil {
		return buf.String(), fmt.Errorf("ssh command %q: %w", cmd, err)
	}
	return buf.String(), nil
}

// esxcliAllowlist defines the esxcli/vim-cmd prefixes permitted via the API.
var esxcliAllowlist = []string{
	"esxcli system version",
	"esxcli system settings",
	"esxcli system health",
	"esxcli hardware cpu",
	"esxcli hardware memory",
	"esxcli hardware nic list",
	"esxcli storage filesystem list",
	"esxcli storage vmfs extent list",
	"esxcli storage core device list",
	"esxcli storage core device smart",
	"esxcli nvme device list",
	"esxcli nvme device log",
	"esxcli nvme device namespace",
	"esxcli network ip interface",
	"esxcli network ip route",
	"esxcli network vswitch standard list",
	"esxcli network vm list",
	"esxcli vm process list",
	"esxcli vm process kill",
	"vim-cmd vimsvc/license",
	"vim-cmd vmsvc/getallvms",
}

func CommandAllowed(cmd string) bool {
	cmd = strings.TrimSpace(cmd)
	for _, p := range esxcliAllowlist {
		if strings.HasPrefix(cmd, p) {
			return true
		}
	}
	return false
}
