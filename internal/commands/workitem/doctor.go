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
	codeBrokenLink         = "workitem.link.broken"
	codeBrokenScope        = "workitem.scope.broken"
	codeOutOfBounds        = "workitem.scope.out-of-bounds"
	codeScopeUnvalidatable = "workitem.scope.unvalidatable"
	codeDuplicatePrimary   = "workitem.link.duplicate-primary"
	codeSchemaViolation    = "workitem.schema.violation"
	codeCurrentMissing     = "workitem.current.missing"
	codeMissingRefField    = "workitem.ref.missing"
	codeWorkitemScanFailed = "workitem.scan.failed"
	codeRegistryParseError = "workitem.registry.parse-error"
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
references. Use --fix to apply auto-repairs for supported findings. Use --json
for machine-readable findings and stable finding codes.`,
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
	knownIDs, err := workitemIDsOnDisk(ctx, root)
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
			findings = collectWorkitemFindings(ctx, root, registry, knownIDs)
			applied, fixErr := autoFixWorkitemFindings(ctx, root, registry, findings, cmd.ErrOrStderr())
			if fixErr != nil {
				return fixErr
			}
			if applied == 0 {
				return links.ErrSkipSave
			}
			knownIDs, _ = workitemIDsOnDisk(ctx, root)
			findings = collectWorkitemFindings(ctx, root, registry, knownIDs)
			return nil
		})
		if err != nil {
			return renderWorkitemDoctorError(cmd, jsonOut, err)
		}
		knownIDs, err = workitemIDsOnDisk(ctx, root)
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
		findings = collectWorkitemFindings(ctx, root, registry, knownIDs)
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

func collectWorkitemFindings(ctx context.Context, root string, registry *links.Links, knownIDs map[string]struct{}) []docFinding {
	var findings []docFinding

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
	primarySeen := make(map[string]string)
	for _, link := range registry.Links {
		if _, known := knownIDs[link.WorkitemID]; !known {
			finding := docFinding{
				Code:        codeBrokenLink,
				Severity:    docSeverityError,
				Target:      "link:" + link.ID,
				Message:     "workitem_id " + link.WorkitemID + " is not present on disk",
				FixHint:     "auto-fix removes the link",
				AutoFixable: true,
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
		if scopeMissing {
			findings = append(findings, docFinding{
				Code:        codeBrokenScope,
				Severity:    docSeverityError,
				Target:      "link:" + link.ID,
				Message:     "scope path " + link.Scope.Path + " does not exist",
				FixHint:     "remove the link or restore the directory; auto-fix removes the link",
				AutoFixable: true,
			})
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
	}

	// Workitems missing the ref field added in v1alpha6. Sorted by path so
	// the order in which DeriveUnique fills collisions during --fix is
	// deterministic.
	missingRefPaths, err := workitemPathsMissingRef(ctx, root)
	if err != nil {
		findings = append(findings, docFinding{
			Code:     codeWorkitemScanFailed,
			Severity: docSeverityError,
			Target:   "workitem-tree",
			Message:  "could not scan workitems for ref backfill: " + err.Error(),
		})
	} else {
		for _, rel := range missingRefPaths {
			findings = append(findings, docFinding{
				Code:        codeMissingRefField,
				Severity:    docSeverityWarning,
				Target:      "workitem:" + rel,
				Message:     "workitem at " + rel + " is missing the ref field added in v1alpha6",
				FixHint:     "run camp workitem doctor --fix to backfill",
				AutoFixable: true,
			})
		}
	}

	cur, err := links.LoadCurrent(ctx, root)
	if err != nil {
		findings = append(findings, docFinding{
			Code:        codeSchemaViolation,
			Severity:    docSeverityError,
			Target:      "current:current.yaml",
			Message:     "current.yaml cannot be loaded: " + err.Error(),
			FixHint:     "auto-fix removes current.yaml",
			AutoFixable: true,
		})
	} else if cur != nil {
		if _, known := knownIDs[cur.WorkitemID]; !known {
			findings = append(findings, docFinding{
				Code:        codeCurrentMissing,
				Severity:    docSeverityWarning,
				Target:      "current:" + cur.WorkitemID,
				Message:     "current.yaml points at workitem " + cur.WorkitemID + " which is not present on disk",
				FixHint:     "auto-fix removes current.yaml",
				AutoFixable: true,
			})
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

func autoFixWorkitemFindings(ctx context.Context, root string, registry *links.Links, findings []docFinding, errw io.Writer) (int, error) {
	applied := 0
	needsRefBackfill := false
	for _, f := range findings {
		if !f.AutoFixable {
			continue
		}
		switch f.Code {
		case codeBrokenLink:
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
		case codeCurrentMissing:
			if err := links.SaveCurrent(ctx, root, nil); err == nil {
				applied++
			}
		case codeSchemaViolation:
			if f.Target == "current:current.yaml" {
				if err := links.SaveCurrent(ctx, root, nil); err == nil {
					applied++
				}
			}
		case codeMissingRefField:
			needsRefBackfill = true
		}
	}
	if needsRefBackfill {
		n, failures, err := backfillMissingRefs(ctx, root)
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

func workitemIDsOnDisk(ctx context.Context, root string) (map[string]struct{}, error) {
	cfg, err := config.LoadCampaignConfig(ctx, root)
	if err != nil {
		return nil, camperrors.Wrap(err, "load campaign config")
	}
	resolver := paths.NewResolverFromConfig(root, cfg)
	items, err := wkitem.Discover(ctx, root, resolver)
	if err != nil {
		return nil, err
	}
	set := make(map[string]struct{}, len(items))
	for _, item := range items {
		if item.StableID != "" {
			set[item.StableID] = struct{}{}
		}
		if item.Key != "" {
			set[item.Key] = struct{}{}
		}
		// Festivals are first-class link targets addressed by their fest.yaml
		// id (e.g. SC0001); keep that id "present on disk" so a link migrated
		// onto a festival by promote is not reported as broken.
		if item.WorkflowType == wkitem.WorkflowTypeFestival && item.SourceID != "" {
			set[item.SourceID] = struct{}{}
		}
	}
	return set, nil
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
