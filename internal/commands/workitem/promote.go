package workitem

import (
	"context"
	"encoding/json"
	"fmt"
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
	navindex "github.com/Obedience-Corp/camp/internal/nav/index"
	"github.com/Obedience-Corp/camp/internal/pathutil"
	promotepkg "github.com/Obedience-Corp/camp/internal/promote"
	"github.com/Obedience-Corp/camp/internal/ui"
	wkitem "github.com/Obedience-Corp/camp/internal/workitem"
	wkaudit "github.com/Obedience-Corp/camp/internal/workitem/audit"
	"github.com/Obedience-Corp/camp/internal/workitem/links"
	"github.com/Obedience-Corp/camp/internal/workitem/locate"
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
		Short: "Promote a workitem to a festival, doc, or dungeon",
		Long: `Promote the workitem identified by [id], by cwd, or by the current pointer.

TARGETS:
  festival    Create a festival from the workitem and shelve the source
  doc         Copy the workitem doc into docs/ and shelve the source
  completed   Move the workitem to its local dungeon/completed
  archived    Move the workitem to its local dungeon/archived
  someday     Move the workitem to its local dungeon/someday`,
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
			return runWorkitemPromote(cmd, runWorkitemPromoteOptions{
				ID: id, Target: target, Dest: dest, Goal: goal,
				Keep: keep, Force: force, DryRun: dryRun, NoCommit: noCommit, JSON: jsonOut,
			})
		},
	}

	f := cmd.Flags()
	f.StringVar(&target, "target", "", "Promotion target: festival, doc, completed, archived, someday")
	f.StringVar(&dest, "dest", "", "Destination path under docs/ for the doc target (must stay within docs/)")
	f.StringVar(&goal, "goal", "", "Festival goal override (default: first paragraph of the workitem doc)")
	f.BoolVar(&keep, "keep", false, "On festival/doc, do not move the source workitem to the dungeon")
	f.BoolVar(&force, "force", false, "Skip readiness checks (e.g. empty doc)")
	f.BoolVar(&dryRun, "dry-run", false, "Print the planned action, change nothing")
	f.BoolVar(&noCommit, "no-commit", false, "Skip the auto-commit")
	f.BoolVar(&jsonOut, "json", false, "Output result as a single JSON object")
	return cmd
}

func runWorkitemPromote(cmd *cobra.Command, opts runWorkitemPromoteOptions) error {
	ctx := cmd.Context()

	switch opts.Target {
	case "festival", "doc", "completed", "archived", "someday":
	case "active":
		return camperrors.New("cannot promote to active: a workitem outside the dungeon is already active; restoring workitems out of the dungeon is not a promote")
	case "":
		return camperrors.New("required flag --target not set")
	default:
		return camperrors.New("invalid target: " + opts.Target + " (use festival, doc, completed, archived, someday)")
	}

	cfg, root, err := config.LoadCampaignConfigFromCwd(ctx)
	if err != nil {
		return camperrors.Wrap(err, "not in a campaign directory")
	}

	loc, err := resolveWorkitem(ctx, root, opts.ID)
	if err != nil {
		return err
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

	if opts.DryRun {
		if opts.JSON {
			return emitPromoteJSON(cmd, result)
		}
		_, err := fmt.Fprintf(cmd.OutOrStdout(),
			"dry-run: would promote workitem %s (%s) to %s\n", loc.Slug, loc.Type, opts.Target)
		return err
	}

	var ci *commitInputs
	switch opts.Target {
	case "festival":
		ci, err = doFestivalPromote(ctx, cmd, opts, root, loc, &result)
	case "doc":
		ci, err = doDocPromote(ctx, opts, root, loc, &result)
	case "completed", "archived", "someday":
		ci, err = doDungeonPromote(ctx, root, loc, opts.Target, &result)
	default:
		return camperrors.New("unhandled target: " + opts.Target)
	}
	if err != nil {
		return err
	}
	if ci == nil {
		return nil
	}

	appendWorkitemAuditEvent(ctx, cmd, root, wkaudit.Event{
		Event:      wkaudit.EventPromote,
		ID:         ledgerID,
		Ref:        ledgerRef,
		Title:      ledgerTitle,
		Type:       result.Type,
		From:       result.From,
		To:         result.To,
		Target:     result.Target,
		PromotedTo: result.PromotedTo,
	})
	ci.destPaths = append(ci.destPaths, filepath.Join(root, ".campaign", "workitems", wkaudit.AuditFile))

	ledger.NewFromRoot(ctx, root, ledger.WarnTo(cmd.ErrOrStderr())).
		Emit(ctx, ledgerkit.KindTransitioned, ledgerkit.Scope{Workitem: result.ID},
			ledger.WithWhy("promote to "+opts.Target),
			ledger.WithPayload(map[string]any{
				"target": result.Target, "from": result.From,
				"to": result.To, "promoted_to": result.PromotedTo,
			}))

	if !opts.NoCommit {
		outcome := dungeoncmd.StageAndCommitDungeonMove(ctx, &dungeoncmd.DungeonMoveCommit{
			Config:           cfg,
			CampaignRoot:     root,
			Description:      ci.description,
			SourcePaths:      ci.sourcePaths,
			DestinationPaths: ci.destPaths,
			RewrittenFiles:   ci.rewritten,
		})
		if !opts.JSON {
			dungeoncmd.PrintDungeonMoveOutcome(cmd.OutOrStdout(), outcome)
		}
		result.Committed = outcome.Committed
		result.CommitMessage = outcome.Message
		if cerr := outcome.Err(); cerr != nil {
			return cerr
		}
	}

	if navErr := navindex.Delete(root); navErr != nil {
		msg := fmt.Sprintf("failed to invalidate navigation cache: %v", navErr)
		result.Warnings = append(result.Warnings, msg)
		if !opts.JSON {
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "%s %s\n", ui.WarningIcon(), msg)
		}
	}

	if opts.JSON {
		return emitPromoteJSON(cmd, result)
	}
	_, err = fmt.Fprintf(cmd.OutOrStdout(), "%s Promoted workitem %s to %s\n", ui.SuccessIcon(), result.ID, result.To)
	return err
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
	moveRes, err := MoveToDungeon(ctx, campaignRoot, loc, status)
	if err != nil {
		return nil, err
	}
	result.To = moveRes.ToRel

	dest := append([]string{moveRes.TargetPath}, moveRes.CreatedFiles...)
	return &commitInputs{
		description: fmt.Sprintf("Promote workitem %s to %s", loc.Slug, status),
		sourcePaths: []string{loc.SourcePath},
		destPaths:   dest,
		rewritten:   moveRes.Svc.RewrittenLinkFiles(),
	}, nil
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
	return appendShelve(ctx, opts, campaignRoot, loc, ci, result)
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
