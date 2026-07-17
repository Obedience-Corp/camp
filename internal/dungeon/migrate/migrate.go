// Package migrate converts every dungeon in a campaign from the legacy
// visible "dungeon" spelling to the hidden ".dungeon" spelling in one sweep.
//
// The sweep is all-or-nothing by design. A campaign holding both spellings is
// the broken state the resolver rejects, so a migration that gave up halfway
// would leave the campaign worse than it started. Plan therefore validates
// every move up front and refuses the whole run if any of them cannot be made.
package migrate

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Obedience-Corp/camp/internal/dungeon/spelling"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/git"
)

// Move relocates one dungeon directory.
type Move struct {
	// From is the existing visible dungeon path.
	From string
	// To is the hidden path it becomes.
	To string
}

// Plan is a validated set of moves for a campaign.
type Plan struct {
	// CampaignRoot is the campaign the plan applies to.
	CampaignRoot string
	// Moves are ordered deepest first.
	Moves []Move
	// AlreadyHidden lists dungeons that need no work.
	AlreadyHidden []string
	// Git reports whether CampaignRoot is a git repository, and so whether
	// Apply stages the renames for a commit.
	Git bool
	// Committable reports whether the migration can land as a commit: a git
	// repository with a commit to build on. A campaign whose scaffold has
	// never been committed has no history to preserve, so its moves just land
	// in the working tree.
	Committable bool
}

// Empty reports whether the campaign is already fully migrated.
func (p *Plan) Empty() bool { return len(p.Moves) == 0 }

// BuildPlan discovers every dungeon under campaignRoot and plans the moves
// that convert the campaign to the hidden spelling.
//
// It returns an error without planning anything when a parent already holds
// both spellings (which needs a human to decide what to keep) or when a
// hidden target is already occupied.
func BuildPlan(ctx context.Context, campaignRoot string) (*Plan, error) {
	if err := ctx.Err(); err != nil {
		return nil, camperrors.Wrap(err, "context cancelled")
	}

	absRoot, err := filepath.Abs(campaignRoot)
	if err != nil {
		return nil, camperrors.Wrapf(err, "resolving campaign root %s", campaignRoot)
	}

	found, err := spelling.Discover(ctx, absRoot)
	if err != nil {
		return nil, camperrors.Wrap(err, "discovering campaign dungeons")
	}

	if err := checkConflicts(absRoot, found); err != nil {
		return nil, err
	}

	plan := &Plan{CampaignRoot: absRoot, Git: git.IsRepo(absRoot)}
	if plan.Git {
		plan.Committable = hasCommits(ctx, absRoot)
	}
	for _, d := range found {
		if d.Hidden() {
			plan.AlreadyHidden = append(plan.AlreadyHidden, d.Path)
			continue
		}
		to := filepath.Join(d.Parent, spelling.Hidden)
		if _, err := os.Lstat(to); err == nil {
			return nil, camperrors.Wrapf(camperrors.ErrAlreadyExists,
				"cannot migrate %s: %s already exists; resolve it by hand, then re-run",
				relTo(absRoot, d.Path), relTo(absRoot, to))
		} else if !os.IsNotExist(err) {
			return nil, camperrors.Wrapf(err, "stat %s", to)
		}
		plan.Moves = append(plan.Moves, Move{From: d.Path, To: to})
	}

	// Reverse path order puts a nested dungeon ahead of the dungeon holding
	// it, because a path always sorts before the paths it prefixes. Renaming
	// the deepest first keeps every remaining From valid.
	sort.Slice(plan.Moves, func(i, j int) bool { return plan.Moves[i].From > plan.Moves[j].From })
	return plan, nil
}

// Apply performs the planned moves. In a git repository the renames are left
// staged for the caller to commit.
func Apply(ctx context.Context, plan *Plan) error {
	for _, m := range plan.Moves {
		if err := ctx.Err(); err != nil {
			return camperrors.Wrap(err, "context cancelled")
		}
		if err := move(ctx, plan, m); err != nil {
			return camperrors.Wrapf(err, "moving %s to %s",
				relTo(plan.CampaignRoot, m.From), relTo(plan.CampaignRoot, m.To))
		}
	}
	return nil
}

// CommitPaths returns the repo-relative paths the migration touched: the new
// hidden paths to stage, and the old visible paths whose staged removal the
// commit has to carry.
//
// Only the outermost moves are listed. A dungeon nested inside another
// migrated dungeon does not come to rest at its own Move.To: it is renamed
// first, and then its ancestor is renamed out from under it, so its final path
// is one no Move records. Staging the ancestor covers the whole subtree,
// including the nested rename.
func (p *Plan) CommitPaths() (added, removed []string) {
	for _, m := range p.Moves {
		if p.nestedUnderAnotherMove(m) {
			continue
		}
		added = append(added, relTo(p.CampaignRoot, m.To))
		removed = append(removed, relTo(p.CampaignRoot, m.From))
	}
	return added, removed
}

// nestedUnderAnotherMove reports whether m's source lives inside the source of
// some other planned move.
func (p *Plan) nestedUnderAnotherMove(m Move) bool {
	for _, other := range p.Moves {
		if other.From == m.From {
			continue
		}
		if strings.HasPrefix(m.From, other.From+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// move renames one dungeon.
//
// git mv is used where it applies, so the rename is staged as a rename and
// history follows the directory. It refuses a path with no tracked content
// ("source directory is empty" / "not under version control"), which is the
// normal state of a campaign whose scaffold has not been committed yet, so
// tracking decides the method up front rather than reading failure messages.
// An untracked dungeon moves with a plain rename and is picked up by the
// commit that follows.
func move(ctx context.Context, plan *Plan, m Move) error {
	if !plan.Git {
		return os.Rename(m.From, m.To)
	}

	from := relTo(plan.CampaignRoot, m.From)
	tracked, err := hasTrackedContent(ctx, plan.CampaignRoot, from)
	if err != nil {
		return err
	}
	if !tracked {
		return os.Rename(m.From, m.To)
	}
	_, err = git.RunGitCmd(ctx, plan.CampaignRoot, "mv", from, relTo(plan.CampaignRoot, m.To))
	return err
}

// hasCommits reports whether repoPath has a resolvable HEAD. camp init leaves
// a repository with no commits, and the commit path builds its index from
// HEAD, so there is nothing to commit against until the scaffold is committed.
func hasCommits(ctx context.Context, repoPath string) bool {
	_, err := git.Output(ctx, repoPath, "rev-parse", "--verify", "HEAD")
	return err == nil
}

// hasTrackedContent reports whether git tracks anything under rel.
func hasTrackedContent(ctx context.Context, repoPath, rel string) (bool, error) {
	out, err := git.Output(ctx, repoPath, "ls-files", "--", rel)
	if err != nil {
		return false, camperrors.Wrapf(err, "listing tracked files under %s", rel)
	}
	return strings.TrimSpace(out) != "", nil
}

func checkConflicts(absRoot string, found []spelling.Dungeon) error {
	spellings := make(map[string]map[string]bool, len(found))
	for _, d := range found {
		if spellings[d.Parent] == nil {
			spellings[d.Parent] = make(map[string]bool, 2)
		}
		spellings[d.Parent][d.Name] = true
	}

	var conflicts []string
	for parent, names := range spellings {
		if names[spelling.Visible] && names[spelling.Hidden] {
			conflicts = append(conflicts, relTo(absRoot, parent))
		}
	}
	if len(conflicts) == 0 {
		return nil
	}
	sort.Strings(conflicts)
	return camperrors.Wrapf(camperrors.ErrConflict,
		"cannot migrate: %d location(s) hold both %s/ and %s/, and only you can decide what to keep: %s. "+
			"Move the contents of one into the other and delete the empty directory, then re-run",
		len(conflicts), spelling.Visible, spelling.Hidden, strings.Join(conflicts, ", "))
}

func relTo(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return path
	}
	return rel
}
