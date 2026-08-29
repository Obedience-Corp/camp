package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/machines"
	"github.com/Obedience-Corp/camp/internal/remote"
)

// testPubKey is a well-formed ed25519 authorized_keys line (body shortened;
// only its shape matters to camp).
const testPubKey = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIBoGusWTiL6ZQ1Zx2vKZ8kMdT0nT9RhP3xQwLmNvBqYz camp-devbox-to-macstudio"

// newPairCmd wires a cobra command with the --json flag pair inherits from the
// machine command group, plus captured streams.
func newPairCmd() (*cobra.Command, *bytes.Buffer) {
	var out bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	cmd.Flags().Bool("json", false, "")
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	return cmd, &out
}

// pairTestEnv isolates machines.yaml (and therefore ~/.obey/keys) and the
// authorized_keys path, then installs stubs for every seam that would
// otherwise need a TTY, an ssh connection, or the developer's real ~/.ssh.
func pairTestEnv(t *testing.T) (fleet string, authKeys string) {
	t.Helper()
	dir := t.TempDir()
	fleet = filepath.Join(dir, "machines.yaml")
	authKeys = filepath.Join(dir, "ssh", "authorized_keys")
	t.Setenv("CAMP_MACHINES_PATH", fleet)
	if err := os.WriteFile(fleet, []byte(
		"version: 1\nmachines:\n  - id: macstudio\n    label: Mac Studio\n"+
			"    host: macstudio.example.ts.net\n    auth_method: tailscale-ssh\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	restore := struct {
		term      func() bool
		confirm   func(context.Context, string) (bool, bool, error)
		inspect   func(context.Context, *machines.Machine, string) (remote.PeerCredentials, error)
		ensure    func(context.Context, *machines.Machine, string, string) (string, error)
		install   func(context.Context, *machines.Machine, string) (bool, error)
		keys      func() (string, error)
		keygen    func(string, string) (string, error)
		reverse   func(context.Context) reverseReport
		rowQuery  func(context.Context, *machines.Machine, string) ([]byte, error)
		heal      func(context.Context, *machines.Machine, string) ([]byte, error)
		localUser func() (string, error)
	}{pairIsTerminal, pairConfirm, pairInspectPeer, pairEnsurePeerKey, pairInstallPeerKey,
		pairAuthorizedKeysPath, pairGenerateKey, pairReverseReachability,
		pairPeerRowQuery, pairHealPeerRow, pairLocalUser}
	t.Cleanup(func() {
		pairIsTerminal, pairConfirm = restore.term, restore.confirm
		pairInspectPeer, pairEnsurePeerKey = restore.inspect, restore.ensure
		pairInstallPeerKey, pairAuthorizedKeysPath = restore.install, restore.keys
		pairGenerateKey, pairReverseReachability = restore.keygen, restore.reverse
		pairPeerRowQuery, pairHealPeerRow, pairLocalUser = restore.rowQuery, restore.heal, restore.localUser
	})

	pairIsTerminal = func() bool { return true }
	pairConfirm = func(context.Context, string) (bool, bool, error) { return true, false, nil }
	pairInspectPeer = func(context.Context, *machines.Machine, string) (remote.PeerCredentials, error) {
		return remote.PeerCredentials{User: "lancerogers"}, nil
	}
	pairEnsurePeerKey = func(context.Context, *machines.Machine, string, string) (string, error) {
		return testPubKey, nil
	}
	pairInstallPeerKey = func(context.Context, *machines.Machine, string) (bool, error) { return true, nil }
	pairAuthorizedKeysPath = func() (string, error) { return authKeys, nil }
	pairGenerateKey = func(path, comment string) (string, error) { return testPubKey, nil }
	pairReverseReachability = func(context.Context) reverseReport {
		return reverseReport{Capable: true, Hint: "sshd is listening here."}
	}
	// The default fleet-heal seams say "the peer has no row for us": an empty
	// machine list from the probe, and a heal call that fails the test if it
	// ever runs (since nothing was found, nothing should be healed). Tests
	// that exercise the heal path override these explicitly.
	pairPeerRowQuery = func(context.Context, *machines.Machine, string) ([]byte, error) {
		return []byte(`{"version":1,"machines":[]}`), nil
	}
	pairHealPeerRow = func(context.Context, *machines.Machine, string) ([]byte, error) {
		t.Error("heal must not run when the probe found no row to heal")
		return nil, nil
	}
	pairLocalUser = func() (string, error) { return "lancerogers", nil }
	return fleet, authKeys
}

// The consent boundary is the whole point of the D003 amendment: without a
// terminal, nothing is written and nothing is even printed.
func TestPairRefusesWithoutTerminal(t *testing.T) {
	_, authKeys := pairTestEnv(t)
	pairIsTerminal = func() bool { return false }
	pairInspectPeer = func(context.Context, *machines.Machine, string) (remote.PeerCredentials, error) {
		t.Error("must not touch the peer before the TTY check")
		return remote.PeerCredentials{}, nil
	}

	cmd, out := newPairCmd()
	err := runMachinePair(cmd, []string{"macstudio"})
	if err == nil || !strings.Contains(err.Error(), "requires an interactive terminal") {
		t.Fatalf("err = %v, want the TTY refusal", err)
	}
	if out.Len() != 0 {
		t.Errorf("a refused pairing must print nothing, got %q", out.String())
	}
	if fileExists(authKeys) {
		t.Error("authorized_keys must not be created by a refused pairing")
	}
}

func TestPairRefusesJSON(t *testing.T) {
	pairTestEnv(t)
	cmd, _ := newPairCmd()
	if err := cmd.Flags().Set("json", "true"); err != nil {
		t.Fatal(err)
	}
	err := runMachinePair(cmd, []string{"macstudio"})
	if err == nil || !strings.Contains(err.Error(), "does not support --json") {
		t.Fatalf("err = %v, want the --json refusal", err)
	}
}

// Declining must leave both machines exactly as they were: this is the
// promise the preview makes right before the prompt.
func TestPairDeclineWritesNothing(t *testing.T) {
	fleet, authKeys := pairTestEnv(t)
	before, err := os.ReadFile(fleet)
	if err != nil {
		t.Fatal(err)
	}
	pairConfirm = func(context.Context, string) (bool, bool, error) { return false, false, nil }
	pairEnsurePeerKey = func(context.Context, *machines.Machine, string, string) (string, error) {
		t.Error("declined pairing must not create a key on the peer")
		return "", nil
	}
	pairInstallPeerKey = func(context.Context, *machines.Machine, string) (bool, error) {
		t.Error("declined pairing must not install a key on the peer")
		return false, nil
	}
	pairGenerateKey = func(string, string) (string, error) {
		t.Error("declined pairing must not generate a local key")
		return "", nil
	}

	cmd, out := newPairCmd()
	if err := runMachinePair(cmd, []string{"macstudio"}); err != nil {
		t.Fatalf("decline should not error: %v", err)
	}
	if !strings.Contains(out.String(), "Nothing was written") {
		t.Errorf("decline should say nothing was written: %q", out.String())
	}
	if fileExists(authKeys) {
		t.Error("declined pairing must not create authorized_keys")
	}
	after, err := os.ReadFile(fleet)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Error("declined pairing must not modify machines.yaml")
	}
}

// The happy path writes all four things the preview promised, and only those.
func TestPairInstallsBothDirections(t *testing.T) {
	fleet, authKeys := pairTestEnv(t)
	var installedRemote string
	pairInstallPeerKey = func(_ context.Context, _ *machines.Machine, pub string) (bool, error) {
		installedRemote = pub
		return true, nil
	}

	cmd, out := newPairCmd()
	if err := runMachinePair(cmd, []string{"macstudio"}); err != nil {
		t.Fatalf("pair failed: %v", err)
	}

	if installedRemote != testPubKey {
		t.Errorf("remote install got %q, want this machine's public key", installedRemote)
	}
	local, err := os.ReadFile(authKeys)
	if err != nil {
		t.Fatalf("local authorized_keys not written: %v", err)
	}
	if !strings.Contains(string(local), testPubKey) {
		t.Errorf("peer key missing from local authorized_keys: %q", local)
	}
	if info, err := os.Stat(authKeys); err == nil && info.Mode().Perm() != 0o600 {
		t.Errorf("authorized_keys mode = %v, want 0600 (sshd ignores looser files)", info.Mode().Perm())
	}

	// The fleet row now carries the credentials the hop needs, including the
	// account name the operator previously had to discover by hand.
	data, err := os.ReadFile(fleet)
	if err != nil {
		t.Fatal(err)
	}
	yaml := string(data)
	for _, want := range []string{"identity_file:", "ssh_user: lancerogers", "auth_method: ssh-agent"} {
		if !strings.Contains(yaml, want) {
			t.Errorf("machines.yaml missing %q after pairing:\n%s", want, yaml)
		}
	}
	if !strings.Contains(out.String(), "paired with macstudio") {
		t.Errorf("missing success line: %q", out.String())
	}
}

// A failing probe means this is the broken direction; the error must send the
// operator to the machine that can already reach here, not to a checklist.
func TestPairFailedProbeNamesTheOtherDirection(t *testing.T) {
	pairTestEnv(t)
	pairInspectPeer = func(context.Context, *machines.Machine, string) (remote.PeerCredentials, error) {
		return remote.PeerCredentials{}, errors.New("SSH permission denied (publickey)")
	}
	cmd, _ := newPairCmd()
	err := runMachinePair(cmd, []string{"macstudio"})
	if err == nil || !strings.Contains(err.Error(), "pair from macstudio instead") {
		t.Fatalf("err = %v, want guidance to pair from the working direction", err)
	}
}

func TestPairUnknownMachineAndSelf(t *testing.T) {
	pairTestEnv(t)
	cmd, _ := newPairCmd()
	err := runMachinePair(cmd, []string{"nosuchbox"})
	if err == nil || !strings.Contains(err.Error(), "unknown machine") {
		t.Fatalf("err = %v, want the unknown-machine error", err)
	}
}

// The reverse direction needs a listener on THIS machine, which camp may
// observe and must never enable. The caveat has to reach the operator.
func TestPairReportsReverseListenerCaveat(t *testing.T) {
	pairTestEnv(t)
	pairReverseReachability = func(context.Context) reverseReport {
		return reverseReport{Hint: "macOS Remote Login is off"}
	}
	cmd, out := newPairCmd()
	if err := runMachinePair(cmd, []string{"macstudio"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "cannot hop here yet") ||
		!strings.Contains(out.String(), "Remote Login is off") {
		t.Errorf("missing reverse-listener caveat: %q", out.String())
	}
}

// TestPairHealPlan exercises pairHealPlan directly against a fixed selfID, so
// it needs neither a TTY nor this machine's real hostname to be a valid
// machine id. It is the single place that pins the probe's tri-state
// contract: a row found with safe content promises a heal, everything else
// (no row, a failed query, unsafe content, a broken local-user lookup)
// degrades to heal=false, with a note only where the operator could
// otherwise mistake silence for the feature not existing.
func TestPairHealPlan(t *testing.T) {
	restore := pairPeerRowQuery
	t.Cleanup(func() { pairPeerRowQuery = restore })

	target := &machines.Machine{ID: "macstudio", Host: "macstudio.example.ts.net"}
	const selfID = "archdtop"

	cases := []struct {
		name      string
		queryOut  string
		queryErr  error
		localUser string
		localErr  error
		wantHeal  bool
		wantNote  string
	}{
		{
			name:      "row found and safe",
			queryOut:  `{"version":1,"machines":[{"id":"archdtop","host":"archdtop.ts.net","label":"Arch Desktop"}]}`,
			localUser: "lancerogers",
			wantHeal:  true,
		},
		{
			name:      "row found with no label",
			queryOut:  `{"version":1,"machines":[{"id":"archdtop","host":"archdtop.ts.net"}]}`,
			localUser: "lancerogers",
			wantHeal:  true,
		},
		{
			name:      "no row for this machine",
			queryOut:  `{"version":1,"machines":[{"id":"someone-else","host":"x"}]}`,
			localUser: "lancerogers",
			wantHeal:  false,
		},
		{
			name:      "query fails",
			queryErr:  errors.New("remote camp not found on macstudio"),
			localUser: "lancerogers",
			wantHeal:  false,
			wantNote:  "could not check whether macstudio already has a fleet row",
		},
		{
			name:      "query returns malformed json",
			queryOut:  "not json",
			localUser: "lancerogers",
			wantHeal:  false,
			wantNote:  "could not check whether macstudio already has a fleet row",
		},
		{
			name:      "row has an unsafe host",
			queryOut:  `{"version":1,"machines":[{"id":"archdtop","host":"h; rm -rf /"}]}`,
			localUser: "lancerogers",
			wantHeal:  false,
			wantNote:  "unexpected shape",
		},
		{
			name:      "row has an unsafe label",
			queryOut:  `{"version":1,"machines":[{"id":"archdtop","host":"archdtop.ts.net","label":"$(rm -rf /)"}]}`,
			localUser: "lancerogers",
			wantHeal:  false,
			wantNote:  "unexpected shape",
		},
		{
			name:      "local user lookup fails",
			queryOut:  `{"version":1,"machines":[{"id":"archdtop","host":"archdtop.ts.net"}]}`,
			localErr:  errors.New("no such user"),
			localUser: "",
			wantHeal:  false,
			wantNote:  "could not determine this machine's account name",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pairPeerRowQuery = func(context.Context, *machines.Machine, string) ([]byte, error) {
				if tc.queryErr != nil {
					return nil, tc.queryErr
				}
				return []byte(tc.queryOut), nil
			}
			heal, _, _, note := pairHealPlan(context.Background(), target, selfID, tc.localUser, tc.localErr)
			if heal != tc.wantHeal {
				t.Errorf("heal = %v, want %v (note=%q)", heal, tc.wantHeal, note)
			}
			if tc.wantNote == "" && note != "" {
				t.Errorf("unexpected note for a case that should stay quiet: %q", note)
			}
			if tc.wantNote != "" && !strings.Contains(note, tc.wantNote) {
				t.Errorf("note = %q, want substring %q", note, tc.wantNote)
			}
		})
	}
}

// TestPairPreviewRendersHealLineOnlyWhenPromised locks in the preview-honesty
// rule the rest of this file already relies on for the other writes: the
// heal line (or its soft-note fallback) appears in the "<peer> will:" section
// if and only if pairPlan actually promises it.
func TestPairPreviewRendersHealLineOnlyWhenPromised(t *testing.T) {
	base := pairPlan{
		Target:        &machines.Machine{ID: "macstudio", Host: "macstudio.example.ts.net"},
		SelfID:        "archdtop",
		LocalKeyPath:  "/home/x/.obey/keys/camp_macstudio_ed25519",
		AuthKeysPath:  "/home/x/.ssh/authorized_keys",
		RemoteKeyName: "camp_archdtop_ed25519",
		RemoteUser:    "lance",
	}

	withHeal := base
	withHeal.HealPeerRow = true
	withHeal.LocalUser = "lancerogers"
	got := pairPreview(withHeal)
	for _, want := range []string{
		`update   machines.yaml row "archdtop"`,
		"auth_method: ssh-agent",
		"ssh_user: lancerogers",
		"identity_file",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("preview missing %q when a heal is promised:\n%s", want, got)
		}
	}

	withoutHeal := base
	got = pairPreview(withoutHeal)
	if strings.Contains(got, `machines.yaml row "archdtop"`) {
		t.Errorf("preview promised a heal that was never found:\n%s", got)
	}

	withNote := base
	withNote.HealSkipNote = "could not check whether macstudio already has a fleet row for this machine"
	got = pairPreview(withNote)
	if !strings.Contains(got, withNote.HealSkipNote) {
		t.Errorf("preview missing the soft note:\n%s", got)
	}
	if strings.Contains(got, `machines.yaml row "archdtop"`) {
		t.Errorf("a soft note must not also promise a heal:\n%s", got)
	}
}

// TestPairHealsPeerRowWhenFound is the end-to-end apply path: a probe that
// finds a row drives a correctly quoted `machine add` on the peer, and both
// the preview and the success report say so.
func TestPairHealsPeerRowWhenFound(t *testing.T) {
	pairTestEnv(t)
	selfID, err := pairSelfID(context.Background())
	if err != nil {
		t.Skipf("cannot derive this machine's id in this environment: %v", err)
	}

	pairPeerRowQuery = func(context.Context, *machines.Machine, string) ([]byte, error) {
		return []byte(`{"version":1,"machines":[{"id":"` + selfID +
			`","host":"self.example.ts.net","label":"My Desktop","auth_method":"tailscale-ssh"}]}`), nil
	}
	pairLocalUser = func() (string, error) { return "lancerogers", nil }

	var gotArgs string
	var gotMachine string
	pairHealPeerRow = func(_ context.Context, m *machines.Machine, args string) ([]byte, error) {
		gotMachine = m.ID
		gotArgs = args
		return nil, nil
	}

	cmd, out := newPairCmd()
	if err := runMachinePair(cmd, []string{"macstudio"}); err != nil {
		t.Fatalf("pair failed: %v", err)
	}

	if gotMachine != "macstudio" {
		t.Errorf("heal ran against %q, want the machine being paired with", gotMachine)
	}
	wantParts := []string{
		"machine add " + remote.ShellQuote(selfID),
		"--host " + remote.ShellQuote("self.example.ts.net"),
		"--label " + remote.ShellQuote("My Desktop"),
		"--auth " + remote.ShellQuote(machines.AuthSSHAgent),
		"--user " + remote.ShellQuote("lancerogers"),
		"--identity " + remote.ShellQuote("~/.obey/keys/"+remote.PeerKeyName(selfID)),
	}
	for _, want := range wantParts {
		if !strings.Contains(gotArgs, want) {
			t.Errorf("heal args = %q, missing %q", gotArgs, want)
		}
	}
	if !strings.Contains(out.String(), `update   machines.yaml row "`+selfID+`"`) {
		t.Errorf("preview did not promise the heal for %q: %q", selfID, out.String())
	}
	if !strings.Contains(out.String(), "macstudio's fleet row for this machine: updated") {
		t.Errorf("apply did not report the heal succeeding: %q", out.String())
	}
}

// TestPairHealFailureDoesNotFailPairing is the "report, don't block" contract
// for the apply-time heal: an unreachable peer camp binary (or any other
// failure of the heal command itself) must not fail a pairing whose earlier
// writes already succeeded, and must leave the operator with the exact
// command to finish the job by hand.
func TestPairHealFailureDoesNotFailPairing(t *testing.T) {
	pairTestEnv(t)
	selfID, err := pairSelfID(context.Background())
	if err != nil {
		t.Skipf("cannot derive this machine's id in this environment: %v", err)
	}

	pairPeerRowQuery = func(context.Context, *machines.Machine, string) ([]byte, error) {
		return []byte(`{"version":1,"machines":[{"id":"` + selfID + `","host":"self.example.ts.net"}]}`), nil
	}
	pairLocalUser = func() (string, error) { return "lancerogers", nil }
	pairHealPeerRow = func(context.Context, *machines.Machine, string) ([]byte, error) {
		return nil, errors.New("remote camp not found on macstudio")
	}

	cmd, out := newPairCmd()
	if err := runMachinePair(cmd, []string{"macstudio"}); err != nil {
		t.Fatalf("a heal failure must not fail the pairing: %v", err)
	}
	if !strings.Contains(out.String(), "paired with macstudio") {
		t.Errorf("pairing must still report success: %q", out.String())
	}
	if !strings.Contains(out.String(), "fleet row for this machine was not updated") {
		t.Errorf("missing the heal-not-updated note: %q", out.String())
	}
	wantFallback := "camp machine add " + selfID + " --host self.example.ts.net --auth ssh-agent " +
		"--user lancerogers --identity ~/.obey/keys/" + remote.PeerKeyName(selfID)
	if !strings.Contains(out.String(), wantFallback) {
		t.Errorf("missing the exact fallback command:\ngot:  %q\nwant substring: %q", out.String(), wantFallback)
	}
}

// TestPairSkipsHealWhenPeerHasNoRow pins the "report, don't block" contract
// for the common case: a peer that was never adopted (or has since removed
// its row for this machine) gets a normal pairing with no mention of a heal
// at all, since the preview never promised one.
func TestPairSkipsHealWhenPeerHasNoRow(t *testing.T) {
	pairTestEnv(t) // default pairPeerRowQuery already reports an empty fleet
	cmd, out := newPairCmd()
	if err := runMachinePair(cmd, []string{"macstudio"}); err != nil {
		t.Fatalf("pair failed: %v", err)
	}
	if strings.Contains(out.String(), "fleet row for this machine") {
		t.Errorf("no row was found to heal, but pairing mentioned one: %q", out.String())
	}
}

// TestPairNotesWhenRowQueryFails covers the no-remote-camp / broken-probe
// path: the probe cannot tell whether the peer has a row, which must degrade
// to a quiet preview note rather than blocking the pairing or attempting a
// heal it never confirmed.
func TestPairNotesWhenRowQueryFails(t *testing.T) {
	pairTestEnv(t)
	pairPeerRowQuery = func(context.Context, *machines.Machine, string) ([]byte, error) {
		return nil, errors.New("remote camp not found on macstudio")
	}
	pairHealPeerRow = func(context.Context, *machines.Machine, string) ([]byte, error) {
		t.Error("heal must not run when the probe could not confirm a row")
		return nil, nil
	}

	cmd, out := newPairCmd()
	if err := runMachinePair(cmd, []string{"macstudio"}); err != nil {
		t.Fatalf("a probe failure must not fail the pairing: %v", err)
	}
	if !strings.Contains(out.String(), "could not check whether macstudio already has a fleet row") {
		t.Errorf("missing the soft note about the failed row check: %q", out.String())
	}
}

func TestInstallLocalAuthorizedKeyIsIdempotentAndShapeChecked(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".ssh", "authorized_keys")

	added, err := installLocalAuthorizedKey(path, testPubKey)
	if err != nil || !added {
		t.Fatalf("first install: added=%v err=%v", added, err)
	}
	added, err = installLocalAuthorizedKey(path, testPubKey)
	if err != nil || added {
		t.Fatalf("second install should be a no-op: added=%v err=%v", added, err)
	}
	data, _ := os.ReadFile(path)
	if strings.Count(string(data), testPubKey) != 1 {
		t.Errorf("re-pairing duplicated the key: %q", data)
	}

	// An existing file without a trailing newline must not have the new key
	// glued onto the last line.
	other := filepath.Join(t.TempDir(), "authorized_keys")
	if err := os.WriteFile(other, []byte("ssh-rsa AAAAB3 existing"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := installLocalAuthorizedKey(other, testPubKey); err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(other)
	if !strings.Contains(string(body), "existing\nssh-ed25519") {
		t.Errorf("append did not start a new line: %q", body)
	}

	// A line that is not a plain ed25519 key is refused: this file grants login.
	for _, bad := range []string{
		`ssh-ed25519 AAAA" ; rm -rf ~`,
		"ssh-ed25519 AAAA $(whoami)",
		"not-a-key",
		`command="rm -rf /" ssh-ed25519 AAAA`,
	} {
		if _, err := installLocalAuthorizedKey(path, bad); err == nil {
			t.Errorf("installed a malformed key line: %q", bad)
		}
	}
}
