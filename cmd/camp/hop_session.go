package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/Obedience-Corp/camp/cmd/camp/cmdutil"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/machines"
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

// originSwitchGuard classifies a remote selector against this shell's hop
// session. unwind means "treat this as `csw -`": the selector names the origin
// with its own campaign (or none). A non-nil refuse means the selector names
// the origin with a DIFFERENT campaign — camp will not open a second ssh back
// into the machine that already has a live inbound session to us, so the user
// gets the unwind gesture instead. Both are false/nil outside an ssh session:
// in a fresh shell, dialing a machine that happens to be in a stale
// CAMP_HOP_ORIGIN is an ordinary hop.
func originSwitchGuard(msel cmdutil.ParsedMachineSelector) (unwind bool, refuse error) {
	if !insideSSHSession() {
		return false, nil
	}
	origin, ok := sessionHopOrigin()
	if !ok || !selectorNamesOrigin(msel.Machine, origin) {
		return false, nil
	}
	if msel.Remainder == "" || msel.Remainder == origin.Campaign {
		return true, nil
	}
	return false, camperrors.New("this shell was hopped here from " + msel.Machine +
		"; 'csw -' returns there without a new connection (then switch to \"" +
		msel.Remainder + "\" locally). Camp does not open a second ssh back into this session's origin")
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
