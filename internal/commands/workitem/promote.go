package workitem

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	dungeoncmd "github.com/Obedience-Corp/camp/cmd/camp/dungeon"
	"github.com/Obedience-Corp/camp/internal/config"
	intdungeon "github.com/Obedience-Corp/camp/internal/dungeon"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/intent"
	"github.com/Obedience-Corp/camp/internal/ledger"
	"github.com/Obedience-Corp/camp/internal/pathutil"
	promotepkg "github.com/Obedience-Corp/camp/internal/promote"
	wkitem "github.com/Obedience-Corp/camp/internal/workitem"
	wkaudit "github.com/Obedience-Corp/camp/internal/workitem/audit"
	"github.com/Obedience-Corp/camp/internal/workitem/links"
	"github.com/Obedience-Corp/camp/internal/workitem/locate"
	"github.com/Obedience-Corp/camp/internal/workitem/priority"
	"github.com/Obedience-Corp/camp/pkg/ledgerkit"
)

type runWorkitemPromoteOptions struct {
	ID       string
	Target   string
	Dest     string
	Goal     string
	Keep     bool
	Force    bool
	DryRun   bool
	NoCommit bool
	JSON     bool
}

type workitemPromoteResult struct {
	ID            string   `json:"id"`
	Type          string   `json:"type"`
	Target        string   `json:"target"`
	From          string   `json:"from"`
	To            string   `json:"to"`
	PromotedTo    string   `json:"promoted_to"`
	SourceShelved string   `json:"source_shelved,omitempty"`
	Committed     bool     `json:"committed"`
	CommitMessage string   `json:"commit_message,omitempty"`
	Warnings      []string `json:"warnings,omitempty"`
	// ReleasedLinks are the links dropped because the workitem is no longer
	// active. Reported so an automatic removal is never silent.
	ReleasedLinks []releasedLink `json:"released_links,omitempty"`
	// ReleasedPriorityKey is the workitem key whose manual priority and
	// attention entries were dropped on shelve, empty when there were none.
	ReleasedPriorityKey string `json:"released_priority_key,omitempty"`
	// ClearedCurrent reports that the current-workitem pointer was cleared
	// because it referenced the shelved workitem.
	ClearedCurrent bool `json:"cleared_current,omitempty"`
}

// releasedLink records a link that promote removed, in enough detail to put it
// back by hand.
type releasedLink struct {
	ID        string `json:"id"`
	ScopeKind string `json:"scope_kind"`
	ScopePath string `json:"scope_path"`
	Role      string `json:"role"`
}

type commitInputs struct {
	description string
	sourcePaths []string
	destPaths   []string
	rewritten   []string
}

func newPromoteCommand() *cobra.Command {
	var (
		target   string
		dest     string
		goal     string
		keep     bool
		force    bool
		dryRun   bool
		noCommit bool
		jsonOut  bool
	)

	cmd := &cobra.Command{
		Use:   "promote [id] --target <target>",
		Short: "Promote a workitem: festival, doc, rail, dungeon",
		Long: `Promote the workitem identified by [id], by cwd, or by the current pointer.

TARGETS:
  festival    Create a festival from the workitem and shelve the source
  doc         Copy the workitem doc into docs/ and shelve the source
  ready       Move the workitem onto the festival rail at festivals/ready
  active      Move the workitem onto the festival rail at festivals/active
  completed   Move the workitem to its local dungeon/completed
  archived    Move the workitem to its local dungeon/archived
  someday     Move the workitem to its local dungeon/someday

The rail is forward-only: root -> ready -> active. A workitem already on a
stage cannot move backward, and moving one out of a dungeon is a restore
rather than a promote. To leave the rail entirely, use 'camp workitem demote',
which returns the workitem to its original workflow type root.

A workitem on the rail keeps its original type: a design item promoted to
active is still a design item, now living at festivals/active/<slug>, and
'camp wi --type design' still finds it.`,
		Args: cobra.RangeArgs(0, 1),
		Annotations: map[string]string{
			"agent_allowed": "true",
			"agent_reason":  "Fully specified by flags; only the bare camp promote selector is interactive",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			id := ""
			if len(args) == 1 {
				id = args[0]
			}
			_, err := runWorkitemPromote(cmd, runWorkitemPromoteOptions{
				ID: id, Target: target, Dest: dest, Goal: goal,
				Keep: keep, Force: force, DryRun: dryRun, NoCommit: noCommit, JSON: jsonOut,
			})
			return err
		},
	}

	f := cmd.Flags()
	f.StringVar(&target, "target", "", "Promotion target: festival, doc, ready, active, completed, archived, someday")
	f.StringVar(&dest, "dest", "", "Destination path under docs/ for the doc target (must stay within docs/)")
	f.StringVar(&goal, "goal", "", "Festival goal override (default: first paragraph of the workitem doc)")
	f.BoolVar(&keep, "keep", false, "On festival/doc, do not move the source workitem to the dungeon")
	f.BoolVar(&force, "force", false, "Skip readiness checks (e.g. empty doc)")
	f.BoolVar(&dryRun, "dry-run", false, "Print the planned action, change nothing")
	f.BoolVar(&noCommit, "no-commit", false, "Skip the auto-commit")
	f.BoolVar(&jsonOut, "json", false, "Output result as a single JSON object")
	return cmd
}

func runWorkitemPromote(cmd *cobra.Command, opts runWorkitemPromoteOptions) (*workitemPromoteResult, error) {
	ctx := cmd.Context()

	switch opts.Target {
	case "festival", "doc", "completed", "archived", "someday":
	case railStageReady, railStageActive:
		// Conditionally valid: the rail is forward-only, so these are checked
		// against the resolved source location below, once loc is known.
	case "":
		return nil, camperrors.New("required flag --target not set")
	default:
		return nil, camperrors.New("invalid target: " + opts.Target + " (use festival, doc, ready, active, completed, archived, someday)")
	}

	cfg, root, err := config.LoadCampaignConfigFromCwd(ctx)
	if err != nil {
		return nil, camperrors.Wrap(err, "not in a campaign directory")
	}

	loc, err := resolveWorkitem(ctx, root, opts.ID)
	if err != nil {
		return nil, err
	}

	if opts.Target == railStageReady || opts.Target == railStageActive {
		if err := checkRailTransition(railStageOf(loc, root), opts.Target); err != nil {
			return nil, err
		}
	}

	// Read .workitem metadata now, before any promote branch runs: festival
	// and doc targets relocate or remove loc.SourcePath while shelving the
	// source, so the marker will not be readable from here afterward. The
	// ledger correlates events by the real generated id/ref, not the slug;
	// a directory promoted without ever being adopted (no marker) still
	// promotes today, so a missing or unreadable marker falls back to the
	// slug rather than failing the command.
	ledgerID, ledgerRef, ledgerTitle := loc.Slug, "", ""
	if meta, metaErr := wkitem.LoadMetadata(ctx, loc.SourcePath); metaErr == nil && meta != nil {
		ledgerID, ledgerRef, ledgerTitle = meta.ID, meta.Ref, meta.Title
	}

	result := workitemPromoteResult{
		ID:     loc.Slug,
		Type:   loc.Type,
		Target: opts.Target,
		From:   filepath.ToSlash(dungeoncmd.RelFromRoot(root, loc.SourcePath)),
	}

	// The retirement gate. A parent that declared successors does not retire
	// until they exist — the successors-before-archive invariant the field
	// trial enforced as prose, made mechanical. It lives here, in the promote
	// path, beside the other readiness checks, because that is the one place
	// every terminal promotion passes through regardless of who called it.
	//
	// Checked BEFORE the dry-run branch: a dry-run that reports "would
	// promote" while the real command would refuse is worse than no dry-run,
	// because it is the output an operator trusts to avoid surprises.
	if isTerminalTarget(opts.Target) && !opts.Force {
		if err := checkSplitGate(ctx, cfg, root, loc); err != nil {
			return nil, err
		}
	}

	if opts.DryRun {
		if opts.JSON {
			return &result, emitPromoteJSON(cmd, result)
		}
		_, err := fmt.Fprintf(cmd.OutOrStdout(),
			"dry-run: would promote workitem %s (%s) to %s\n", loc.Slug, loc.Type, opts.Target)
		return nil, err
	}

	var ci *commitInputs
	switch opts.Target {
	case "festival":
		ci, err = doFestivalPromote(ctx, cmd, opts, root, loc, &result)
	case "doc":
		ci, err = doDocPromote(ctx, opts, root, loc, &result)
	case railStageReady, railStageActive:
		ci, err = doRailPromote(ctx, cfg, root, loc, opts.Target, &result)
	case "completed", "archived", "someday":
		ci, err = doDungeonPromote(ctx, root, loc, opts.Target, &result)
	default:
		return nil, camperrors.New("unhandled target: " + opts.Target)
	}
	if err != nil {
		return nil, err
	}
	if ci == nil {
		return &result, nil
	}

	return &result, finishWorkitemMove(ctx, cmd, cfg, root, ci, &result, moveTail{
		LedgerID:    ledgerID,
		LedgerRef:   ledgerRef,
		LedgerTitle: ledgerTitle,
		Why:         "promote to " + opts.Target,
		SuccessVerb: "Promoted",
		Options:     moveTailOptions{NoCommit: opts.NoCommit, JSON: opts.JSON},
	})
}

// PromoteOutcome is where a promotion put a workitem and whether it committed.
// Exported so `camp triage apply` can execute a promotion through this exact
// path instead of re-implementing one or re-entering camp as a subprocess.
type PromoteOutcome struct {
	// PromotedTo is the campaign-relative destination the workitem landed at.
	PromotedTo string
	// From is where it started, which is what an undo has to restore it to.
	From string
	// Committed reports whether the auto-commit ran.
	Committed bool
}

// PromoteWorkitem promotes a workitem to a rail or dungeon target through the
// same code path `camp workitem promote` runs, and reports where it landed.
//
// The output the command would print is discarded: a caller executing a plan
// renders its own receipts, and interleaving the promote command's chatter
// would make the apply transcript unreadable.
func PromoteWorkitem(ctx context.Context, out io.Writer, stableID, target string) (*PromoteOutcome, error) {
	cmd := &cobra.Command{}
	cmd.SetContext(ctx)
	cmd.SetOut(out)
	cmd.SetErr(out)

	result, err := runWorkitemPromote(cmd, runWorkitemPromoteOptions{
		ID: stableID, Target: target,
	})
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, camperrors.New("promote reported no result for " + stableID)
	}
	// PromotedTo is set by the festival and doc paths; the dungeon and rail
	// paths record their destination in To. Preferring one and falling back
	// to the other keeps the caller from having to know which promote it
	// asked for -- and getting this wrong silently cost the receipts their
	// undo command, which is the one thing an undo cannot be reconstructed
	// from later.
	destination := result.PromotedTo
	if destination == "" {
		destination = result.To
	}
	return &PromoteOutcome{
		PromotedTo: destination,
		From:       result.From,
		Committed:  result.Committed,
	}, nil
}

// isTerminalTarget reports whether a promote target retires a workitem.
func isTerminalTarget(target string) bool {
	switch target {
	case "completed", "archived", "someday":
		return true
	}
	return false
}

// checkSplitGate refuses a terminal promotion whose declared successors are
// not all discoverable.
//
// Existence, not content: whether a successor adequately captures the parent's
// scope is the split author's judgment, reviewed like any other work. Camp
// verifies the trail, not the prose.
func checkSplitGate(ctx context.Context, cfg *config.CampaignConfig, root string, loc *locate.Location) error {
	meta, err := wkitem.LoadMetadata(ctx, loc.SourcePath)
	if err != nil || meta == nil {
		// A workitem with no readable marker never declared successors, so
		// there is no gate to enforce. Promote's own checks own that case.
		return nil
	}
	declared := wkitem.SplitIntoOf(meta)
	if len(declared) == 0 {
		return nil
	}

	discovered, err := discoveredIDs(ctx, root, cfg)
	if err != nil {
		return err
	}
	if missing := wkitem.MissingSuccessors(declared, discovered); len(missing) > 0 {
		return wkitem.SplitGateError(meta.ID, missing)
	}
	return nil
}

// printReleasedLinks names every link promote dropped and how to restore it.
// Removing a row the user did not ask about is only acceptable because it is
// reported at the moment it happens.
func printReleasedLinks(w io.Writer, result workitemPromoteResult) error {
	for _, l := range result.ReleasedLinks {
		if _, err := fmt.Fprintf(w, "  released link %s (%s:%s); %s is no longer active\n",
			l.ID, l.ScopeKind, l.ScopePath, result.ID); err != nil {
			return err
		}
	}
	if len(result.ReleasedLinks) > 0 {
		if _, err := fmt.Fprintf(w, "  undo: git checkout -- .campaign/workitems/links.yaml\n"); err != nil {
			return err
		}
	}
	if result.ReleasedPriorityKey != "" {
		if _, err := fmt.Fprintf(w, "  released priority/attention for %s; %s is no longer active\n",
			result.ReleasedPriorityKey, result.ID); err != nil {
			return err
		}
	}
	if result.ClearedCurrent {
		if _, err := fmt.Fprintf(w, "  cleared the current workitem pointer; %s is no longer active\n",
			result.ID); err != nil {
			return err
		}
	}
	return nil
}

func emitPromoteJSON(cmd *cobra.Command, result workitemPromoteResult) error {
	enc := json.NewEncoder(cmd.OutOrStdout())
	enc.SetIndent("", "  ")
	if err := enc.Encode(result); err != nil {
		return camperrors.Wrap(err, "encoding JSON output")
	}
	return nil
}

func doDungeonPromote(ctx context.Context, campaignRoot string, loc *locate.Location, status string, result *workitemPromoteResult) (*commitInputs, error) {
	// Capture identity before the move: workitem_key encodes the source path,
	// which is about to change.
	oldID, oldKey := promotedWorkitemIdentity(ctx, campaignRoot, loc, result)

	moveRes, err := MoveToDungeon(ctx, campaignRoot, loc, status)
	if err != nil {
		return nil, err
	}
	result.To = moveRes.ToRel

	dest := append([]string{moveRes.TargetPath}, moveRes.CreatedFiles...)
	ci := &commitInputs{
		description: fmt.Sprintf("Promote workitem %s to %s", loc.Slug, status),
		sourcePaths: []string{loc.SourcePath},
		destPaths:   dest,
		rewritten:   moveRes.Svc.RewrittenLinkFiles(),
	}

	// A link attaches an active workitem to a working location -- usually a
	// worktree, which is temporary by construction. Shelving the workitem ends
	// that: nothing should still be checked out "for" completed work, and a
	// link left behind only resolves to a workitem the selector cannot see
	// (`camp p commit` silently stops stamping the ref). So the links go with
	// the workitem, reported rather than dropped quietly.
	if err := releaseLinksForShelvedSource(ctx, campaignRoot, oldID, oldKey, ci, result); err != nil {
		return nil, err
	}
	if err := releasePathStateForShelvedSource(ctx, campaignRoot, oldID, oldKey, result); err != nil {
		return nil, err
	}
	return ci, nil
}

// unlinkShelvedWorkitem removes every link pointing at a workitem that just
// moved into a dungeon and returns them so the caller can report what went.
func unlinkShelvedWorkitem(ctx context.Context, campaignRoot, oldID, oldKey string) ([]releasedLink, error) {
	if oldID == "" && oldKey == "" {
		return nil, nil
	}

	var dropped []releasedLink
	err := links.WithLock(ctx, campaignRoot, func(reg *links.Links) error {
		kept := reg.Links[:0]
		for _, l := range reg.Links {
			matches := (oldID != "" && l.WorkitemID == oldID) ||
				(oldKey != "" && l.WorkitemKey == oldKey)
			if matches {
				dropped = append(dropped, releasedLink{
					ID:        l.ID,
					ScopeKind: string(l.Scope.Kind),
					ScopePath: l.Scope.Path,
					Role:      string(l.Role),
				})
				continue
			}
			kept = append(kept, l)
		}
		if len(dropped) == 0 {
			return links.ErrSkipSave
		}
		reg.Links = kept
		return nil
	})
	if err != nil {
		return nil, err
	}
	return dropped, nil
}

func doFestivalPromote(ctx context.Context, cmd *cobra.Command, opts runWorkitemPromoteOptions, campaignRoot string, loc *locate.Location, result *workitemPromoteResult) (*commitInputs, error) {
	docContent, err := primaryDocContent(loc.SourcePath)
	if err != nil {
		return nil, err
	}
	if !opts.Force && strings.TrimSpace(docContent) == "" {
		return nil, camperrors.New("workitem doc is empty; use --force to promote anyway")
	}
	if opts.Dest != "" {
		return nil, camperrors.New("--dest is only valid for --target doc; fest chooses the festival directory")
	}

	// Capture the source workitem identity before the festival is created and
	// the source is shelved, so links pointing at it can be migrated onto the
	// festival afterward.
	oldID, oldKey := promotedWorkitemIdentity(ctx, campaignRoot, loc, result)

	name := intent.SlugFromTitle(titleFromDoc(docContent, loc.Slug))
	goal := opts.Goal
	if goal == "" {
		goal = promotepkg.ExtractFirstParagraph(docContent)
	}

	fr, err := promotepkg.FindAndCreateFestival(ctx, campaignRoot, name, goal)
	if err != nil {
		return nil, camperrors.Wrap(err, "creating festival")
	}
	if fr.NotFound {
		_, perr := fmt.Fprintf(cmd.ErrOrStderr(),
			"Note: fest CLI not found. Workitem left active. Create the festival manually with:\n"+
				"  fest create festival --type standard --name %q\n", name)
		return nil, perr
	}
	if !fr.Created {
		return nil, camperrors.New("festival creation failed: " + fr.CLIError)
	}

	isFile, slug := promoteSourceShape(loc)
	ingestDir := filepath.Join(campaignRoot, "festivals", fr.Dest, fr.Dir, "001_INGEST", "input_specs", slug)
	copyDest := ingestDir
	if isFile {
		copyDest = filepath.Join(ingestDir, filepath.Base(loc.SourcePath))
	}
	if err := promotepkg.CopyTree(loc.SourcePath, copyDest); err != nil {
		return nil, camperrors.Wrap(err, "copying workitem into festival ingest")
	}
	promotedTo := filepath.ToSlash(filepath.Join("festivals", fr.Dest, fr.Dir))

	if err := recordPromotedTo(ctx, campaignRoot, loc, promotedTo); err != nil {
		return nil, err
	}

	result.To = promotedTo
	result.PromotedTo = promotedTo

	ci := &commitInputs{
		description: fmt.Sprintf("Promote workitem %s to festival %s", loc.Slug, promotedTo),
		destPaths: []string{
			filepath.Join(campaignRoot, "festivals", fr.Dest, fr.Dir),
			filepath.Join(campaignRoot, "festivals", ".festival", ".state"),
		},
	}
	ci, err = appendShelve(ctx, opts, campaignRoot, loc, ci, result)
	if err != nil {
		return nil, err
	}

	// Only a shelved source orphans its links; --keep leaves the source in
	// place and resolvable, so there is nothing to migrate.
	if !opts.Keep {
		migrated, mErr := migratePromotedLinks(ctx, campaignRoot, oldID, oldKey, promotedTo)
		if mErr != nil {
			return nil, camperrors.Wrap(mErr, "migrate workitem links to festival")
		}
		if migrated {
			ci.destPaths = append(ci.destPaths, links.LinksPath(campaignRoot))
		}
	}
	return ci, nil
}

func doDocPromote(ctx context.Context, opts runWorkitemPromoteOptions, campaignRoot string, loc *locate.Location, result *workitemPromoteResult) (*commitInputs, error) {
	docContent, err := primaryDocContent(loc.SourcePath)
	if err != nil {
		return nil, err
	}
	if !opts.Force && strings.TrimSpace(docContent) == "" {
		return nil, camperrors.New("workitem doc is empty; use --force to promote anyway")
	}

	// Capture identity before the source is shelved: the marker is read from
	// loc.SourcePath, which appendShelve moves.
	oldID, oldKey := promotedWorkitemIdentity(ctx, campaignRoot, loc, result)

	isFile, cleanSlug := promoteSourceShape(loc)
	relDest := opts.Dest
	if relDest == "" {
		relDest = cleanSlug
	}
	docsRoot := filepath.Join(campaignRoot, "docs")
	if err := os.MkdirAll(docsRoot, 0o755); err != nil {
		return nil, camperrors.Wrap(err, "creating docs directory")
	}
	destDir := filepath.Join(docsRoot, relDest)
	if err := pathutil.ValidateBoundary(docsRoot, destDir); err != nil {
		return nil, camperrors.Wrapf(err, "doc destination %q must stay within docs/", relDest)
	}

	if !opts.Force {
		if entries, _ := os.ReadDir(destDir); len(entries) > 0 {
			return nil, camperrors.New("docs/" + relDest + " already exists and is not empty; use --force to overwrite")
		}
	}
	copyDest := destDir
	if isFile {
		copyDest = filepath.Join(destDir, filepath.Base(loc.SourcePath))
	}
	if err := promotepkg.CopyTree(loc.SourcePath, copyDest); err != nil {
		return nil, camperrors.Wrap(err, "copying workitem into docs")
	}
	promotedTo := filepath.ToSlash(filepath.Join("docs", relDest))

	if err := recordPromotedTo(ctx, campaignRoot, loc, promotedTo); err != nil {
		return nil, err
	}

	result.To = promotedTo
	result.PromotedTo = promotedTo

	ci := &commitInputs{
		description: fmt.Sprintf("Promote workitem %s to %s", loc.Slug, promotedTo),
		destPaths:   []string{destDir},
	}
	ci, err = appendShelve(ctx, opts, campaignRoot, loc, ci, result)
	if err != nil {
		return nil, err
	}

	// Shelving the source ends the workitem's active life exactly as a dungeon
	// target does, so its links go the same way. --keep leaves the source in
	// place and resolvable, so there is nothing to release.
	//
	// This cannot live inside appendShelve: the festival path shelves too, but
	// its rows have somewhere to go and migratePromotedLinks re-points them
	// afterward. A doc has no workitem identity to carry them.
	if !opts.Keep {
		if err := releaseLinksForShelvedSource(ctx, campaignRoot, oldID, oldKey, ci, result); err != nil {
			return nil, err
		}
		if err := releasePathStateForShelvedSource(ctx, campaignRoot, oldID, oldKey, result); err != nil {
			return nil, err
		}
	}
	return ci, nil
}

// releaseLinksForShelvedSource drops the links a workitem held once its source
// has been shelved, recording them on result and adding links.yaml to the
// commit when anything changed.
func releaseLinksForShelvedSource(ctx context.Context, campaignRoot, oldID, oldKey string,
	ci *commitInputs, result *workitemPromoteResult,
) error {
	dropped, err := unlinkShelvedWorkitem(ctx, campaignRoot, oldID, oldKey)
	if err != nil {
		return camperrors.Wrap(err, "release workitem links on shelve")
	}
	if len(dropped) > 0 {
		ci.destPaths = append(ci.destPaths, links.LinksPath(campaignRoot))
		result.ReleasedLinks = append(result.ReleasedLinks, dropped...)
	}
	return nil
}

// releasePathStateForShelvedSource drops the per-machine state keyed on the
// source's pre-move path. Both stores are gitignored, so neither is staged.
//
// A manual priority and an attention stage describe how to rank active work. The
// key encodes the path, so a shelve strands the entry: nothing resolves it, and
// Prune only reaches it on a later full discovery pass. Dropping it here is the
// same call the link release makes for the same reason, so the two cannot
// disagree about whether a shelved workitem is still active.
func releasePathStateForShelvedSource(ctx context.Context, campaignRoot, oldID, oldKey string,
	result *workitemPromoteResult,
) error {
	if oldKey != "" {
		storePath := priority.StorePath(campaignRoot)
		store, err := priority.Load(storePath)
		if err != nil {
			return camperrors.Wrap(err, "load priority store on shelve")
		}
		_, hasPriority := store.ManualPriorities[oldKey]
		_, hasAttention := store.Attention[oldKey]
		if hasPriority || hasAttention {
			if err := priority.WithLock(ctx, storePath, func(s *priority.Store) error {
				priority.Clear(s, oldKey)
				delete(s.Attention, oldKey)
				return nil
			}); err != nil {
				return camperrors.Wrap(err, "release priority entries on shelve")
			}
			result.ReleasedPriorityKey = oldKey
		}
	}

	cur, err := links.LoadCurrent(ctx, campaignRoot)
	if err != nil {
		return camperrors.Wrap(err, "load current workitem on shelve")
	}
	if cur == nil {
		return nil
	}
	if cur.WorkitemID != oldID && cur.WorkitemID != oldKey {
		return nil
	}
	// A current pointer at a shelved workitem is worse than none: the selector
	// cannot see dungeon items, so camp p commit silently stops stamping the ref.
	if err := links.SaveCurrent(ctx, campaignRoot, nil); err != nil {
		return camperrors.Wrap(err, "clear current workitem on shelve")
	}
	result.ClearedCurrent = true
	return nil
}

// promotedWorkitemIdentity returns the stable id and key that links may
// reference for the workitem being promoted, read from its .workitem marker
// before it is shelved. Returns ("", key) for an unadopted directory (no
// marker), which has no stable-id links to migrate.
func promotedWorkitemIdentity(ctx context.Context, campaignRoot string, loc *locate.Location, result *workitemPromoteResult) (id, key string) {
	key = loc.Type + ":" + result.From
	// A file source has no .workitem sibling; its identity is in its own
	// frontmatter, so load it there (mirrors gather's ItemKind branch). Without
	// this a file source yields an empty id and link migration can only match by
	// key, never by stable ID.
	if isFile, _ := promoteSourceShape(loc); isFile {
		if meta, err := wkitem.LoadFrontmatterMetadata(loc.SourcePath); err == nil && meta != nil {
			id = meta.ID
		}
		return id, key
	}
	if meta, err := wkitem.LoadMetadata(ctx, loc.SourcePath); err == nil && meta != nil {
		id = meta.ID
	}
	return id, key
}

// migratePromotedLinks re-points every link that referenced the promoted
// workitem (by its old stable id or key) onto the created festival, addressed
// by the festival's single-segment fest.yaml id. Returns whether the registry
// changed. Best-effort: if the festival has no readable id there is no valid
// single-segment link target, so the links are left untouched.
func migratePromotedLinks(ctx context.Context, campaignRoot, oldID, oldKey, promotedTo string) (bool, error) {
	newID := readFestivalID(campaignRoot, promotedTo)
	if newID == "" || (oldID == "" && oldKey == "") {
		return false, nil
	}
	newKey := "festival:" + promotedTo

	changed := false
	err := links.WithLock(ctx, campaignRoot, func(reg *links.Links) error {
		if !rehomePromotedLinks(reg, oldID, oldKey, newID, newKey) {
			return links.ErrSkipSave
		}
		changed = true
		return nil
	})
	if err != nil {
		return false, err
	}
	return changed, nil
}

// rehomePromotedLinks re-points links matching the promoted workitem onto the
// festival (updating both workitem_id and workitem_key), then drops links that
// became exact duplicates. Mirrors rehomeGatherLinks, extended to carry the key
// because a festival's id and key differ from the source workitem's.
func rehomePromotedLinks(reg *links.Links, oldID, oldKey, newID, newKey string) bool {
	changed := false
	for i := range reg.Links {
		l := &reg.Links[i]
		matches := (oldID != "" && l.WorkitemID == oldID) || (oldKey != "" && l.WorkitemKey == oldKey)
		if matches && (l.WorkitemID != newID || l.WorkitemKey != newKey) {
			l.WorkitemID = newID
			l.WorkitemKey = newKey
			changed = true
		}
	}
	if !changed {
		return false
	}
	seen := make(map[string]bool, len(reg.Links))
	deduped := make([]links.Link, 0, len(reg.Links))
	for _, link := range reg.Links {
		key := link.WorkitemID + "|" + string(link.Scope.Kind) + "|" + link.Scope.Path + "|" + string(link.Role)
		if seen[key] {
			continue
		}
		seen[key] = true
		deduped = append(deduped, link)
	}
	reg.Links = deduped
	return true
}

// readFestivalID returns the fest.yaml metadata id (e.g. SC0001) for the
// festival at promotedTo (a campaign-relative festivals/... path), or "" when
// it cannot be read. This is the same id discovery exposes as the festival
// workitem's SourceID, which the selector resolves and links address.
func readFestivalID(campaignRoot, promotedTo string) string {
	data, err := os.ReadFile(filepath.Join(campaignRoot, filepath.FromSlash(promotedTo), "fest.yaml"))
	if err != nil {
		return ""
	}
	var doc struct {
		Metadata struct {
			ID string `yaml:"id"`
		} `yaml:"metadata"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return ""
	}
	return strings.TrimSpace(doc.Metadata.ID)
}

func recordPromotedTo(ctx context.Context, campaignRoot string, loc *locate.Location, promotedTo string) error {
	info, statErr := os.Stat(loc.SourcePath)
	if statErr != nil {
		return nil
	}
	// A directory source needs a .workitem marker to stamp; a file source is
	// itself the frontmatter target, so it always proceeds. wkitem.RecordPromotion
	// picks the right surface by shape.
	if info.IsDir() {
		if _, err := os.Stat(filepath.Join(loc.SourcePath, wkitem.MetadataFilename)); os.IsNotExist(err) {
			return nil
		}
	}
	relPath := filepath.ToSlash(dungeoncmd.RelFromRoot(campaignRoot, loc.SourcePath))
	if err := promotepkg.RecordPromotion(promotedTo, func(rec promotepkg.PromotionRecord) error {
		return wkitem.RecordPromotion(ctx, campaignRoot, relPath, rec.PromotedTo, rec.PromotedAt)
	}); err != nil {
		return camperrors.Wrap(err, "recording promotion on workitem")
	}
	return nil
}

func appendShelve(ctx context.Context, opts runWorkitemPromoteOptions, campaignRoot string, loc *locate.Location, ci *commitInputs, result *workitemPromoteResult) (*commitInputs, error) {
	if opts.Keep {
		ci.destPaths = append(ci.destPaths, loc.SourcePath)
		return ci, nil
	}

	moveRes, err := MoveToDungeon(ctx, campaignRoot, loc, "completed")
	if err != nil {
		return nil, camperrors.Wrap(err, "shelving source workitem")
	}
	result.SourceShelved = moveRes.ToRel
	ci.sourcePaths = []string{loc.SourcePath}
	ci.destPaths = append(ci.destPaths, moveRes.TargetPath)
	ci.destPaths = append(ci.destPaths, moveRes.CreatedFiles...)
	ci.rewritten = moveRes.Svc.RewrittenLinkFiles()
	return ci, nil
}

// DungeonMove is the outcome of moving a workitem directory into a dungeon
// status, carrying everything the auto-commit step needs.
type DungeonMove struct {
	Svc          *intdungeon.Service
	CreatedFiles []string
	TargetPath   string
	FromRel      string
	ToRel        string
}

// MoveToDungeon moves the workitem at loc into the given dungeon status using
// the shared dungeon plumbing. It is the single implementation behind both
// camp workitem promote and the deprecated camp shelve alias.
func MoveToDungeon(ctx context.Context, campaignRoot string, loc *locate.Location, status string) (DungeonMove, error) {
	// Directory and file workitems both move via the generic dungeon plumbing
	// (statusmove + link rewriting); the shape does not matter here.
	if _, err := os.Stat(loc.SourcePath); err != nil {
		return DungeonMove{}, camperrors.Wrapf(err, "stat workitem %s", loc.SourcePath)
	}

	if loc.InDungeon && loc.Status == status {
		return DungeonMove{}, camperrors.New(fmt.Sprintf("workitem %q is already at status %q", loc.Slug, status))
	}

	svc := intdungeon.NewService(campaignRoot, loc.DungeonPath)
	initResult, err := svc.Init(ctx, intdungeon.InitOptions{})
	if err != nil {
		return DungeonMove{}, camperrors.Wrap(err, "initializing workitem dungeon")
	}

	targetPath, err := svc.MoveToDungeonStatus(ctx, loc.Slug, loc.ParentPath, status)
	if err != nil {
		return DungeonMove{}, dungeoncmd.WrapDungeonMoveError(err, loc.Slug, status)
	}

	return DungeonMove{
		Svc:          svc,
		CreatedFiles: initResult.CreatedFiles,
		TargetPath:   targetPath,
		FromRel:      filepath.ToSlash(dungeoncmd.RelFromRoot(campaignRoot, loc.SourcePath)),
		ToRel:        filepath.ToSlash(dungeoncmd.RelFromRoot(campaignRoot, targetPath)),
	}, nil
}

// promoteSourceShape reports whether loc's source is a single file and returns a
// container slug for it: the extension-stripped slug for a file (so a file lands
// under <dest>/<slug>/<file>.md rather than <dest>/<file>.md/), or loc.Slug for a
// directory.
func promoteSourceShape(loc *locate.Location) (isFile bool, slug string) {
	if info, err := os.Stat(loc.SourcePath); err == nil && !info.IsDir() {
		return true, strings.TrimSuffix(loc.Slug, filepath.Ext(loc.Slug))
	}
	return false, loc.Slug
}

// primaryDocContent returns the promotable markdown for a source path. For a
// file workitem the source is itself the doc; for a directory it is README.md or
// the first top-level .md.
func primaryDocContent(srcDir string) (string, error) {
	if info, err := os.Stat(srcDir); err == nil && !info.IsDir() {
		data, rerr := os.ReadFile(srcDir)
		if rerr != nil {
			return "", camperrors.Wrap(rerr, "reading workitem file")
		}
		return string(data), nil
	}

	if data, err := os.ReadFile(filepath.Join(srcDir, "README.md")); err == nil {
		return string(data), nil
	} else if !os.IsNotExist(err) {
		return "", camperrors.Wrap(err, "reading workitem README")
	}

	entries, err := os.ReadDir(srcDir)
	if err != nil {
		return "", camperrors.Wrap(err, "reading workitem directory")
	}
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
			data, readErr := os.ReadFile(filepath.Join(srcDir, e.Name()))
			if readErr != nil {
				return "", camperrors.Wrap(readErr, "reading workitem doc")
			}
			return string(data), nil
		}
	}
	return "", nil
}

func titleFromDoc(content, fallbackSlug string) string {
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "# ") {
			return strings.TrimSpace(strings.TrimPrefix(trimmed, "# "))
		}
	}
	return fallbackSlug
}

func resolveWorkitem(ctx context.Context, campaignRoot, id string) (*locate.Location, error) {
	if id != "" {
		return locateByID(ctx, campaignRoot, id)
	}

	cwd, err := os.Getwd()
	if err != nil {
		return nil, camperrors.Wrap(err, "getting current directory")
	}
	if loc, err := locate.DetectFromCwd(campaignRoot, cwd); err == nil {
		return loc, nil
	}

	if loc, err := locateFromCurrent(ctx, campaignRoot); err == nil && loc != nil {
		return loc, nil
	}

	return nil, camperrors.New("no workitem in context (pass an id, cd into a workitem, or set current)")
}

func locateByID(ctx context.Context, root, id string) (*locate.Location, error) {
	wi, err := resolveSelector(ctx, root, id, false)
	if err != nil {
		return nil, err
	}
	if wi.RelativePath == "" {
		return nil, camperrors.New("resolved workitem has no path on disk")
	}
	return locate.DetectFromCwd(root, filepath.Join(root, wi.RelativePath))
}

func locateFromCurrent(ctx context.Context, root string) (*locate.Location, error) {
	cur, err := links.LoadCurrent(ctx, root)
	if err != nil {
		return nil, err
	}
	if cur == nil || cur.WorkitemID == "" {
		return nil, camperrors.New("no current workitem set")
	}
	return locateByID(ctx, root, cur.WorkitemID)
}

func auditFilePath(root string) string {
	return filepath.Join(root, ".campaign", "workitems", wkaudit.AuditFile)
}

func emitTransitionLedger(ctx context.Context, cmd *cobra.Command, root string, result *workitemPromoteResult, why string) {
	ledger.NewFromRoot(ctx, root, ledger.WarnTo(cmd.ErrOrStderr())).
		Emit(ctx, ledgerkit.KindTransitioned, ledgerkit.Scope{Workitem: result.ID},
			ledger.WithWhy(why),
			ledger.WithPayload(map[string]any{
				"target": result.Target, "from": result.From,
				"to": result.To, "promoted_to": result.PromotedTo,
			}))
}
