package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/machines"
	"github.com/Obedience-Corp/camp/internal/pathutil"
	"github.com/Obedience-Corp/camp/internal/remote"
	"github.com/Obedience-Corp/camp/internal/ui"
)

// `camp machine pair` is the one consentful exception to D003 (diagnose, never
// manage) for LOGIN state, and it is deliberately shaped like `camp machine
// adopt`, the exception that already exists for fleet rows: an interactive
// terminal, a preview of every byte that will be written on both sides, one
// confirmation, and no flag anywhere that skips it.
//
// What the exception does NOT extend to, at any TTY: enabling a login service
// (macOS Remote Login, sshd), or touching ~/.ssh/id_*. Camp creates its own
// dedicated keys under ~/.obey/keys and appends its own authorized_keys lines.
// If the reverse direction needs a listener that is off, camp says so and
// stops — an instruction, never an action.
//
// Non-interactive callers (agents, BatchMode, scripts) can never reach any of
// this: the TTY check fails first, and there is no --yes.

// Production seams. Tests swap them so the whole consent and write path can be
// exercised without a TTY, an ssh connection, or the developer's real ~/.ssh.
var (
	pairIsTerminal          = ui.IsTerminal
	pairConfirm             = confirmForm
	pairInspectPeer         = remote.InspectPeer
	pairEnsurePeerKey       = remote.EnsurePeerKey
	pairInstallPeerKey      = remote.InstallAuthorizedKey
	pairAuthorizedKeysPath  = defaultAuthorizedKeysPath
	pairGenerateKey         = generateLocalKey
	pairReverseReachability = func(ctx context.Context) reverseReport {
		return checkReverseReachability(ctx, defaultReverseProbes())
	}
)

var machinePairCmd = &cobra.Command{
	Use:   "pair <machine>",
	Short: "Exchange ssh keys with a machine so hops work both ways",
	Long: `Exchange ssh keys with a registered machine so hops work in both directions,
after showing you exactly what will be written on each side.

Run this from the machine that can ALREADY reach the other one. One working
direction is enough to pair both: camp installs this machine's key over there,
reads that machine's key back, and installs it here. That is what makes a GUI
macOS peer reachable afterwards, since it cannot serve Tailscale SSH at all.

Camp creates dedicated ed25519 keys under ~/.obey/keys, one per direction of
one pair, so a pairing can be revoked on its own. It never touches ~/.ssh/id_*,
and it never enables a login service: if the reverse direction needs sshd or
macOS Remote Login turned on, camp tells you and stops.

Pairing is an explicit, interactive act. There is no flag that skips the
confirmation, and it cannot run without a terminal.`,
	Example: `  camp machine pair mac-studio    # from the machine that can already reach it`,
	Args:    cobra.ExactArgs(1),
	Annotations: map[string]string{
		"agent_allowed": "false",
		"agent_reason":  "Pairing changes login state and requires human consent at a TTY",
	},
	RunE: runMachinePair,
}

func init() {
	machineCmd.AddCommand(machinePairCmd)
}

func runMachinePair(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	if jsonOut, _ := cmd.Flags().GetBool("json"); jsonOut {
		return camperrors.New("camp machine pair does not support --json: exchanging keys is " +
			"an explicit consent step, not a query")
	}

	mf, err := machines.Load()
	if err != nil {
		return err
	}
	target, _, found := mf.Lookup(args[0])
	if !found {
		return camperrors.New("unknown machine \"" + args[0] + "\"; add it first with " +
			"'camp machine add " + args[0] + " --host <host>'")
	}
	if isSelfHost(ctx, target.Host) {
		return camperrors.New("camp machine pair: " + target.ID + " is this machine; " +
			"pairing exchanges keys between two machines")
	}
	if err := remote.EnsureKeyAuth(target); err != nil {
		return err
	}

	// The TTY refusal comes before any output, matching adopt: a non-interactive
	// run must not leave half a consent surface on stdout.
	if !pairIsTerminal() {
		return camperrors.New("camp machine pair requires an interactive terminal: exchanging " +
			"ssh keys is an explicit consent step and cannot be automated")
	}

	selfID, err := pairSelfID(ctx)
	if err != nil {
		return err
	}
	localKey, err := localKeyPath(target.ID)
	if err != nil {
		return err
	}
	authKeys, err := pairAuthorizedKeysPath()
	if err != nil {
		return err
	}

	// Read-only probe first. It is the channel check as well as the preview's
	// source of truth: if this fails, pairing cannot run from this direction,
	// and the classified ssh error says which direction to try instead.
	remoteKeyName := remote.PeerKeyName(selfID)
	creds, err := pairInspectPeer(ctx, target, remoteKeyName)
	if err != nil {
		return camperrors.Wrapf(err, "camp machine pair must run from a machine that can already "+
			"reach %s; this hop failed, so pair from %s instead", target.ID, target.ID)
	}

	plan := pairPlan{
		Target:        target,
		SelfID:        selfID,
		LocalKeyPath:  localKey,
		LocalKeyNew:   !fileExists(localKey),
		AuthKeysPath:  authKeys,
		RemoteKeyName: remoteKeyName,
		RemoteKeyNew:  creds.PublicKey == "",
		RemoteUser:    creds.User,
		SetSSHUser:    target.SSHUser == "" && creds.User != "",
		SetAuthMethod: target.AuthMethod != machines.AuthSSHAgent,
	}
	if _, err := fmt.Fprint(cmd.OutOrStdout(), pairPreview(plan)); err != nil {
		return err
	}

	ok, canceled, err := pairConfirm(ctx, fmt.Sprintf("Exchange keys with %q?", target.ID))
	if err != nil {
		return err
	}
	if canceled || !ok {
		_, err := fmt.Fprintln(cmd.OutOrStdout(), "Not paired. Nothing was written.")
		return err
	}

	return applyPairing(ctx, cmd, mf, plan)
}

// pairPlan is everything the preview promises and applyPairing then performs.
// Building it before the confirmation is what makes the preview honest: the
// same struct drives both, so the text cannot describe one thing and the
// writes do another.
type pairPlan struct {
	Target        *machines.Machine
	SelfID        string
	LocalKeyPath  string
	LocalKeyNew   bool
	AuthKeysPath  string
	RemoteKeyName string
	RemoteKeyNew  bool
	RemoteUser    string
	SetSSHUser    bool
	SetAuthMethod bool
}

func pairPreview(p pairPlan) string {
	var b strings.Builder
	b.WriteString("\nPairing this machine (" + p.SelfID + ") with " + p.Target.ID + ".\n\n")
	fmt.Fprintf(&b, "  channel   ssh to %s works; pairing rides it in both directions\n", p.Target.ID)
	fmt.Fprintf(&b, "  account   %s@%s\n", p.RemoteUser, p.Target.Host)

	b.WriteString("\nThis machine (" + p.SelfID + ") will:\n")
	fmt.Fprintf(&b, "  %-8s %s%s\n", verb(p.LocalKeyNew), p.LocalKeyPath, keyNote(p.LocalKeyNew))
	fmt.Fprintf(&b, "  %-8s %s's public key to %s\n", "append", p.Target.ID, p.AuthKeysPath)
	fmt.Fprintf(&b, "  %-8s machines.yaml row %q: identity_file", "update", p.Target.ID)
	if p.SetSSHUser {
		b.WriteString(", ssh_user: " + p.RemoteUser)
	}
	if p.SetAuthMethod {
		b.WriteString(", auth_method: " + machines.AuthSSHAgent)
	}
	b.WriteString("\n")

	b.WriteString("\n" + p.Target.ID + " will:\n")
	fmt.Fprintf(&b, "  %-8s ~/.obey/keys/%s%s\n", verb(p.RemoteKeyNew), p.RemoteKeyName, keyNote(p.RemoteKeyNew))
	fmt.Fprintf(&b, "  %-8s this machine's public key to ~/.ssh/authorized_keys\n", "append")

	b.WriteString("\nCamp will not enable a login service on either machine, and never touches\n" +
		"~/.ssh/id_*. Keys are per-pair, per-direction, so this pairing can be revoked\n" +
		"on its own. Appends are skipped if the key is already present.\n\n")
	return b.String()
}

func verb(isNew bool) string {
	if isNew {
		return "create"
	}
	return "reuse"
}

// keyNote says the passphrase part out loud rather than burying it. A hop runs
// under BatchMode, so a passphrase-protected key cannot serve it; the honest
// trade is stated where the operator decides, not in a doc they will not read.
func keyNote(isNew bool) string {
	if isNew {
		return "  (new ed25519, no passphrase — a hop runs unattended)"
	}
	return "  (existing)"
}

// applyPairing performs the writes in the order that leaves the least mess if
// one fails: keys first (creating a key changes no access), then the two
// authorized_keys appends, then the fleet row. A failure part-way leaves
// credentials that grant nothing rather than a fleet row pointing at a key
// that does not exist.
func applyPairing(ctx context.Context, cmd *cobra.Command, mf *machines.File, p pairPlan) error {
	out := cmd.OutOrStdout()
	comment := "camp-" + p.SelfID + "-to-" + p.Target.ID

	localPub, err := pairGenerateKey(p.LocalKeyPath, comment)
	if err != nil {
		return err
	}
	remotePub, err := pairEnsurePeerKey(ctx, p.Target, p.RemoteKeyName,
		"camp-"+p.Target.ID+"-to-"+p.SelfID)
	if err != nil {
		return err
	}

	addedRemote, err := pairInstallPeerKey(ctx, p.Target, localPub)
	if err != nil {
		return err
	}
	addedLocal, err := installLocalAuthorizedKey(p.AuthKeysPath, remotePub)
	if err != nil {
		return err
	}

	p.Target.IdentityFile = p.LocalKeyPath
	if p.SetSSHUser {
		p.Target.SSHUser = p.RemoteUser
	}
	if p.SetAuthMethod {
		p.Target.AuthMethod = machines.AuthSSHAgent
	}
	mf.Upsert(*p.Target)
	if err := mf.Save(); err != nil {
		return err
	}

	_, _ = fmt.Fprintf(out, "%s paired with %s\n", ui.SuccessIcon(), p.Target.ID)
	_, _ = fmt.Fprintf(out, "  %s -> %s: %s\n", p.SelfID, p.Target.ID, appendResult(addedRemote))
	_, _ = fmt.Fprintf(out, "  %s -> %s: %s\n", p.Target.ID, p.SelfID, appendResult(addedLocal))

	// The reverse direction needs a listener HERE. Camp can observe that half
	// and must not fix it, so the hint is printed as the operator's next step.
	if rev := pairReverseReachability(ctx); !rev.Capable {
		_, _ = fmt.Fprintf(out, "\n  %s cannot hop here yet: %s\n", p.Target.ID, rev.Hint)
	}
	_, _ = fmt.Fprintf(out, "\n  next: camp machine diagnose %s\n", p.Target.ID)
	_, err = fmt.Fprintf(out, "        on %s, 'camp machine adopt' (in a shell hopped from here) registers this\n"+
		"        machine over there so its own hops know the way back\n", p.Target.ID)
	return err
}

func appendResult(added bool) string {
	if added {
		return "key installed"
	}
	return "key already present"
}

// pairSelfID derives the id this machine is known by, which names the key the
// peer will hold for us. It is the same derivation the hop payload uses, so a
// machine cannot be called one thing by a hop and another by a pairing.
func pairSelfID(ctx context.Context) (string, error) {
	host, err := detectReachableName(ctx, runTailscaleStatusForSelf)
	if err != nil {
		return "", camperrors.Wrap(err, "camp machine pair: cannot determine this machine's name")
	}
	id := suggestedMachineID(host)
	if id == "" {
		return "", camperrors.New("camp machine pair: cannot derive a machine id for this machine from " +
			"\"" + host + "\"; set a hostname that is a valid machine id (lowercase letters, digits, hyphens)")
	}
	return id, nil
}

// localKeyPath places camp's dedicated keys beside machines.yaml, so they
// follow the same CAMP_MACHINES_PATH / XDG_CONFIG_HOME resolution the rest of
// camp's machine state does — and so a test that isolates one isolates both.
func localKeyPath(peerID string) (string, error) {
	base, err := machines.MachinesPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(base), "keys", remote.PeerKeyName(peerID)), nil
}

func defaultAuthorizedKeysPath() (string, error) {
	home, err := pathutil.Home()
	if err != nil {
		return "", camperrors.Wrap(err, "resolve home directory for ~/.ssh/authorized_keys")
	}
	return filepath.Join(home, ".ssh", "authorized_keys"), nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// generateLocalKey creates the keypair if absent and returns its public half.
// ssh-keygen is exec'd directly (no shell), so the comment and path need no
// quoting rules to be safe.
func generateLocalKey(path, comment string) (string, error) {
	if !fileExists(path) {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return "", camperrors.Wrap(err, "create camp key directory")
		}
		cmd := exec.Command("ssh-keygen", "-t", "ed25519", "-N", "", "-C", comment, "-f", path)
		if out, err := cmd.CombinedOutput(); err != nil {
			return "", camperrors.Wrapf(err, "generate %s: %s", path, strings.TrimSpace(string(out)))
		}
	}
	pub, err := os.ReadFile(path + ".pub")
	if err != nil {
		return "", camperrors.Wrapf(err, "read %s.pub", path)
	}
	return strings.TrimSpace(string(pub)), nil
}

// installLocalAuthorizedKey is the local half of InstallAuthorizedKey, applying
// the same shape check and the same 700/600 permissions sshd requires — a
// too-permissive authorized_keys is ignored by sshd, which would make pairing
// report success while hops kept failing.
func installLocalAuthorizedKey(path, pub string) (bool, error) {
	pub = strings.TrimSpace(pub)
	if !remote.IsSafeAuthorizedKey(pub) {
		return false, camperrors.New("refusing to install a public key with an unexpected shape")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return false, camperrors.Wrap(err, "create .ssh directory")
	}
	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return false, camperrors.Wrapf(err, "read %s", path)
	}
	for _, line := range strings.Split(string(existing), "\n") {
		if strings.TrimSpace(line) == pub {
			return false, nil
		}
	}
	body := string(existing)
	if body != "" && !strings.HasSuffix(body, "\n") {
		body += "\n"
	}
	if err := os.WriteFile(path, []byte(body+pub+"\n"), 0o600); err != nil {
		return false, camperrors.Wrapf(err, "write %s", path)
	}
	return true, nil
}
