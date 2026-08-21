package workitem

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/jsoncontract"
	"github.com/Obedience-Corp/camp/internal/paths"
	"github.com/Obedience-Corp/camp/internal/quest"
	wkitem "github.com/Obedience-Corp/camp/internal/workitem"
	"github.com/Obedience-Corp/camp/internal/workitem/links"
	"github.com/Obedience-Corp/camp/internal/workitem/priority"
)

// Doctor finding codes (dotted-domain form). Stable strings; consumers
// dispatch on them.
const (
	codeBrokenLink               = "workitem.link.broken"
	codeBrokenScope              = "workitem.scope.broken"
	codeOutOfBounds              = "workitem.scope.out-of-bounds"
	codeScopeUnvalidatable       = "workitem.scope.unvalidatable"
	codeScopeNotLocal            = "workitem.scope.not-on-this-machine"
	codeWorkitemShelved          = "workitem.link.shelved"
	codeDuplicatePrimary         = "workitem.link.duplicate-primary"
	codeSchemaViolation          = "workitem.schema.violation"
	codeMissingRefField          = "workitem.ref.missing"
	codeWorkitemScanFailed       = "workitem.scan.failed"
	codeRegistryParseError       = "workitem.registry.parse-error"
	codeProjectNotFound          = "workitem.project.not-found"
	codeProjectUnvalidatable     = "workitem.project.unvalidatable"
	codeDeprecatedRelatedProject = "workitem.link.related-project-deprecated"
	codeUnstampedResident        = "workitem.resident.unstamped"
	codeResidentMissingHome      = "workitem.resident.missing-home"
)

const (
	docSeverityError   = "error"
	docSeverityWarning = "warning"
	docSeverityInfo    = "info"
)

type docFinding struct {
	Code        string `json:"code"`
	Severity    string `json:"severity"`
	Target      string `json:"target,omitempty"`
	Message     string `json:"message"`
	FixHint     string `json:"fix_hint,omitempty"`
	AutoFixable bool   `json:"auto_fixable"`

	// migrateToID/migrateToKey carry a non-destructive recovery target for a
	// broken link whose workitem was promoted to a festival: --fix re-points
	// the link onto that festival instead of removing it. Unexported so they
	// stay out of the --json contract.
	migrateToID  string
	migrateToKey string
	// renameFrom/renameTo rewrite a stale projects: entry when git recorded
	// the project directory rename. Unexported; --json still uses the public
	// auto_fixable / fix_hint fields.
	renameFrom string
	renameTo   string
}

// errDoctorIssues triggers a non-zero exit from cobra after we have already
// emitted findings.
var errDoctorIssues = camperrors.NewCommand(
	"camp workitem doctor",
	2,
	"doctor reported error-severity findings",
	nil,
)

func newDoctorCommand() *cobra.Command {
	var jsonOut, fix bool
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Report link-registry health issues",
		Long: `Report health issues in the campaign workitem link registry.

The command reads .campaign/workitems/links.yaml, scans .workitem metadata on
disk, and checks current-workitem and priority stores for stale or inconsistent
references. Use --fix to apply auto-repairs for supported findings, including
rewriting projects: entries whose path git recorded as a project rename. Use
--json for machine-readable findings and stable finding codes.`,
		Args: jsoncontract.Args(WorkitemDoctorJSONVersion, func() bool { return jsonOut }, cobra.NoArgs),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDoctor(cmd.Context(), cmd, jsonOut, fix)
		},
		SilenceErrors: true,
	}
	cmd.SetFlagErrorFunc(jsoncontract.FlagErrorFunc(WorkitemDoctorJSONVersion, func() bool { return jsonOut }))
	cmd.Flags().BoolVar(&jsonOut, "json", false, "emit a structured JSON result")
	cmd.Flags().BoolVar(&fix, "fix", false, "auto-repair findings tagged auto_fixable")
	return cmd
}

func runDoctor(ctx context.Context, cmd *cobra.Command, jsonOut, fix bool) error {
	_, root, err := config.LoadCampaignConfigFromCwd(ctx)
	if err != nil {
		return renderWorkitemDoctorError(cmd, jsonOut, camperrors.Wrap(err, "not in a campaign directory"))
	}
	knownIDs, items, err := workitemIDsOnDisk(ctx, root)
	if err != nil {
		return renderWorkitemDoctorError(cmd, jsonOut, err)
	}

	var findings []docFinding
	if fix {
		if _, loadErr := links.Load(ctx, root); loadErr != nil {
			quarantined, qerr := links.QuarantineBroken(ctx, root)
			if qerr != nil {
				return renderWorkitemDoctorError(cmd, jsonOut, camperrors.Wrap(qerr, "quarantine broken registry"))
			}
			if quarantined != "" {
				if _, err := fmt.Fprintf(cmd.ErrOrStderr(),
					"quarantined broken links.yaml to %s; bootstrapped empty registry\n",
					quarantined); err != nil {
					return err
				}
			}
		}
		err = links.WithLock(ctx, root, func(registry *links.Links) error {
			findings = collectWorkitemFindings(ctx, root, registry, knownIDs, items)
			applied, fixErr := autoFixWorkitemFindings(ctx, root, registry, findings, items, cmd.ErrOrStderr())
			if fixErr != nil {
				return fixErr
			}
			if applied == 0 {
				return links.ErrSkipSave
			}
			knownIDs, items, _ = workitemIDsOnDisk(ctx, root)
			findings = collectWorkitemFindings(ctx, root, registry, knownIDs, items)
			return nil
		})
		if err != nil {
			return renderWorkitemDoctorError(cmd, jsonOut, err)
		}
		knownIDs, _, err = workitemIDsOnDisk(ctx, root)
		if err != nil {
			return renderWorkitemDoctorError(cmd, jsonOut, err)
		}
		if err := prunePriorityStoreIfPresent(ctx, root, knownIDs); err != nil {
			if _, writeErr := fmt.Fprintf(cmd.ErrOrStderr(), "warning: priority prune during fix: %v\n", err); writeErr != nil {
				return writeErr
			}
		}
	} else {
		registry, loadErr := links.Load(ctx, root)
		if loadErr != nil {
			parseFinding := docFinding{
				Code:        codeRegistryParseError,
				Severity:    docSeverityError,
				Target:      "registry:links.yaml",
				Message:     "links.yaml cannot be parsed: " + loadErr.Error(),
				FixHint:     "run `camp workitem doctor --fix` to quarantine the broken file and bootstrap an empty registry",
				AutoFixable: true,
			}
			if jsonOut {
				if jerr := emitDocJSON(cmd.OutOrStdout(), []docFinding{parseFinding}); jerr != nil {
					return jerr
				}
				return errDoctorIssues
			}
			if err := emitDocHuman(cmd.OutOrStdout(), []docFinding{parseFinding}); err != nil {
				return err
			}
			return camperrors.Wrap(loadErr, "load links registry")
		}
		findings = collectWorkitemFindings(ctx, root, registry, knownIDs, items)
	}

	if jsonOut {
		if err := emitDocJSON(cmd.OutOrStdout(), findings); err != nil {
			return err
		}
	} else {
		if err := emitDocHuman(cmd.OutOrStdout(), findings); err != nil {
			return err
		}
	}

	if hasErrorFinding(findings) {
		return errDoctorIssues
	}
	return nil
}

func prunePriorityStoreIfPresent(ctx context.Context, root string, knownIDs map[string]struct{}) error {
	storePath := priority.StorePath(root)
	if _, err := os.Stat(storePath); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return camperrors.Wrap(err, "stat priority store")
	}
	validKeys := make(map[string]bool, len(knownIDs))
	for id := range knownIDs {
		validKeys[id] = true
	}
	return priority.WithLock(ctx, storePath, func(store *priority.Store) error {
		priority.Prune(store, validKeys)
		return nil
	})
}

func renderWorkitemDoctorError(cmd *cobra.Command, jsonOut bool, err error) error {
	if err == nil || !jsonOut {
		return err
	}
	return jsoncontract.RenderError(cmd, WorkitemDoctorJSONVersion, err)
}

func collectWorkitemFindings(ctx context.Context, root string, registry *links.Links, knownIDs map[string]struct{}, items []wkitem.WorkItem) []docFinding {
	var findings []docFinding
	findings = append(findings, collectResidentFindings(root, items)...)

	// Schema-level validation.
	for _, v := range links.Validate(ctx, registry, links.ValidateOptions{
		CampaignRoot: root,
		WorkitemIDs:  nil, // existence is checked below as a separate finding
	}) {
		findings = append(findings, docFinding{
			Code:     codeSchemaViolation,
			Severity: docSeverityError,
			Target:   targetForLinkID(v.LinkID),
			Message:  v.Field + ": " + v.Message,
		})
	}

	promotedTargets := promotedFestivalTargets(ctx, root)
	shelved := dungeonedWorkitems(ctx, root)
	primarySeen := make(map[string]string)
	for _, link := range registry.Links {
		// A deprecated related-project link (doc 04 D005) is handled solely by
		// the migration below: --fix moves it into the workitem's projects: and
		// removes the row, and a row whose workitem cannot be resolved is left in
		// place and reported. It is never removed as a broken link/scope, so no
		// data is lost.
		deprecatedRelatedProject := link.Role == links.RoleRelated && link.Scope.Kind == links.ScopeProject

		if _, known := knownIDs[link.WorkitemID]; !known && !deprecatedRelatedProject {
			finding := docFinding{
				Code:        codeBrokenLink,
				Severity:    docSeverityError,
				Target:      "link:" + link.ID,
				Message:     "workitem_id " + link.WorkitemID + " is not present on disk",
				FixHint:     "auto-fix removes the link",
				AutoFixable: true,
			}
			// A link is an active workitem's attachment to a working location,
			// so a shelved workitem should not hold one. Removal is the right
			// action either way; saying which case this is turns "your registry
			// is corrupt" into "this is leftover housekeeping". Promote now
			// releases these itself, so this path covers workitems dungeoned
			// before that landed.
			//
			// A festival target outranks that framing and is handled below: the
			// source sits in a dungeon because doFestivalPromote put it there,
			// and the row has somewhere real to go rather than being cleanup.
			// Keeping it at error severity leaves doctor's exit code pointed at
			// the one case that still needs a decision.
			_, hasFestivalTarget := promotedTargets[link.WorkitemID]
			if dungeonPath, ok := shelved[link.WorkitemID]; ok && !hasFestivalTarget {
				finding.Code = codeWorkitemShelved
				finding.Severity = docSeverityWarning
				finding.Message = "workitem " + link.WorkitemID + " is shelved at " + dungeonPath +
					"; a workitem that is no longer active should not hold links"
				finding.FixHint = "auto-fix removes the link"
			}
			if target, ok := promotedTargets[link.WorkitemID]; ok {
				finding.Message = "workitem_id " + link.WorkitemID +
					" is not present on disk; it was promoted to festival " + target.id
				finding.FixHint = "auto-fix re-links to festival " + target.id +
					" (or re-link manually: camp workitem link " + target.id +
					" --worktree <scope> --replace)"
				finding.migrateToID = target.id
				finding.migrateToKey = target.key
			}
			findings = append(findings, finding)
		}
		scopeMissing := !scopeTargetExists(root, link.Scope.Path)
		if scopeMissing && !deprecatedRelatedProject {
			// links.yaml is tracked, so removing a row propagates to every
			// machine this campaign syncs to. A missing worktree or an
			// uninitialized submodule is absent *here*, not gone, and deleting
			// its link would destroy a row that is correct elsewhere. Report
			// those and leave them alone.
			if links.MachineLocal(root, link.Scope) {
				findings = append(findings, docFinding{
					Code:     codeScopeNotLocal,
					Severity: docSeverityWarning,
					Target:   "link:" + link.ID,
					Message: "scope path " + link.Scope.Path + " is not on this machine" +
						" (" + string(link.Scope.Kind) + " scopes are machine-local)",
					FixHint: "expected if the worktree or submodule lives on another machine;" +
						" remove it explicitly with `camp workitem unlink --id " + link.ID + "` if it is really gone",
				})
			} else {
				findings = append(findings, docFinding{
					Code:        codeBrokenScope,
					Severity:    docSeverityError,
					Target:      "link:" + link.ID,
					Message:     "scope path " + link.Scope.Path + " does not exist",
					FixHint:     "remove the link or restore the directory; auto-fix removes the link",
					AutoFixable: true,
				})
			}
		}
		if err := quest.ValidateLinkPath(root, link.Scope.Path); err != nil {
			switch {
			case errors.Is(err, camperrors.ErrInvalidInput):
				findings = append(findings, docFinding{
					Code:     codeOutOfBounds,
					Severity: docSeverityError,
					Target:   "link:" + link.ID,
					Message:  "scope path " + link.Scope.Path + " escapes the campaign root",
				})
			case !scopeMissing:
				findings = append(findings, docFinding{
					Code:     codeScopeUnvalidatable,
					Severity: docSeverityError,
					Target:   "link:" + link.ID,
					Message:  "scope path " + link.Scope.Path + " could not be validated: " + err.Error(),
				})
			}
		}
		if link.Role == links.RolePrimary {
			key := string(link.Scope.Kind) + "::" + link.Scope.Path
			if other, dup := primarySeen[key]; dup {
				findings = append(findings, docFinding{
					Code:     codeDuplicatePrimary,
					Severity: docSeverityError,
					Target:   "scope:" + key,
					Message:  "primary links " + other + " and " + link.ID + " collide on the same scope",
				})
			} else {
				primarySeen[key] = link.ID
			}
		}
		if deprecatedRelatedProject {
			findings = append(findings, docFinding{
				Code:        codeDeprecatedRelatedProject,
				Severity:    docSeverityWarning,
				Target:      "link:" + link.ID,
				Message:     "workitem " + link.WorkitemID + " has a deprecated related-project link to " + link.Scope.Path,
				FixHint:     "run `camp workitem doctor --fix` to migrate it into the workitem's projects: field",
				AutoFixable: true,
			})
		}
	}

	// Workitems missing the ref field added in v1alpha6. Uses the Discover
	// snapshot from workitemIDsOnDisk. Sorted by path so DeriveUnique's
	// collision retry during --fix is deterministic.
	for _, rel := range workitemPathsMissingRef(root, items) {
		findings = append(findings, docFinding{
			Code:        codeMissingRefField,
			Severity:    docSeverityWarning,
			Target:      "workitem:" + rel,
			Message:     "workitem at " + rel + " is missing the ref field added in v1alpha6",
			FixHint:     "run camp workitem doctor --fix to backfill",
			AutoFixable: true,
		})
	}

	var renames map[string]string
	renamesLoaded := false
	for _, item := range items {
		for _, projectPath := range item.Projects {
			_, statErr := os.Stat(filepath.Join(root, filepath.FromSlash(projectPath)))
			switch {
			case statErr == nil:
				continue
			case errors.Is(statErr, fs.ErrNotExist):
				finding := docFinding{
					Code:        codeProjectNotFound,
					Severity:    docSeverityWarning,
					Target:      "workitem:" + item.RelativePath,
					Message:     "projects entry " + projectPath + " does not exist",
					FixHint:     "verify the project was renamed/removed intentionally; doctor --fix does not auto-remove this entry",
					AutoFixable: false,
				}
				if !renamesLoaded {
					renames = loadProjectRenameMap(ctx, root)
					renamesLoaded = true
				}
				if toRoot, ok := renames[projectRootPath(projectPath)]; ok && toRoot != "" {
					to := mappedProjectPath(projectPath, toRoot)
					finding.AutoFixable = true
					finding.FixHint = "run `camp workitem doctor --fix` to rewrite " + projectPath + " -> " + to
					finding.renameFrom = projectPath
					finding.renameTo = to
				}
				findings = append(findings, finding)
			default:
				findings = append(findings, docFinding{
					Code:        codeProjectUnvalidatable,
					Severity:    docSeverityWarning,
					Target:      "workitem:" + item.RelativePath,
					Message:     "projects entry " + projectPath + " could not be validated: " + statErr.Error(),
					FixHint:     "resolve the filesystem error (for example a permissions issue) and re-run doctor",
					AutoFixable: false,
				})
			}
		}
	}

	sort.SliceStable(findings, func(i, j int) bool {
		if findings[i].Code != findings[j].Code {
			return findings[i].Code < findings[j].Code
		}
		return findings[i].Target < findings[j].Target
	})
	return findings
}

func autoFixWorkitemFindings(ctx context.Context, root string, registry *links.Links, findings []docFinding, items []wkitem.WorkItem, errw io.Writer) (int, error) {
	applied := 0
	needsRefBackfill := false
	for _, f := range findings {
		if !f.AutoFixable {
			continue
		}
		switch f.Code {
		case codeBrokenLink, codeWorkitemShelved:
			id := strings.TrimPrefix(f.Target, "link:")
			if f.migrateToID != "" {
				if repointLinkByID(registry, id, f.migrateToID, f.migrateToKey) {
					applied++
				}
			} else if registry.RemoveLinkByID(id) {
				applied++
			}
		case codeBrokenScope:
			id := strings.TrimPrefix(f.Target, "link:")
			if registry.RemoveLinkByID(id) {
				applied++
			}
		case codeMissingRefField:
			needsRefBackfill = true
		case codeProjectNotFound:
			rel := strings.TrimPrefix(f.Target, "workitem:")
			if f.renameFrom == "" || f.renameTo == "" || rel == "" {
				continue
			}
			if err := rewriteWorkitemProjectPath(ctx, root, rel, f.renameFrom, f.renameTo); err != nil {
				if _, writeErr := fmt.Fprintf(errw, "warning: cannot rewrite project path on %s: %v\n", rel, err); writeErr != nil {
					return applied, writeErr
				}
				continue
			}
			applied++
		case codeDeprecatedRelatedProject:
			linkID := strings.TrimPrefix(f.Target, "link:")
			link, ok := registry.FindByID(linkID)
			if !ok {
				continue
			}
			if err := migrateRelatedProjectLink(ctx, root, *link); err != nil {
				// Unmigratable (e.g. the workitem no longer resolves): report and
				// leave the row in place. Never delete the data.
				if _, writeErr := fmt.Fprintf(errw, "warning: cannot migrate related-project link %s: %v\n", linkID, err); writeErr != nil {
					return applied, writeErr
				}
			} else if registry.RemoveLinkByID(linkID) {
				applied++
			}
		}
	}
	if needsRefBackfill {
		n, failures, err := backfillMissingRefs(ctx, root, items)
		applied += n
		for _, f := range failures {
			if _, writeErr := fmt.Fprintf(errw, "warning: backfill ref for %s: %v\n", f.RelativePath, f.Err); writeErr != nil {
				return applied, writeErr
			}
		}
		if err != nil {
			if _, writeErr := fmt.Fprintf(errw, "warning: backfill refs: %v\n", err); writeErr != nil {
				return applied, writeErr
			}
			return applied, nil
		}
	}
	return applied, nil
}

func targetForLinkID(linkID string) string {
	if linkID == "" {
		return "registry"
	}
	return "link:" + linkID
}

func scopeTargetExists(root, scopePath string) bool {
	if scopePath == "" {
		return false
	}
	abs := filepath.Join(root, filepath.FromSlash(scopePath))
	_, err := os.Stat(abs)
	if err == nil {
		return true
	}
	return !errors.Is(err, fs.ErrNotExist)
}

func workitemIDsOnDisk(ctx context.Context, root string) (map[string]struct{}, []wkitem.WorkItem, error) {
	cfg, err := config.LoadCampaignConfig(ctx, root)
	if err != nil {
		return nil, nil, camperrors.Wrap(err, "load campaign config")
	}
	resolver := paths.NewResolverFromConfig(root, cfg)
	items, err := wkitem.Discover(ctx, root, resolver)
	if err != nil {
		return nil, nil, err
	}
	set := make(map[string]struct{}, len(items))
	for _, item := range items {
		if item.StableID != "" {
			set[item.StableID] = struct{}{}
		}
		if item.Key != "" {
			set[item.Key] = struct{}{}
		}
		// Festivals and intents are first-class link targets addressed by the id
		// declared in their own source document (a fest.yaml id such as SC0001,
		// an intent's frontmatter id); keep those ids "present on disk" so a
		// link written against one is not reported as broken.
		if id := wkitem.SourceDeclaredID(&item); id != "" {
			set[id] = struct{}{}
		}
	}
	return set, items, nil
}

func hasErrorFinding(findings []docFinding) bool {
	for _, f := range findings {
		if f.Severity == docSeverityError {
			return true
		}
	}
	return false
}

// festivalTarget is a resolvable festival link target: its single-segment
// fest.yaml id and its "festival:<path>" key.
type festivalTarget struct {
	id  string
	key string
}

// promotedFestivalTargets maps a promoted workitem's stable id to the festival
// it was promoted to, for every design/explore workitem marker (including
// shelved ones) that recorded a promoted_to festival that still exists. It lets
// doctor offer a broken link a non-destructive migration onto that festival
// instead of only deletion. Best-effort: unreadable markers are skipped.
func promotedFestivalTargets(ctx context.Context, root string) map[string]festivalTarget {
	out := map[string]festivalTarget{}
	cfg, err := config.LoadCampaignConfig(ctx, root)
	if err != nil {
		return out
	}
	resolver := paths.NewResolverFromConfig(root, cfg)
	for _, dir := range []string{resolver.Design(), resolver.Explore()} {
		_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil || d.IsDir() || d.Name() != wkitem.MetadataFilename {
				return nil
			}
			meta, err := wkitem.LoadMetadata(ctx, filepath.Dir(path))
			if err != nil || meta == nil || meta.ID == "" || meta.PromotedTo == "" {
				return nil
			}
			promotedTo := filepath.ToSlash(meta.PromotedTo)
			if !strings.HasPrefix(promotedTo, "festivals/") {
				return nil
			}
			festID := readFestivalID(root, promotedTo)
			if festID == "" {
				return nil
			}
			out[meta.ID] = festivalTarget{id: festID, key: "festival:" + promotedTo}
			return nil
		})
	}
	return out
}

// repointLinkByID re-points the link with the given id onto a new workitem id
// and key. Returns true when a link was updated.
func repointLinkByID(registry *links.Links, linkID, newID, newKey string) bool {
	for i := range registry.Links {
		if registry.Links[i].ID == linkID {
			registry.Links[i].WorkitemID = newID
			registry.Links[i].WorkitemKey = newKey
			return true
		}
	}
	return false
}

func emitDocHuman(w io.Writer, findings []docFinding) error {
	if len(findings) == 0 {
		_, err := fmt.Fprintln(w, "doctor: 0 findings")
		return err
	}
	if _, err := fmt.Fprintf(w, "doctor: %d finding(s)\n", len(findings)); err != nil {
		return err
	}
	for _, f := range findings {
		if _, err := fmt.Fprintf(w, "  [%s] %s %s — %s\n", f.Severity, f.Code, f.Target, f.Message); err != nil {
			return err
		}
		if f.FixHint != "" {
			if _, err := fmt.Fprintln(w, "    hint: "+f.FixHint); err != nil {
				return err
			}
		}
	}
	return nil
}

func emitDocJSON(w io.Writer, findings []docFinding) error {
	if findings == nil {
		findings = []docFinding{}
	}
	out := struct {
		SchemaVersion string       `json:"schema_version"`
		GeneratedAt   time.Time    `json:"generated_at"`
		Findings      []docFinding `json:"findings"`
		ErrorCount    int          `json:"error_count"`
		WarningCount  int          `json:"warning_count"`
		InfoCount     int          `json:"info_count"`
	}{
		SchemaVersion: WorkitemDoctorJSONVersion,
		GeneratedAt:   time.Now().UTC(),
		Findings:      findings,
	}
	for _, f := range findings {
		switch f.Severity {
		case docSeverityError:
			out.ErrorCount++
		case docSeverityWarning:
			out.WarningCount++
		case docSeverityInfo:
			out.InfoCount++
		}
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

// collectResidentFindings reports lifecycle directories camp cannot act on: one
// that is neither a stamped resident nor a festival, and a resident whose type
// root is gone so demote has nowhere to land.
func collectResidentFindings(root string, items []wkitem.WorkItem) []docFinding {
	findings := unclassifiableLifecycleDirs(root)
	return append(findings, residentsWithoutHome(root, items)...)
}

func unclassifiableLifecycleDirs(root string) []docFinding {
	var findings []docFinding
	for _, stage := range []string{railStageReady, railStageActive} {
		stageDir := filepath.Join(root, festivalsDir, stage)
		entries, err := os.ReadDir(stageDir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
				continue
			}
			dir := filepath.Join(stageDir, e.Name())
			if pathExists(filepath.Join(dir, wkitem.MetadataFilename)) || isFestivalDir(dir) {
				continue
			}
			rel := festivalsDir + "/" + stage + "/" + e.Name()
			findings = append(findings, docFinding{
				Code:     codeUnstampedResident,
				Severity: docSeverityWarning,
				Target:   rel,
				Message:  rel + " is neither a stamped resident nor a valid festival",
				FixHint:  "stamp it with camp workitem adopt/create, or add festival markers; --fix does not guess",
			})
		}
	}
	return findings
}

func residentsWithoutHome(root string, items []wkitem.WorkItem) []docFinding {
	var findings []docFinding
	for _, it := range items {
		if it.WorkflowType == wkitem.WorkflowTypeFestival {
			continue
		}
		rel := filepath.ToSlash(it.RelativePath)
		if !strings.HasPrefix(rel, festivalsDir+"/") {
			continue
		}
		typeRoot := filepath.Join(root, "workflow", string(it.WorkflowType))
		if pathExists(typeRoot) {
			continue
		}
		findings = append(findings, docFinding{
			Code:     codeResidentMissingHome,
			Severity: docSeverityWarning,
			Target:   rel,
			Message: "resident " + rel + " has no home type root workflow/" +
				string(it.WorkflowType) + "/ to demote into",
			FixHint: "recreate workflow/" + string(it.WorkflowType) + "/ or demote to an existing type",
		})
	}
	return findings
}

// isFestivalDir reports a fest-owned directory by its markers. camp does not
// import fest, so the marker names are duplicated here deliberately.
func isFestivalDir(dir string) bool {
	for _, name := range []string{"fest.yaml", "FESTIVAL_GOAL.md", "FESTIVAL_OVERVIEW.md"} {
		if pathExists(filepath.Join(dir, name)) {
			return true
		}
	}
	return false
}

func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
