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
		term    func() bool
		confirm func(context.Context, string) (bool, bool, error)
		inspect func(context.Context, *machines.Machine, string) (remote.PeerCredentials, error)
		ensure  func(context.Context, *machines.Machine, string, string) (string, error)
		install func(context.Context, *machines.Machine, string) (bool, error)
		keys    func() (string, error)
		keygen  func(string, string) (string, error)
		reverse func(context.Context) reverseReport
	}{pairIsTerminal, pairConfirm, pairInspectPeer, pairEnsurePeerKey, pairInstallPeerKey,
		pairAuthorizedKeysPath, pairGenerateKey, pairReverseReachability}
	t.Cleanup(func() {
		pairIsTerminal, pairConfirm = restore.term, restore.confirm
		pairInspectPeer, pairEnsurePeerKey = restore.inspect, restore.ensure
		pairInstallPeerKey, pairAuthorizedKeysPath = restore.install, restore.keys
		pairGenerateKey, pairReverseReachability = restore.keygen, restore.reverse
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
