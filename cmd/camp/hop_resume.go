package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/Obedience-Corp/camp/cmd/camp/cmdutil"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/machines"
	"github.com/Obedience-Corp/camp/internal/remote"
)

// hopResumeSockEnv is set inside a hopped shell when the shell that opened the
// hop is listening for a follow-up switch. Unwind writes the next selector
// here and exits; that shell applies it, so a switch never nests a second ssh.
const hopResumeSockEnv = "CAMP_HOP_RESUME_SOCK"

func init() {
	rootCmd.AddCommand(hopResumeCmd)
	hopResumeCmd.AddCommand(hopResumeSendCmd, hopResumeRunCmd)
	hopResumeSendCmd.Flags().String("org", "", "Limit the resumed switch to this org")
	hopResumeSendCmd.Flags().String("status", "", "Limit the resumed switch to this status")
	hopResumeSendCmd.Flags().Bool("all", false, "Include all lifecycle statuses")
	hopResumeSendCmd.Flags().String("host", "", "Selected machine host identity")
}

// hopResumeCmd is wrapper plumbing. Operators do not type it; shell-init calls
// `run` around every outbound ssh, and an unwinding switch calls `send`.
var hopResumeCmd = &cobra.Command{
	Use:    "hop-resume",
	Short:  "Carry a switch back across an unwound hop (internal)",
	Hidden: true,
}

var hopResumeSendCmd = &cobra.Command{
	Use:    "send <selector>",
	Hidden: true,
	Args:   cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		scope, err := switchScopeFromFlags(cmd)
		if err != nil {
			return err
		}
		host, err := cmd.Flags().GetString("host")
		if err != nil {
			return err
		}
		return sendHopResume(cmd.Context(), args[0], scope, host)
	},
}

var hopResumeRunCmd = &cobra.Command{
	Use:    "run <ssh-line>",
	Hidden: true,
	Args:   cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		follow, err := runHopResume(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		if follow != "" {
			_, err = io.WriteString(cmd.OutOrStdout(), follow)
		}
		return err
	},
}

type hopResumeRequest struct {
	Selector string                `json:"selector"`
	Scope    cmdutil.CampaignScope `json:"scope"`
	Host     string                `json:"host,omitempty"`
}

func sendHopResume(ctx context.Context, selector string, scope cmdutil.CampaignScope, host string) error {
	selector, err := sanitizeResumeSelector(selector)
	if err != nil {
		return err
	}
	if selector == "" {
		return camperrors.New("hop-resume: empty selector")
	}
	request, err := newHopResumeRequest(selector, scope, host)
	if err != nil {
		return err
	}
	sock := strings.TrimSpace(os.Getenv(hopResumeSockEnv))
	if sock == "" {
		return camperrors.New("hop-resume: " + hopResumeSockEnv + " is not set\nHint: re-source shell init and hop again")
	}
	dialer := net.Dialer{Timeout: 2 * time.Second}
	conn, err := dialer.DialContext(ctx, "unix", sock)
	if err != nil {
		return camperrors.Wrap(err, "hop-resume: tell the shell that opened this hop")
	}
	defer func() { _ = conn.Close() }()
	stopClose := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stopClose()
	_ = conn.SetDeadline(time.Now().Add(hopResumeTimeout))
	if err := json.NewEncoder(conn).Encode(hopResumeEnvelope{Version: 1, Request: request}); err != nil {
		return err
	}
	var response hopResumeResponse
	if err := json.NewDecoder(io.LimitReader(conn, 4096)).Decode(&response); err != nil {
		return camperrors.Wrap(err, "hop-resume: parent did not acknowledge the route; staying in this shell")
	}
	if response.Version != 1 {
		return camperrors.New("hop-resume: unsupported parent acknowledgement; staying in this shell")
	}
	if response.Error != "" {
		return camperrors.New(response.Error)
	}
	if !response.Accepted {
		return camperrors.New("hop-resume: parent rejected the route; staying in this shell")
	}
	return nil
}

func sanitizeResumeSelector(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if strings.ContainsAny(raw, "\r\n") || strings.Contains(raw, "\x00") {
		return "", camperrors.New("hop-resume: selector contains a newline")
	}
	if raw == "-" {
		return "", camperrors.New("hop-resume: '-' is hop-back, not a resume selector")
	}
	return raw, nil
}

// runHopResume runs one outbound ssh line with a resume socket forwarded to
// the far shell. stdout of this process is reserved for the follow-up shell
// line (possibly empty), so the interactive session is attached to the
// terminal this process inherited on stdin and stderr.
func runHopResume(ctx context.Context, line string) (string, error) {
	local, remoteSock, err := resumeSocketPaths()
	if err != nil {
		return "", err
	}
	defer func() { _ = os.Remove(local) }()

	ln, err := net.Listen("unix", local)
	if err != nil {
		return "", camperrors.Wrap(err, "hop-resume: listen")
	}
	_ = os.Chmod(local, 0o700)
	defer func() { _ = ln.Close() }()

	resumeCtx, cancelResume := context.WithCancel(ctx)
	defer cancelResume()
	got := make(chan string, 1)
	go func() {
		follow, _ := serveHopResume(resumeCtx, ln, prepareHopResume)
		got <- follow
	}()

	argv, err := prepareResumeSSH(line, local, remoteSock)
	if err != nil {
		return "", err
	}
	in, out, inherited := inheritedHopTerminal(os.Stdin, os.Stderr, isTerminalFile)
	if !inherited {
		tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
		if err != nil {
			return "", camperrors.Wrap(err, "hop-resume: open /dev/tty")
		}
		defer func() { _ = tty.Close() }()
		in, out = tty, tty
		argv = withoutControlMaster(argv)
	}
	sshCmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	sshCmd.Stdin = in
	sshCmd.Stdout = out
	sshCmd.Stderr = out
	runErr := sshCmd.Run()
	cancelResume()
	_ = ln.Close()
	follow := <-got
	if runErr != nil {
		return "", runErr
	}
	return follow, nil
}

// inheritedHopTerminal returns the terminal descriptors ssh may hand to a
// ControlMaster. A multiplexed ssh passes its stdio to the master, which is
// detached from every terminal, so the descriptors must name the terminal
// device itself. /dev/tty resolves against the calling process instead: on
// macOS the master gets EIO from it and drops the session with status 255.
func inheritedHopTerminal(stdin, stderr *os.File, isTerminal func(*os.File) bool) (in, out *os.File, ok bool) {
	if isTerminal(stdin) && isTerminal(stderr) {
		return stdin, stderr, true
	}
	return nil, nil, false
}

func isTerminalFile(f *os.File) bool { return term.IsTerminal(int(f.Fd())) }

// withoutControlMaster makes ssh own its connection. ssh keeps the first value
// it sees for an option, so this must precede the line's own ControlPath.
func withoutControlMaster(argv []string) []string {
	out := make([]string, 0, len(argv)+2)
	out = append(out, argv[:2]...)
	out = append(out, "-o", "ControlPath=none")
	return append(out, argv[2:]...)
}

func resumeSocketPaths() (local, remoteSock string, err error) {
	f, err := os.CreateTemp("/tmp", "camp-hr-*.path")
	if err != nil {
		return "", "", err
	}
	name := f.Name()
	_ = f.Close()
	_ = os.Remove(name)
	// Keep the paths short: macOS unix-socket addresses stop at 104 bytes,
	// and CreateTemp's directory prefix can already be long under TMPDIR.
	base := strings.TrimPrefix(name, "/tmp/")
	return "/tmp/" + base + "-l.sock", "/tmp/" + base + "-r.sock", nil
}

// A request is acknowledged only after the parent has prepared its route.
// The child exits on success, so errors must arrive while its shell is alive.
const hopResumeTimeout = 30 * time.Second

// Keep the actionable selector nested: a legacy reader sees an empty selector
// and cannot queue a misdirected switch when this client rejects its missing ACK.
type hopResumeEnvelope struct {
	Version int              `json:"version"`
	Request hopResumeRequest `json:"request"`
}

type hopResumeResponse struct {
	Version  int    `json:"version"`
	Accepted bool   `json:"accepted"`
	Error    string `json:"error,omitempty"`
}

func newHopResumeRequest(selector string, scope cmdutil.CampaignScope, host string) (hopResumeRequest, error) {
	request := hopResumeRequest{Selector: selector, Scope: scope, Host: host}
	parsed, err := cmdutil.ParseMachineSelector(selector)
	if err != nil {
		return request, err
	}
	if parsed.Machine == "" {
		return request, camperrors.New("hop-resume: selector must name a machine")
	}
	if parsed.Machine == machines.LocalMachineID {
		if host != "" {
			return request, camperrors.New("hop-resume: local selector cannot carry a remote host")
		}
	} else if strings.TrimSpace(host) == "" {
		return request, camperrors.New("hop-resume: remote request has no host identity; re-source shell init and hop again")
	}
	return request, nil
}

// Capture the selected identity before emitting shell code. The later send
// subprocess must not reinterpret an alias if the child's registry changes.
func hopResumeTargetHost(selector string) (string, error) {
	parsed, err := cmdutil.ParseMachineSelector(selector)
	if err != nil {
		return "", err
	}
	if parsed.Machine == "" || parsed.Machine == machines.LocalMachineID {
		return "", nil
	}
	mf, err := machines.Load()
	if err != nil {
		return "", err
	}
	machine, _, found := mf.Lookup(parsed.Machine)
	if !found || machine == nil || strings.TrimSpace(machine.Host) == "" {
		return "", camperrors.New("hop-resume: unknown target machine " + parsed.Machine)
	}
	return machine.Host, nil
}

// mapHopResume uses host identity, never an alias from the child registry.
// The selected parent entry supplies all authentication and endpoint settings.
func mapHopResume(request hopResumeRequest, registry *machines.File) (hopResumeRequest, error) {
	parsed, err := cmdutil.ParseMachineSelector(request.Selector)
	if err != nil {
		return request, err
	}
	if parsed.Machine == "" {
		return request, camperrors.New("hop-resume: selector must name a machine")
	}
	if parsed.Machine == machines.LocalMachineID {
		if request.Host != "" {
			return request, camperrors.New("hop-resume: local selector cannot carry a remote host")
		}
		return request, nil
	}
	host := strings.ToLower(normalizeDNSName(strings.TrimSpace(request.Host)))
	if host == "" {
		return request, camperrors.New("hop-resume: remote request has no host identity; re-source shell init and hop again")
	}
	var match *machines.Machine
	for i := range registry.Machines {
		machine := &registry.Machines[i]
		if strings.ToLower(normalizeDNSName(machine.Host)) != host {
			continue
		}
		if match != nil {
			return request, camperrors.New("hop-resume: parent has multiple routes to " + request.Host + "; staying in this shell")
		}
		match = machine
	}
	if match == nil {
		return request, camperrors.New("hop-resume: parent has no route to " + request.Host + "; add the host to its machines registry before switching")
	}
	request.Selector = match.ID + ":" + parsed.Remainder
	return request, nil
}

func prepareHopResume(ctx context.Context, request hopResumeRequest) (string, error) {
	registry, err := machines.Load()
	if err != nil {
		return "", err
	}
	request, err = mapHopResume(request, registry)
	if err != nil {
		return "", err
	}
	return switchFollowUpContext(ctx, request)
}

func serveHopResume(ctx context.Context, ln net.Listener, prepare func(context.Context, hopResumeRequest) (string, error)) (string, error) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return "", err
		}
		follow, err := acceptHopResume(ctx, conn, prepare)
		_ = conn.Close()
		if err == nil {
			return follow, nil
		}
		// A rejected request must not disable onward switching for this shell.
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
	}
}

func acceptHopResume(ctx context.Context, conn net.Conn, prepare func(context.Context, hopResumeRequest) (string, error)) (string, error) {
	_ = conn.SetDeadline(time.Now().Add(hopResumeTimeout))
	ctx, cancel := context.WithTimeout(ctx, hopResumeTimeout-time.Second)
	defer cancel()
	stopClose := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stopClose()
	var envelope hopResumeEnvelope
	err := json.NewDecoder(io.LimitReader(conn, 4096)).Decode(&envelope)
	if err == nil && envelope.Version != 1 {
		err = camperrors.New("hop-resume: unsupported request version; update camp on both machines and hop again")
	}
	request := envelope.Request
	if err == nil {
		request.Selector, err = sanitizeResumeSelector(request.Selector)
	}
	if err == nil && request.Selector == "" {
		err = camperrors.New("hop-resume: empty selector")
	}
	var follow string
	if err == nil {
		follow, err = prepare(ctx, request)
	}
	response := hopResumeResponse{Version: 1, Accepted: err == nil}
	if err != nil {
		response.Error = err.Error()
	}
	if writeErr := json.NewEncoder(conn).Encode(response); writeErr != nil {
		return "", writeErr
	}
	return follow, err
}

// prepareResumeSSH parses the one ssh line emitShellConnect prints and adds a
// unix-socket remote forward plus CAMP_HOP_RESUME_SOCK in the remote command.
// Every token of that line is shell-quoted except the leading `ssh -t`.
func prepareResumeSSH(line, localSock, remoteSock string) ([]string, error) {
	tokens, err := splitShellQuoted(line)
	if err != nil {
		return nil, err
	}
	if len(tokens) < 4 || tokens[0] != "ssh" || tokens[1] != "-t" {
		return nil, camperrors.New("hop-resume: expected an ssh -t switch line")
	}
	remoteCmd := tokens[len(tokens)-1]
	target := tokens[len(tokens)-2]
	mid := append([]string{}, tokens[2:len(tokens)-2]...)
	argv := make([]string, 0, len(tokens)+6)
	argv = append(argv, "ssh", "-t")
	argv = append(argv, mid...)
	argv = append(argv, "-o", "StreamLocalBindUnlink=yes", "-R", remoteSock+":"+localSock)
	argv = append(argv, target)
	argv = append(argv, "export "+hopResumeSockEnv+"="+remote.ShellQuote(remoteSock)+" && "+remoteCmd)
	return argv, nil
}

func resumeSwitchArgs(request hopResumeRequest) []string {
	args := []string{"switch", "--shell-connect"}
	if request.Scope.Org != "" {
		args = append(args, "--org", request.Scope.Org)
	}
	if request.Scope.Status != "" {
		args = append(args, "--status", request.Scope.Status)
	}
	if request.Scope.All {
		args = append(args, "--all")
	}
	return append(args, "--", request.Selector)
}

func switchFollowUpContext(ctx context.Context, request hopResumeRequest) (string, error) {
	exe, err := os.Executable()
	if err != nil || exe == "" {
		exe = "camp"
	}
	cmd := exec.CommandContext(ctx, exe, resumeSwitchArgs(request)...)
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// splitShellQuoted splits a line of shell-quoted tokens. It understands the
// single-quote form remote.ShellQuote emits, including embedded quotes.
func splitShellQuoted(line string) ([]string, error) {
	s := strings.TrimSpace(line)
	var out []string
	for len(s) > 0 {
		if s[0] == '\'' {
			var b strings.Builder
			i := 1
			closed := false
			for i < len(s) {
				if strings.HasPrefix(s[i:], `'\''`) {
					b.WriteByte('\'')
					i += 4
					continue
				}
				if s[i] == '\'' {
					i++
					out = append(out, b.String())
					s = strings.TrimPrefix(s[i:], " ")
					closed = true
					break
				}
				b.WriteByte(s[i])
				i++
			}
			if !closed {
				return nil, camperrors.New("hop-resume: unterminated quote in switch line")
			}
			continue
		}
		i := strings.IndexByte(s, ' ')
		if i < 0 {
			out = append(out, s)
			break
		}
		out = append(out, s[:i])
		s = strings.TrimPrefix(s[i:], " ")
	}
	return out, nil
}

// resumeSwitchLine is the remote shell line that hands selector to the shell
// underneath this hop and then leaves. `exit` is what pops the ssh; send runs
// first so a failed handoff does not dump the user in the wrong camp.
func resumeSwitchLine(selector string, scope cmdutil.CampaignScope) string {
	return resumeSwitchLineForHost(selector, scope, "")
}

func resumeSwitchLineForHost(selector string, scope cmdutil.CampaignScope, host string) string {
	var flags string
	if host != "" {
		flags += " --host " + remote.ShellQuote(host)
	}
	if scope.Org != "" {
		flags += " --org " + remote.ShellQuote(scope.Org)
	}
	if scope.Status != "" {
		flags += " --status " + remote.ShellQuote(scope.Status)
	}
	if scope.All {
		flags += " --all"
	}
	return fmt.Sprintf("command camp hop-resume send%s -- %s && exit\n", flags, remote.ShellQuote(selector))
}
