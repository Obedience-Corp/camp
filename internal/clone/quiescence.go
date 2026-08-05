package clone

import (
	"context"
	"errors"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/pathutil"
	"github.com/Obedience-Corp/camp/internal/peer"
	"github.com/Obedience-Corp/camp/internal/remote"
)

// The quiescence contract (decision D001) is what makes a byte copy of a
// peer's `.git/objects/pack` safe: camp only trusts pack bytes from a
// repository the peer is not writing to. One batched script over the peer's
// existing ControlMaster connection answers three questions per repository —
// is the working tree clean, is any git operation in flight, is HEAD readable
// — for the campaign root and every submodule, so the verdict is per-repo and
// the fallback stays surgical. The recorded HEAD is the other half: it is
// re-verified after the copy so the race window between check and copy becomes
// a detected condition instead of a corrupt clone.
//
// The wire format is deliberately trivial and version-free: framing markers
// around one tab-separated line per repository. Anything camp cannot parse is
// an error, never a quiescent verdict — "camp does not know" must not read as
// "safe to copy".

const (
	// quiescenceBeginMarker frames the start of the report. A login shell may
	// print a banner before the script runs, so everything before this line is
	// somebody else's output and is discarded.
	quiescenceBeginMarker = "CAMP-QUIESCENCE-BEGIN"
	// quiescenceEndMarker terminates the report. Its absence means the stream
	// was cut short, which is the case that must never read as quiescent.
	quiescenceEndMarker = "CAMP-QUIESCENCE-END"
	// quiescenceFields is the exact field count of a repo line:
	// repo, status, busy, head, gitdir.
	quiescenceFields = 5
	// quiescenceEmpty is the placeholder the script emits for an absent value,
	// so no field is ever empty and a short line is unambiguously truncation.
	quiescenceEmpty = "-"
	// quiescenceRootRepo is the repo name the script reports for the campaign
	// root repository itself.
	quiescenceRootRepo = "."
)

// Working-tree statuses the peer script emits. Any other value is treated as
// non-quiescent (fail closed) rather than ignored.
const (
	quiescenceStatusClean   = "clean"
	quiescenceStatusDirty   = "dirty"
	quiescenceStatusUnknown = "unknown"
	quiescenceStatusMissing = "missing"
)

// ErrQuiescenceProtocol marks output camp cannot trust as a quiescence report:
// missing framing, a short line, a stream that stopped before the terminator,
// or a repo path that escapes the peer's campaign root. It is deliberately
// distinct from a non-quiescent verdict — a non-quiescent peer is a normal
// outcome that selects the bundle fallback, while unparseable output means the
// check itself did not happen.
var ErrQuiescenceProtocol = errors.New("malformed quiescence report from peer")

// RepoVerdict is one repository's quiescence verdict on the peer.
type RepoVerdict struct {
	// Repo is the repository path relative to the peer's campaign root, or
	// "." for the campaign root repository itself.
	Repo string `json:"repo"`
	// Quiescent reports whether this repository may be byte-copied.
	Quiescent bool `json:"quiescent"`
	// Reasons lists why the repository is not quiescent (empty when it is).
	Reasons []string `json:"reasons,omitempty"`
	// HeadSHA is the repository's HEAD commit at check time, used to re-verify
	// after the copy that the peer did not move underneath it. Empty when HEAD
	// was unreadable, which is itself disqualifying.
	HeadSHA string `json:"head_sha,omitempty"`
	// GitDir is the absolute git directory on the peer, which is where the
	// pack and ref bytes are read from. It is reported rather than derived
	// because a submodule's git directory is .git/modules/<name>, not
	// <path>/.git, and the peer's own git already resolved it during the
	// check. Empty when the repo reported missing.
	GitDir string `json:"git_dir,omitempty"`
}

// QuiescenceReport is a peer's verdicts for a whole campaign.
type QuiescenceReport struct {
	// MachineID is the peer the report came from.
	MachineID string `json:"machine_id"`
	// Root is the campaign root on the peer the verdicts describe.
	Root string `json:"root"`
	// Repos holds one verdict per repository, campaign root first.
	Repos []RepoVerdict `json:"repos"`
}

// Quiescent reports whether every repository in the report may be byte-copied.
// An empty report is not quiescent: no verdicts means nothing was verified.
func (r *QuiescenceReport) Quiescent() bool {
	if r == nil || len(r.Repos) == 0 {
		return false
	}
	for _, v := range r.Repos {
		if !v.Quiescent {
			return false
		}
	}
	return true
}

// NonQuiescent returns the repositories that failed the contract, in report
// order, so callers can both explain the fallback and scope it per repo.
func (r *QuiescenceReport) NonQuiescent() []RepoVerdict {
	if r == nil {
		return nil
	}
	var failed []RepoVerdict
	for _, v := range r.Repos {
		if !v.Quiescent {
			failed = append(failed, v)
		}
	}
	return failed
}

// Verdict returns the verdict for one repository path. The second result is
// false when the peer reported nothing for that repo, which callers must treat
// as "not verified" rather than as a passing verdict.
func (r *QuiescenceReport) Verdict(repo string) (RepoVerdict, bool) {
	if r == nil {
		return RepoVerdict{}, false
	}
	for _, v := range r.Repos {
		if v.Repo == repo {
			return v, true
		}
	}
	return RepoVerdict{}, false
}

// CheckQuiescence collects per-repo quiescence verdicts from src in a single
// batched round-trip (D001). The peer's own git answers every question, so the
// verdict reflects the peer's real state rather than an inference from local
// data. Failure to reach or parse the peer is an error, never a verdict.
func CheckQuiescence(ctx context.Context, src *peer.Source) (*QuiescenceReport, error) {
	if src == nil {
		return nil, camperrors.New("quiescence check requires a peer source")
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	root := src.Root()
	out, err := src.RunShell(ctx, QuiescenceScript(root))
	if err != nil {
		return nil, camperrors.Wrapf(err, "quiescence check on peer %s", src.ID())
	}
	report, err := ParseQuiescenceReport(src.ID(), root, out)
	if err != nil {
		return nil, camperrors.Wrapf(err, "quiescence check on peer %s", src.ID())
	}
	return report, nil
}

// QuiescenceScript builds the POSIX shell script camp runs on a peer. It is a
// pure function of the campaign root, and together with ParseQuiescenceReport
// it is the whole contract — transport is the only other moving part. Keeping
// the two exported separately is what lets the container harness run the real
// script against real repositories over real ssh and feed the real parser,
// rather than testing a paraphrase of either.
func QuiescenceScript(root string) string {
	return "set -u\nroot=" + remote.ShellQuote(root) + "\n" + quiescenceScriptBody
}

// ParseQuiescenceReport turns raw output of QuiescenceScript into a report,
// whatever moved the bytes. root is the campaign root the output describes and
// is used to confirm reported repo paths stay inside it.
func ParseQuiescenceReport(machineID, root string, out []byte) (*QuiescenceReport, error) {
	verdicts, err := parseQuiescence(out, root)
	if err != nil {
		return nil, err
	}
	return &QuiescenceReport{MachineID: machineID, Root: root, Repos: verdicts}, nil
}

// quiescenceScriptBody reports the campaign root plus every submodule declared
// in the peer's .gitmodules, one line each, framed by the begin/end markers.
//
// Notes on the shell, because they are load-bearing:
//   - busy is collected before `git status` so the probe cannot flag its own
//     lock, and status runs with --no-optional-locks so a read-only check never
//     writes to the peer's index.
//   - status ignores submodule work-tree dirt (--ignore-submodules=dirty).
//     Without it a superproject inherits every submodule's dirt and the root
//     repo can never be quiescent, which is D001's rejected "one dirty repo
//     poisons the whole seed" reintroduced by accident. A moved gitlink is
//     still the superproject's own uncommitted change and is still reported.
//   - the git directory is resolved with rev-parse rather than assumed to be
//     "$dir/.git": a submodule's .git is a file pointing into
//     .git/modules/<name>, which is where its index.lock actually lives.
//   - "$dir/.git" must exist before rev-parse runs. An uninitialized submodule
//     is an empty directory inside the campaign repository, and rev-parse from
//     inside one walks UP and answers for the campaign root — reporting the
//     root's clean status and HEAD under the submodule's name. That is the
//     precise shape of a false quiescent verdict, so the guard is load-bearing,
//     not a tidiness check.
//   - locks under refs/ count as busy too (D006). A concurrent fetch holds
//     refs/heads/<name>.lock while writing new objects into the very pack
//     directory a cold seed copies, and the git dir's own *.lock glob does not
//     see it. Capped at five so a pathological repo cannot produce an
//     unbounded line; the verdict is non-quiescent either way.
//   - every field falls back to "-" so a repo line always has four fields and a
//     short line can only mean a truncated stream.
const quiescenceScriptBody = `camp_report_repo() {
	repo=$1
	dir=$2
	if [ ! -e "$dir/.git" ]; then
		printf '%s\tmissing\t-\t-\t-\n' "$repo"
		return
	fi
	gitdir=$(git -C "$dir" rev-parse --absolute-git-dir 2>/dev/null) || gitdir=
	if [ -z "$gitdir" ]; then
		printf '%s\tmissing\t-\t-\t-\n' "$repo"
		return
	fi
	busy=
	for entry in "$gitdir"/*.lock; do
		[ -e "$entry" ] || continue
		busy=${busy:+$busy,}${entry##*/}
	done
	for marker in MERGE_HEAD CHERRY_PICK_HEAD REVERT_HEAD BISECT_LOG rebase-merge rebase-apply; do
		[ -e "$gitdir/$marker" ] || continue
		busy=${busy:+$busy,}$marker
	done
	if [ -d "$gitdir/refs" ]; then
		reflocks=$(find "$gitdir/refs" -name '*.lock' 2>/dev/null | head -n 5)
		for entry in $reflocks; do
			busy=${busy:+$busy,}refs/${entry##*/}
		done
	fi
	[ -n "$busy" ] || busy=-
	if porcelain=$(git --no-optional-locks -C "$dir" status --porcelain --ignore-submodules=dirty 2>/dev/null); then
		if [ -n "$porcelain" ]; then
			status=dirty
		else
			status=clean
		fi
	else
		status=unknown
	fi
	head=$(git -C "$dir" rev-parse HEAD 2>/dev/null) || head=
	[ -n "$head" ] || head=-
	printf '%s\t%s\t%s\t%s\t%s\n' "$repo" "$status" "$busy" "$head" "$gitdir"
}
printf '%s\n' 'CAMP-QUIESCENCE-BEGIN'
camp_report_repo . "$root"
git -C "$root" config -f "$root/.gitmodules" --get-regexp '^submodule\..*\.path$' 2>/dev/null |
while IFS= read -r line; do
	sub=${line#* }
	[ -n "$sub" ] || continue
	[ "$sub" != "$line" ] || continue
	camp_report_repo "$sub" "$root/$sub"
done
printf '%s\n' 'CAMP-QUIESCENCE-END'
`

// parseQuiescence turns the peer script's stdout into per-repo verdicts. It is
// pure — no ssh, no filesystem — so the whole fixture matrix is a unit test.
// root is used only to validate that reported repo paths stay inside the peer's
// campaign root.
func parseQuiescence(out []byte, root string) ([]RepoVerdict, error) {
	var (
		verdicts []RepoVerdict
		started  bool
		ended    bool
		lastRepo string
	)

	for _, raw := range strings.Split(string(out), "\n") {
		line := strings.TrimRight(raw, "\r")
		if !started {
			// Login-shell banners land here; the report has not started yet.
			started = line == quiescenceBeginMarker
			continue
		}
		if line == quiescenceEndMarker {
			ended = true
			break
		}
		if strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != quiescenceFields {
			return nil, fieldCountError(fields, lastRepo)
		}
		if err := validateRepoPath(fields[0], root); err != nil {
			return nil, err
		}
		verdicts = append(verdicts, verdictFromFields(fields))
		lastRepo = fields[0]
	}

	if !started {
		return nil, camperrors.WrapJoin(ErrQuiescenceProtocol, nil,
			"peer produced no report (begin marker missing); it may not have run the check at all")
	}
	if !ended {
		return nil, truncatedStreamError(lastRepo)
	}
	return verdicts, nil
}

// verdictFromFields maps one well-formed report line to a verdict. Every branch
// that is not an affirmative "clean, idle, HEAD readable" adds a reason, and a
// verdict is quiescent only when it collected none, so an unrecognized status
// from a future peer fails closed.
func verdictFromFields(fields []string) RepoVerdict {
	v := RepoVerdict{Repo: fields[0]}

	switch status := fields[1]; status {
	case quiescenceStatusClean:
	case quiescenceStatusDirty:
		v.Reasons = append(v.Reasons, "uncommitted changes in the working tree")
	case quiescenceStatusMissing:
		v.Reasons = append(v.Reasons, "not a git repository on the peer")
	case quiescenceStatusUnknown:
		v.Reasons = append(v.Reasons, "peer could not report working-tree status")
	default:
		v.Reasons = append(v.Reasons, "unrecognized working-tree status "+status)
	}

	if busy := fields[2]; busy != quiescenceEmpty {
		v.Reasons = append(v.Reasons, "git operation in progress: "+busy)
	}

	if head := fields[3]; head == quiescenceEmpty {
		v.Reasons = append(v.Reasons, "HEAD unreadable")
	} else {
		v.HeadSHA = head
	}

	if gitDir := fields[4]; gitDir != quiescenceEmpty {
		v.GitDir = gitDir
	}

	v.Quiescent = len(v.Reasons) == 0
	return v
}

// validateRepoPath rejects a reported repo path that is not a plain relative
// path under the peer's campaign root. The paths come from the peer's
// .gitmodules, and downstream slices join them to build copy destinations, so
// containment is checked where the value enters camp rather than at each use.
func validateRepoPath(repo, root string) error {
	if repo == quiescenceRootRepo {
		return nil
	}
	if err := pathutil.ValidateSubmodulePath(root, repo); err != nil {
		return camperrors.WrapJoinf(ErrQuiescenceProtocol, err,
			"peer reported unusable repo path %q", repo)
	}
	return nil
}

// fieldCountError reports a line that did not carry exactly the expected
// fields — a stream cut mid-line, or a repo path that itself contained a tab.
// The repo name is the first field when the line got that far, so the operator
// learns which repository the report went wrong on.
func fieldCountError(fields []string, lastRepo string) error {
	repo := ""
	if len(fields) > 0 {
		repo = strings.TrimSpace(fields[0])
	}
	switch {
	case repo != "":
		return camperrors.WrapJoinf(ErrQuiescenceProtocol, nil,
			"report line for repo %q carried %d fields, want %d", repo, len(fields), quiescenceFields)
	case lastRepo != "":
		return camperrors.WrapJoinf(ErrQuiescenceProtocol, nil,
			"report line after repo %q carried %d fields, want %d", lastRepo, len(fields), quiescenceFields)
	default:
		return camperrors.WrapJoinf(ErrQuiescenceProtocol, nil,
			"first report line carried %d fields, want %d", len(fields), quiescenceFields)
	}
}

// truncatedStreamError reports a stream that stopped between repo lines. This
// is the case a lenient parser would silently read as "every repo I saw was
// clean", so it names where the report stopped and refuses the whole report.
func truncatedStreamError(lastRepo string) error {
	if lastRepo == "" {
		return camperrors.WrapJoin(ErrQuiescenceProtocol, nil,
			"report ended before any repo verdict")
	}
	return camperrors.WrapJoinf(ErrQuiescenceProtocol, nil,
		"report ended after repo %q without the end marker", lastRepo)
}
