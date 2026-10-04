package main

import (
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/spf13/cobra"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/remote"
)

// hopResumeSockEnv is set inside a hopped shell when the shell that opened the
// hop is listening for a follow-up switch. Unwind writes the next selector
// here and exits; that shell applies it, so a switch never nests a second ssh.
const hopResumeSockEnv = "CAMP_HOP_RESUME_SOCK"

func init() {
	rootCmd.AddCommand(hopResumeCmd)
	hopResumeCmd.AddCommand(hopResumeSendCmd, hopResumeRunCmd)
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
	RunE: func(_ *cobra.Command, args []string) error {
		return sendHopResume(args[0])
	},
}

var hopResumeRunCmd = &cobra.Command{
	Use:    "run <ssh-line>",
	Hidden: true,
	Args:   cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		follow, err := runHopResume(args[0])
		if err != nil {
			return err
		}
		if follow != "" {
			_, err = io.WriteString(cmd.OutOrStdout(), follow)
		}
		return err
	},
}

func sendHopResume(selector string) error {
	selector, err := sanitizeResumeSelector(selector)
	if err != nil {
		return err
	}
	if selector == "" {
		return camperrors.New("hop-resume: empty selector")
	}
	sock := strings.TrimSpace(os.Getenv(hopResumeSockEnv))
	if sock == "" {
		return camperrors.New("hop-resume: " + hopResumeSockEnv + " is not set\nHint: re-source shell init and hop again")
	}
	dialer := net.Dialer{Timeout: 2 * time.Second}
	conn, err := dialer.Dial("unix", sock)
	if err != nil {
		return camperrors.Wrap(err, "hop-resume: tell the shell that opened this hop")
	}
	defer func() { _ = conn.Close() }()
	_, err = io.WriteString(conn, selector+"\n")
	return err
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
// line (possibly empty); the interactive session is attached to /dev/tty
// because the wrapper captures stdout.
func runHopResume(line string) (string, error) {
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

	got := make(chan string, 1)
	go func() {
		sel, _ := readResumeSelector(ln)
		got <- sel
	}()

	argv, err := prepareResumeSSH(line, local, remoteSock)
	if err != nil {
		return "", err
	}
	sshCmd := exec.Command(argv[0], argv[1:]...)
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return "", camperrors.Wrap(err, "hop-resume: open /dev/tty")
	}
	defer func() { _ = tty.Close() }()
	sshCmd.Stdin = tty
	sshCmd.Stdout = tty
	sshCmd.Stderr = tty
	runErr := sshCmd.Run()
	_ = ln.Close()
	sel := <-got
	if runErr != nil {
		return "", runErr
	}
	if sel == "" {
		return "", nil
	}
	return switchFollowUp(sel)
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

func readResumeSelector(ln net.Listener) (string, error) {
	conn, err := ln.Accept()
	if err != nil {
		return "", err
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
	data, err := io.ReadAll(io.LimitReader(conn, 4096))
	if err != nil && !os.IsTimeout(err) {
		return "", err
	}
	return sanitizeResumeSelector(string(data))
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

func switchFollowUp(selector string) (string, error) {
	exe, err := os.Executable()
	if err != nil || exe == "" {
		exe = "camp"
	}
	cmd := exec.Command(exe, "switch", selector, "--shell-connect")
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
func resumeSwitchLine(selector string) string {
	return fmt.Sprintf("command camp hop-resume send -- %s && exit\n", remote.ShellQuote(selector))
}
