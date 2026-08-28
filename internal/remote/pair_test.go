package remote

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Obedience-Corp/camp/internal/machines"
)

func TestPeerKeyName(t *testing.T) {
	if got := PeerKeyName("mac-studio"); got != "camp_mac-studio_ed25519" {
		t.Errorf("PeerKeyName = %q", got)
	}
	if !safeKeyName.MatchString(PeerKeyName("archdtop")) {
		t.Error("PeerKeyName must produce a name its own validator accepts")
	}
}

// The authorized_keys shape check is a security boundary, not tidiness: the
// key is interpolated into a double-quoted word in a remote shell script and
// appended to a file that grants login.
func TestIsSafeAuthorizedKey(t *testing.T) {
	const good = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIBoGusWTiL6ZQ1Zx2vKZ8kMdT0nT9RhP3xQwLmNvBqYz camp-a-to-b"
	if !IsSafeAuthorizedKey(good) {
		t.Fatalf("well-formed key rejected: %q", good)
	}
	if !IsSafeAuthorizedKey(good + "\n") {
		t.Error("a trailing newline should be trimmed, not rejected")
	}

	bad := []string{
		`ssh-ed25519 AAAA" ; rm -rf ~ ; echo "`, // closes the quoted word
		"ssh-ed25519 AAAA $(whoami)",            // command substitution
		"ssh-ed25519 AAAA `id`",                 // backtick substitution
		`ssh-ed25519 AAAA \x60id\x60`,           // backslash
		"ssh-ed25519 AAAA comment with spaces",
		`command="rm -rf /" ssh-ed25519 AAAA`, // forced-command prefix
		"ssh-rsa AAAAB3NzaC1yc2E",             // camp only writes its own ed25519 keys
		"ssh-ed25519",
		"",
		"ssh-ed25519 AAAA\nssh-ed25519 BBBB", // a second line
	}
	for _, k := range bad {
		if IsSafeAuthorizedKey(k) {
			t.Errorf("accepted an unsafe authorized_keys line: %q", k)
		}
	}
}

// Every writing entry point validates before it dials, so a malformed input
// cannot reach the far machine at all. A nil-host machine would fail any real
// ssh attempt, which is what makes "no error about the network" proof that no
// connection was attempted.
func TestPairWritersValidateBeforeDialing(t *testing.T) {
	ctx := context.Background()
	m := &machines.Machine{ID: "peer", Host: "peer.example.ts.net", AuthMethod: machines.AuthSSHAgent}

	if _, err := InstallAuthorizedKey(ctx, m, "not-a-key"); err == nil ||
		!strings.Contains(err.Error(), "unexpected shape") {
		t.Errorf("InstallAuthorizedKey err = %v, want a shape refusal", err)
	}
	if _, err := EnsurePeerKey(ctx, m, "id_rsa", "camp-a-to-b"); err == nil ||
		!strings.Contains(err.Error(), "unexpected key name") {
		t.Errorf("EnsurePeerKey err = %v, want a key-name refusal", err)
	}
	if _, err := EnsurePeerKey(ctx, m, PeerKeyName("a"), `bad" comment`); err == nil ||
		!strings.Contains(err.Error(), "unexpected key comment") {
		t.Errorf("EnsurePeerKey err = %v, want a comment refusal", err)
	}
	if _, err := InspectPeer(ctx, m, "../../etc/passwd"); err == nil ||
		!strings.Contains(err.Error(), "unexpected key name") {
		t.Errorf("InspectPeer err = %v, want a key-name refusal", err)
	}
}

// Password-auth machines are rejected for pairing exactly as they are for
// hops: v1 is key auth only, and pairing must not become the one path that
// prompts.
func TestPairRefusesPasswordAuthMachine(t *testing.T) {
	m := &machines.Machine{ID: "peer", Host: "peer.ts.net", AuthMethod: machines.AuthSSHPassword}
	if _, err := InspectPeer(context.Background(), m, PeerKeyName("a")); err == nil ||
		!strings.Contains(err.Error(), "password auth") {
		t.Errorf("err = %v, want the password-auth refusal", err)
	}
}

// zsh's builtin echo treats a lone "-" as an end-of-options marker and prints
// an empty line instead of a dash, which collapsed inspectScript's two-line
// output to one and made InspectPeer fail on every zsh peer with no camp key
// yet. This runs the exact script InspectPeer sends under both a POSIX shell
// and zsh (skipped if zsh is not on PATH) to pin the fix across both.
func TestInspectScriptTwoLinesAcrossShells(t *testing.T) {
	const keyName = "camp_peer_ed25519"

	shells := []struct {
		name string
		bin  string
		args []string
	}{
		{name: "sh", bin: "sh", args: []string{"-c"}},
		{name: "zsh", bin: "zsh", args: []string{"-lc"}},
	}

	cases := []struct {
		name    string
		withKey bool
		want    string
	}{
		{name: "no key file", want: "-"},
		{name: "existing key file", withKey: true, want: "ssh-ed25519 AAAAtest camp-a-to-b"},
	}

	for _, sh := range shells {
		t.Run(sh.name, func(t *testing.T) {
			if _, err := exec.LookPath(sh.bin); err != nil {
				t.Skipf("%s not on PATH", sh.bin)
			}

			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					home := t.TempDir()
					t.Setenv("HOME", home)
					if tc.withKey {
						keyDir := filepath.Join(home, ".obey", "keys")
						if err := os.MkdirAll(keyDir, 0o700); err != nil {
							t.Fatalf("mkdir key dir: %v", err)
						}
						pubPath := filepath.Join(keyDir, keyName+".pub")
						if err := os.WriteFile(pubPath, []byte(tc.want+"\n"), 0o600); err != nil {
							t.Fatalf("write fixture key: %v", err)
						}
					}

					args := append(append([]string{}, sh.args...), inspectScript(keyName))
					out, err := exec.Command(sh.bin, args...).Output()
					if err != nil {
						t.Fatalf("run inspect script under %s: %v", sh.bin, err)
					}

					lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
					if len(lines) != 2 {
						t.Fatalf("%s output = %q, want exactly 2 lines", sh.bin, out)
					}
					if lines[1] != tc.want {
						t.Errorf("%s second line = %q, want %q", sh.bin, lines[1], tc.want)
					}
				})
			}
		})
	}
}
