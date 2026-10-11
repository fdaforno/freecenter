package esxi

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"regexp"
	"slices"
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

// esxcliNamespaces are the esxcli namespaces the diagnostics console may
// query. Only their "list" and "get" commands are accepted (see CommandAllowed).
var esxcliNamespaces = [][]string{
	{"system", "version"},
	{"system", "settings"},
	{"system", "health"},
	{"hardware", "cpu"},
	{"hardware", "memory"},
	{"hardware", "nic"},
	{"storage", "filesystem"},
	{"storage", "vmfs", "extent"},
	{"storage", "core", "device"},
	{"nvme", "device"},
	{"network", "ip", "interface"},
	{"network", "ip", "route"},
	{"network", "vswitch", "standard"},
	{"network", "vm"},
	{"vm", "process"},
}

// vimCmdQueries are the vim-cmd invocations the diagnostics console accepts,
// matched as a whole: nothing may be appended to them.
var vimCmdQueries = [][]string{
	{"vimsvc/license", "--show"},
	{"vmsvc/getallvms"},
}

// commandArgRE is what one word of a console command may look like. The
// command reaches the host's shell as typed, so anything the shell would
// interpret (separators, pipes, redirections, substitutions, quotes) is refused.
var commandArgRE = regexp.MustCompile(`^[A-Za-z0-9_.:/=,@-]+$`)

// CommandAllowed reports whether cmd is a query the diagnostics console may
// run on a host: an esxcli "list" or "get" in one of esxcliNamespaces, or one
// of vimCmdQueries. Commands that change the host are never accepted.
func CommandAllowed(cmd string) bool {
	if strings.ContainsAny(cmd, "\t\r\n") {
		return false
	}
	args := strings.Fields(cmd)
	if len(args) == 0 {
		return false
	}
	for _, a := range args {
		if !commandArgRE.MatchString(a) {
			return false
		}
	}
	switch args[0] {
	case "esxcli":
		return esxcliQuery(args[1:])
	case "vim-cmd":
		return slices.ContainsFunc(vimCmdQueries, func(q []string) bool { return slices.Equal(q, args[1:]) })
	}
	return false
}

// esxcliQuery reports whether args (an esxcli command line without the
// program name) is "<namespace...> list|get [options]" in an allowed namespace.
func esxcliQuery(args []string) bool {
	// The command is the last word before the options.
	words := args
	if i := slices.IndexFunc(args, func(a string) bool { return strings.HasPrefix(a, "-") }); i >= 0 {
		words = args[:i]
	}
	if len(words) < 2 {
		return false
	}
	if verb := words[len(words)-1]; verb != "list" && verb != "get" {
		return false
	}
	namespace := words[:len(words)-1]
	return slices.ContainsFunc(esxcliNamespaces, func(ns []string) bool {
		return len(namespace) >= len(ns) && slices.Equal(namespace[:len(ns)], ns)
	})
}
