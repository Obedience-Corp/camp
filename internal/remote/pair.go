package remote

import (
	"context"
	"regexp"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/machines"
)

// This file holds the far-side half of `camp machine pair` (design WI-44f57e
// Q3/Q4): read what credentials a peer already has, create a keypair there,
// and install a public key into its authorized_keys.
//
// Pairing bootstraps over the direction that ALREADY works. One working
// direction is enough to pair both, because ssh gives us read and write on the
// far side: we install our key over there, and we read their key back and
// install it here. That is why nothing in this file needs the broken direction
// to come up first — and why a GUI macOS peer, which cannot serve Tailscale
// SSH, can still end up reachable.
//
// Every function here writes only under the peer's $HOME, and only ~/.ssh and
// ~/.obey/keys within it. None of them enables a login service: that stays
// outside camp's consent boundary at any TTY (D003 amendment).

// PeerKeyName is the file name of the dedicated keypair camp maintains on a
// machine for reaching one specific peer. Keying by peer id (rather than one
// shared camp key) is what makes a pairing revocable on its own: delete the
// key, remove the one authorized_keys line, and only that direction of that
// pair stops working.
func PeerKeyName(peerID string) string {
	return "camp_" + peerID + "_ed25519"
}

// safeKeyName bounds what can be interpolated into a remote shell script as a
// file name. Machine ids are already validated lowercase/digits/hyphens by the
// time they reach PeerKeyName, so this is a second, local guarantee rather
// than the only one.
var safeKeyName = regexp.MustCompile(`^camp_[a-z0-9-]+_ed25519$`)

// safeAuthorizedKey bounds what can be interpolated into a remote shell script
// as an authorized_keys line. The line is appended to a file that grants
// login, so the shape is asserted rather than assumed: an ed25519 key, its
// base64 body, and an optional comment restricted to characters that are inert
// in a double-quoted shell word (no $, no backtick, no quote, no backslash).
var safeAuthorizedKey = regexp.MustCompile(`^ssh-ed25519 [A-Za-z0-9+/=]+(?: [A-Za-z0-9._@-]+)?$`)

// PeerCredentials is what a read-only probe can learn about a peer before the
// operator has consented to anything.
type PeerCredentials struct {
	// User is the account ssh logged into over there, which is the value a
	// fleet row's ssh_user needs and the one thing an operator most often has
	// to discover by hand.
	User string
	// PublicKey is the peer's existing camp key for reaching us, or "" when it
	// has none yet.
	PublicKey string
}

// InspectPeer reads what the peer already has: the account name, and the
// public half of its camp key for reaching us if one exists. Read-only by
// construction — it creates nothing — so it is safe to run BEFORE consent, and
// it doubles as the channel check: if this fails, pairing cannot proceed from
// this direction and the classified ssh error says why.
func InspectPeer(ctx context.Context, m *machines.Machine, keyName string) (PeerCredentials, error) {
	if !safeKeyName.MatchString(keyName) {
		return PeerCredentials{}, camperrors.New("refusing to probe an unexpected key name: " + keyName)
	}
	script := `id -un; f="$HOME/.obey/keys/` + keyName + `.pub"; ` +
		`if [ -f "$f" ]; then cat "$f"; else echo -; fi`
	out, err := runPeerScript(ctx, m, script)
	if err != nil {
		return PeerCredentials{}, err
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) < 2 {
		return PeerCredentials{}, camperrors.New("unexpected response while inspecting " + m.ID)
	}
	creds := PeerCredentials{User: strings.TrimSpace(lines[0])}
	if pub := strings.TrimSpace(lines[len(lines)-1]); pub != "-" {
		creds.PublicKey = pub
	}
	return creds, nil
}

// EnsurePeerKey creates the peer's dedicated keypair if it does not have one
// and returns its public half. Post-consent only: it writes.
//
// The key is generated without a passphrase, which is the honest trade and is
// said out loud in the pairing preview: a passphrase-protected key cannot be
// used by a hop that runs under BatchMode, so the alternative is not "a safer
// pairing" but "no pairing". It lives in ~/.obey/keys with a 077 umask, and it
// is one key for one direction of one pair.
func EnsurePeerKey(ctx context.Context, m *machines.Machine, keyName, comment string) (string, error) {
	if !safeKeyName.MatchString(keyName) {
		return "", camperrors.New("refusing to create an unexpected key name: " + keyName)
	}
	if !safeKeyComment.MatchString(comment) {
		return "", camperrors.New("refusing to use an unexpected key comment: " + comment)
	}
	script := `umask 077; mkdir -p "$HOME/.obey/keys"; f="$HOME/.obey/keys/` + keyName + `"; ` +
		`if [ ! -f "$f" ]; then ssh-keygen -t ed25519 -N "" -C "` + comment + `" -f "$f" >/dev/null 2>&1 || exit 3; fi; ` +
		`cat "$f.pub"`
	out, err := runPeerScript(ctx, m, script)
	if err != nil {
		return "", camperrors.Wrapf(err, "create a camp key on %s", m.ID)
	}
	pub := strings.TrimSpace(string(out))
	if pub == "" {
		return "", camperrors.New("no public key came back from " + m.ID)
	}
	return pub, nil
}

// safeKeyComment bounds the -C comment for the same reason safeAuthorizedKey
// bounds the key line: it is interpolated into a double-quoted shell word.
var safeKeyComment = regexp.MustCompile(`^[A-Za-z0-9._@-]+$`)

// InstallAuthorizedKey appends pub to the peer's ~/.ssh/authorized_keys unless
// it is already there, and reports whether it added a line. Idempotent, so
// re-pairing an already-paired machine is a no-op rather than a file that
// grows a duplicate every time.
//
// It creates ~/.ssh and authorized_keys with the permissions sshd insists on
// (700/600) — a file created too permissively is silently ignored by sshd,
// which would make pairing "succeed" and hops keep failing.
func InstallAuthorizedKey(ctx context.Context, m *machines.Machine, pub string) (added bool, err error) {
	pub = strings.TrimSpace(pub)
	if !safeAuthorizedKey.MatchString(pub) {
		return false, camperrors.New("refusing to install a public key with an unexpected shape")
	}
	script := `umask 077; mkdir -p "$HOME/.ssh"; chmod 700 "$HOME/.ssh"; ` +
		`touch "$HOME/.ssh/authorized_keys"; chmod 600 "$HOME/.ssh/authorized_keys"; ` +
		`if grep -qxF "` + pub + `" "$HOME/.ssh/authorized_keys"; then echo present; ` +
		`else printf "%s\n" "` + pub + `" >> "$HOME/.ssh/authorized_keys"; echo added; fi`
	out, err := runPeerScript(ctx, m, script)
	if err != nil {
		return false, camperrors.Wrapf(err, "install a public key on %s", m.ID)
	}
	return strings.TrimSpace(string(out)) == "added", nil
}

// IsSafeAuthorizedKey reports whether pub has the shape camp is willing to
// write into an authorized_keys file, on either side of a pairing. Exported so
// the local half applies exactly the same rule as the remote half.
func IsSafeAuthorizedKey(pub string) bool {
	return safeAuthorizedKey.MatchString(strings.TrimSpace(pub))
}

// runPeerScript runs one /bin/sh script on the peer through its login shell,
// over the same endpoint resolution (and thus the same MagicDNS fallback and
// ControlMaster socket) every other camp hop uses.
func runPeerScript(ctx context.Context, m *machines.Machine, script string) ([]byte, error) {
	if err := EnsureKeyAuth(m); err != nil {
		return nil, err
	}
	e := ResolveEndpoint(ctx, m)
	return Run(ctx, e.Target(), e.Opts(), LoginShellCommand(script))
}
