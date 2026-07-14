// Package peer resolves a transfer source for campaign git repositories: a
// campaign root on another machine (from ~/.obey/machines.yaml, reached over
// ssh) or on the local filesystem (a mounted volume or second checkout). It
// builds the git-facing plumbing for fetching from that source: repository
// URLs, a namespaced refspec, and a GIT_SSH_COMMAND that rides the same ssh
// identity and ControlMaster multiplexing as the rest of camp's multi-machine
// stack. The peer is a transfer optimization only: content fetched from a
// peer is verified by git on arrival exactly like content fetched from
// origin, and origin remains the configured origin.
package peer

import (
	"context"
	"os"
	"os/exec"
	"path"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/machines"
	"github.com/Obedience-Corp/camp/internal/remote"
)

// Source is a resolved peer campaign root to fetch git objects from.
type Source struct {
	id      string
	root    string
	target  string // ssh target (user@host); empty for filesystem sources
	sshOpts []string
}

// ErrPeerConfig marks a peer-resolution failure that is a configuration or
// usage error — an unknown machine id, the reserved "local" id, or unusable
// auth — as opposed to a reachability failure (the peer is configured but
// offline/unresolvable). Callers hard-fail on ErrPeerConfig so a typo or
// misconfiguration is caught immediately, but degrade to the origin path on
// reachability failures, matching the documented "unreachable peer degrades
// to a warning" contract.
var ErrPeerConfig = camperrors.New("peer configuration error")

// FromMachine resolves machineID in ~/.obey/machines.yaml and asks the far
// machine's own camp where campaign `remainder` lives (remote registry
// resolution via `camp switch --print`, never the local filesystem).
//
// Configuration failures (unknown id, "local", bad auth) are wrapped with
// ErrPeerConfig; a reachability failure from resolving the root on the peer
// is returned as-is so callers can degrade rather than abort.
func FromMachine(ctx context.Context, machineID, remainder string) (*Source, error) {
	file, err := machines.Load()
	if err != nil {
		return nil, camperrors.WrapJoinf(ErrPeerConfig, err, "load machines file")
	}
	m, isLocal, found := file.Lookup(machineID)
	if !found {
		return nil, camperrors.WrapJoinf(ErrPeerConfig, nil, "machine %q not found in %s (see 'camp machine list')",
			machineID, machines.MachinesPath())
	}
	if isLocal {
		return nil, camperrors.WrapJoinf(ErrPeerConfig, nil, "machine %q is this machine; --from needs a different machine", machineID)
	}
	if err := remote.EnsureKeyAuth(m); err != nil {
		return nil, camperrors.WrapJoin(ErrPeerConfig, err, "")
	}
	root, err := remote.ResolveRoot(ctx, m, remainder)
	if err != nil {
		// Reachability/resolution failure: not wrapped with ErrPeerConfig, so
		// callers degrade to origin.
		return nil, err
	}
	return &Source{id: m.ID, root: root, target: remote.Target(m), sshOpts: remote.Opts(m)}, nil
}

// FromPath builds a filesystem source rooted at an absolute campaign root on
// this machine (a mounted volume or a second checkout).
func FromPath(id, root string) *Source {
	return &Source{id: id, root: root}
}

// ID returns the source's machine id, used in refspecs and messages.
func (s *Source) ID() string { return s.id }

// IsFilesystem reports whether the source is a local filesystem path rather
// than an ssh machine. Callers cloning submodules from a filesystem source
// need `-c protocol.file.allow=always` (CVE-2022-39253 restricts file
// transport in submodule contexts; the URL here is camp-constructed from the
// user's own peer selection, not attacker-controlled .gitmodules content,
// matching the precedent in internal/project/add.go).
func (s *Source) IsFilesystem() bool { return s.target == "" }

// Root returns the campaign root path on the peer.
func (s *Source) Root() string { return s.root }

// URL returns the git URL for the repository at relPath under the peer's
// campaign root; "" addresses the campaign root repository itself.
func (s *Source) URL(relPath string) string {
	p := path.Join(s.root, relPath)
	if s.target == "" {
		return p
	}
	return "ssh://" + s.target + p
}

// Refspecs returns the refspecs peer fetches use. Heads land under
// refs/peer/<id>/* so transferred objects stay reachable (no GC exposure)
// without touching origin's remote-tracking refs. HEAD is included so a
// detached gitlink SHA (common for recorded submodule commits that are not
// branch tips) still transfers when the peer checkout is detached there.
func (s *Source) Refspecs() []string {
	ns := "refs/peer/" + s.id
	return []string{
		"+HEAD:" + ns + "/HEAD",
		"+refs/heads/*:" + ns + "/*",
	}
}

// Refspec returns the primary heads refspec (kept for callers/tests that
// only need the heads mapping). Prefer Refspecs for fetches.
func (s *Source) Refspec() string {
	return s.Refspecs()[1]
}

// GitEnv returns the environment for git commands that dial this source:
// os.Environ plus a GIT_SSH_COMMAND carrying the machine's ssh options
// (BatchMode, ConnectTimeout, ControlMaster multiplexing, identity file).
// Filesystem sources need no override and return nil, which keeps the
// calling process environment for exec.Cmd.
func (s *Source) GitEnv() []string {
	if s.target == "" {
		return nil
	}
	parts := make([]string, 0, len(s.sshOpts)+1)
	parts = append(parts, "ssh")
	for _, o := range s.sshOpts {
		parts = append(parts, remote.ShellQuote(o))
	}
	return append(os.Environ(), "GIT_SSH_COMMAND="+strings.Join(parts, " "))
}

// Fetch fetches heads and HEAD from the peer copy of the repository at
// relPath into the local repository at dir, under the Refspecs namespace.
// It moves objects only: refs outside refs/peer/<id>/* and the working tree
// are untouched.
func (s *Source) Fetch(ctx context.Context, dir, relPath string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	args := []string{"-C", dir, "fetch", "--no-tags", s.URL(relPath)}
	args = append(args, s.Refspecs()...)
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Env = s.GitEnv()
	if output, err := cmd.CombinedOutput(); err != nil {
		return camperrors.Wrapf(err, "peer fetch %q from %s: %s", relPath, s.id, strings.TrimSpace(string(output)))
	}
	return nil
}
