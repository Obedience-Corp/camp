package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/cmd/camp/cmdutil"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/machines"
	"github.com/Obedience-Corp/camp/internal/ui"
)

// The hop session model (design WI-44f57e): the real shell stack IS the
// session. A hop no longer execs over the local shell (emitShellConnect), so
// the shell that hopped survives underneath the ssh client, and returning to
// it is `exit` — an unwind, not a new dial. One CAMP_HOP_ORIGIN frame per
// shell is then correct by construction: in a chain A→B→C, B's shell carries
// origin=A and C's carries origin=B, and each unwind pops exactly one level.
// No env stack, no session file, no daemon.

// sshSessionEnvVars are OpenSSH's markers that this login arrived over ssh.
// Camp reads them as a signal — "this shell can be unwound" — never as a
// router: which machine to return to still comes from CAMP_HOP_ORIGIN, and a
// shell with the payload but none of these markers falls back to the dial-back
// hop, so an exotic transport degrades to today's behavior instead of a wrong
// `exit`.
var sshSessionEnvVars = []string{"SSH_CONNECTION", "SSH_TTY", "SSH_CLIENT"}

func insideSSHSession() bool {
	for _, v := range sshSessionEnvVars {
		if strings.TrimSpace(os.Getenv(v)) != "" {
			return true
		}
	}
	return false
}

// sessionHopOrigin returns this hopped shell's parsed origin frame. A missing
// or malformed payload is simply "no origin" here: the callers are guards
// deciding whether a selector names the origin, and a guard that errors on a
// forged env would turn session-local state into a denial of service on
// ordinary switching. `camp switch -` keeps its own stricter parse with real
// error messages.
func sessionHopOrigin() (HopOrigin, bool) {
	raw := strings.TrimSpace(os.Getenv(HopOriginEnvVar))
	if raw == "" {
		return HopOrigin{}, false
	}
	origin, err := ParseHopOrigin(raw)
	if err != nil {
		return HopOrigin{}, false
	}
	return origin, true
}

// selectorNamesOrigin reports whether a selector's machine segment names the
// machine this shell was hopped from: the payload's advisory id, the origin
// host itself, or a registered id whose row points at the origin host. The
// fleet lookup matters because the steady state after mutual adoption is an
// operator typing their own fleet id ("archdtop"), not the payload's derived
// one — and those only sometimes coincide.
func selectorNamesOrigin(machineSel string, origin HopOrigin) bool {
	if machineSel == "" {
		return false
	}
	if machineSel == transientOriginID(origin.Host) {
		return true
	}
	if origin.ID != "" && machineSel == origin.ID {
		return true
	}
	want := strings.ToLower(normalizeDNSName(origin.Host))
	if strings.ToLower(normalizeDNSName(machineSel)) == want {
		return true
	}
	if mf, err := machines.Load(); err == nil {
		if m, _, found := mf.Lookup(machineSel); found && m != nil {
			return strings.ToLower(normalizeDNSName(m.Host)) == want
		}
	}
	return false
}

// unwindInsteadOfHop decides whether a remote selector must pop this hop
// instead of dialing. Outside an ssh session, or with no origin payload, it
// returns false and the caller dials — a stale CAMP_HOP_ORIGIN in a fresh
// shell is an ordinary hop.
//
// Inside a hop, every remote target unwinds. The shell underneath already
// exists, and a second ssh into it (or out of it, nested under it) is the
// opposite of moving between camps as if they were on one machine. resume is
// what that shell should switch to after it resumes. Empty resume means the
// shell is already in the right camp, so the line is just `exit`.
func unwindInsteadOfHop(msel cmdutil.ParsedMachineSelector) (resume string, unwind bool) {
	if !insideSSHSession() {
		return "", false
	}
	origin, ok := sessionHopOrigin()
	if !ok {
		return "", false
	}
	if !selectorNamesOrigin(msel.Machine, origin) {
		if msel.Remainder == "" {
			return msel.Machine + ":", true
		}
		return msel.Machine + ":" + msel.Remainder, true
	}
	parsed := cmdutil.ParseSwitchSelector(msel.Remainder)
	if parsed.Campaign == "" {
		return "", true
	}
	return "local:" + msel.Remainder, true
}

// emitHopUnwind writes the shell line that pops this hop. A non-empty resume
// is delivered to the shell underneath through the resume socket that the
// outbound hop forwarded; without that socket (a hop opened before the
// wrapper knew how) we refuse rather than exit into the wrong camp.
func emitHopUnwind(cmd *cobra.Command, resume string, printOnly, shellConnect, jsonOut bool) error {
	origin, hasOrigin := sessionHopOrigin()
	label := "the machine that opened this hop"
	back := "the camp you hopped from"
	if hasOrigin {
		label = hopOriginLabel(origin)
		if origin.Campaign != "" {
			back = origin.Campaign
		}
	}
	if !shellConnect {
		target := label
		if resume != "" {
			target = resume
		}
		return camperrors.New("resolved " + target + "; run via the csw shell wrapper to hop there")
	}
	if err := guardRemoteOutputFlags(label, printOnly, jsonOut); err != nil {
		return err
	}
	if resume == "" {
		_, _ = fmt.Fprintln(cmd.ErrOrStderr(), ui.Dim("camp: returning to "+label+
			" — unwinding this ssh session, no new connection"))
		return emitUnwind(cmd.OutOrStdout())
	}
	if strings.TrimSpace(os.Getenv(hopResumeSockEnv)) == "" {
		return camperrors.New("this hop can return to " + back + ", but it cannot switch onward to \"" + resume + "\"\n" +
			"Hint: 'csw -' returns to " + back + ". Re-source shell init (eval \"$(camp shell-init zsh)\") and hop again so a switch can continue on that machine")
	}
	_, _ = fmt.Fprintln(cmd.ErrOrStderr(), ui.Dim("camp: returning to "+label+
		" — unwinding this ssh session, then switching to "+resume))
	scope, err := switchScopeFromFlags(cmd)
	if err != nil {
		return err
	}
	_, err = io.WriteString(cmd.OutOrStdout(), resumeSwitchLine(resume, scope))
	return err
}

// emitUnwind prints the one line that returns a hopped shell to its origin:
// `exit`. The wrapper evals it, this shell ends, the inbound ssh terminates,
// and the origin shell — still alive beneath its ssh client — resumes exactly
// where it was. No dial, no nested connection, and the origin needs nothing
// installed. Inside tmux or a nested subshell on this side, `exit` pops that
// level instead; repeating the gesture still converges.
func emitUnwind(w io.Writer) error {
	_, err := fmt.Fprintln(w, "exit")
	return err
}
