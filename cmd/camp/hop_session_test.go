package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Obedience-Corp/camp/cmd/camp/cmdutil"
	"github.com/Obedience-Corp/camp/internal/machines"
)

// clearSSHSessionEnv pins the test to "not inside ssh" regardless of how the
// test process itself was launched.
func clearSSHSessionEnv(t *testing.T) {
	t.Helper()
	for _, v := range sshSessionEnvVars {
		t.Setenv(v, "")
	}
}

func TestInsideSSHSession(t *testing.T) {
	clearSSHSessionEnv(t)
	if insideSSHSession() {
		t.Fatal("no markers set, must not read as an ssh session")
	}
	t.Setenv("SSH_CONNECTION", "100.1.2.3 50000 100.4.5.6 22")
	if !insideSSHSession() {
		t.Fatal("SSH_CONNECTION set, must read as an ssh session")
	}
}

// The unwind is the whole point of the session model: inside an ssh session,
// `csw -` pops the shell stack instead of dialing a new connection back into
// the machine that already has an inbound session to us.
func TestHopBackUnwindsInsideSSHSession(t *testing.T) {
	clearSSHSessionEnv(t)
	t.Setenv("SSH_TTY", "/dev/pts/3")
	t.Setenv(HopOriginEnvVar, testOriginPayload)
	t.Setenv("CAMP_MACHINES_PATH", filepath.Join(t.TempDir(), "machines.yaml"))

	// A dial on the unwind path is the regression this test exists to catch.
	restore := resolveRemoteRoot
	resolveRemoteRoot = func(context.Context, *machines.Machine, string) (string, error) {
		t.Error("unwind must not resolve the origin over ssh")
		return "", nil
	}
	t.Cleanup(func() { resolveRemoteRoot = restore })

	cmd, stdout, stderr := newHopBackCmd()
	if err := runHopBack(context.Background(), cmd, false, true, false); err != nil {
		t.Fatalf("unwind hop-back failed: %v", err)
	}
	if got := stdout.String(); got != "exit\n" {
		t.Errorf("stdout = %q, want exactly \"exit\\n\"", got)
	}
	if !strings.Contains(stderr.String(), "unwinding this ssh session") {
		t.Errorf("stderr should explain the unwind: %q", stderr.String())
	}

	// A payload with no campaign still unwinds: the return shell resumes
	// wherever it was, so the campaign is not needed on this path.
	t.Setenv(HopOriginEnvVar, "v1;host=devbox.example.ts.net;user=alex")
	cmd, stdout, _ = newHopBackCmd()
	if err := runHopBack(context.Background(), cmd, false, true, false); err != nil {
		t.Fatalf("campaign-less unwind failed: %v", err)
	}
	if stdout.String() != "exit\n" {
		t.Errorf("campaign-less unwind stdout = %q", stdout.String())
	}

	// Output flags are refused identically on the unwind path.
	cmd, _, _ = newHopBackCmd()
	if err := runHopBack(context.Background(), cmd, true, true, false); err == nil ||
		!strings.Contains(err.Error(), "--print is local-only") {
		t.Errorf("--print on unwind path: err = %v, want the local-only refusal", err)
	}
}

func TestSelectorNamesOrigin(t *testing.T) {
	origin := HopOrigin{Host: "devbox.example.ts.net", User: "alex", Campaign: "obey-campaign", ID: "devbox"}

	fleet := filepath.Join(t.TempDir(), "machines.yaml")
	if err := os.WriteFile(fleet, []byte(
		"version: 1\nmachines:\n  - id: workbox\n    host: devbox.example.ts.net\n  - id: other\n    host: elsewhere.ts.net\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CAMP_MACHINES_PATH", fleet)

	cases := []struct {
		sel  string
		want bool
	}{
		{"devbox", true},                 // payload's advisory id
		{"devbox.example.ts.net", true},  // the origin host itself
		{"DEVBOX.example.ts.net.", true}, // normalized comparison
		{"workbox", true},                // fleet id whose row points at the origin
		{"other", false},                 // fleet id pointing elsewhere
		{"unrelated", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := selectorNamesOrigin(tc.sel, origin); got != tc.want {
			t.Errorf("selectorNamesOrigin(%q) = %v, want %v", tc.sel, got, tc.want)
		}
	}
}

func TestUnwindInsteadOfHop(t *testing.T) {
	clearSSHSessionEnv(t)
	t.Setenv("CAMP_MACHINES_PATH", filepath.Join(t.TempDir(), "machines.yaml"))
	t.Setenv(HopOriginEnvVar, testOriginPayload) // origin devbox, campaign obey-campaign

	sel := func(raw string) cmdutil.ParsedMachineSelector {
		t.Helper()
		msel, err := cmdutil.ParseMachineSelector(raw)
		if err != nil {
			t.Fatal(err)
		}
		return msel
	}

	// Outside an ssh session a stale payload must not hijack ordinary hops.
	if resume, unwind := unwindInsteadOfHop(sel("devbox:obey-campaign")); unwind || resume != "" {
		t.Errorf("fresh shell: unwind fired (unwind=%v resume=%q)", unwind, resume)
	}

	t.Setenv("SSH_CONNECTION", "100.1.2.3 50000 100.4.5.6 22")

	// Named camps resolve again on the parent so lifecycle scope is enforced.
	if resume, unwind := unwindInsteadOfHop(sel("devbox:obey-campaign")); !unwind || resume != "local:obey-campaign" {
		t.Errorf("origin+own camp: want plain unwind, got unwind=%v resume=%q", unwind, resume)
	}
	// Preserve the typed selector for resolution on the parent.
	if resume, unwind := unwindInsteadOfHop(sel("devbox:obey_campaign")); !unwind || resume != "local:obey_campaign" {
		t.Errorf("origin camp alias: want plain unwind, got unwind=%v resume=%q", unwind, resume)
	}
	// A different camp on the origin unwinds and tells the shell underneath
	// which camp to open. It must not be a second ssh.
	if resume, unwind := unwindInsteadOfHop(sel("devbox:something-else")); !unwind || resume != "local:something-else" {
		t.Errorf("origin+other camp: want resume something-else, got unwind=%v resume=%q", unwind, resume)
	}
	// A third machine unwinds too. The shell underneath opens that hop, so the
	// ssh stack stays one level deep.
	if resume, unwind := unwindInsteadOfHop(sel("thirdbox:any")); !unwind || resume != "thirdbox:any" {
		t.Errorf("third machine: want resume thirdbox:any, got unwind=%v resume=%q", unwind, resume)
	}
}
