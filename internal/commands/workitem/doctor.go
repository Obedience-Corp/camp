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
	"github.com/Obedience-Corp/camp/internal/paths"
	"github.com/Obedience-Corp/camp/internal/quest"
	wkitem "github.com/Obedience-Corp/camp/internal/workitem"
	"github.com/Obedience-Corp/camp/internal/workitem/links"
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
}

// errDoctorIssues triggers a non-zero exit from cobra after we have already
// emitted findings.
var errDoctorIssues = camperrors.NewValidation("doctor", "doctor reported error-severity findings", nil)

func newDoctorCommand() *cobra.Command {
	var jsonOut, fix bool
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Report workitem link-registry health issues",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDoctor(cmd.Context(), cmd, jsonOut, fix)
		},
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "emit a structured JSON result")
	cmd.Flags().BoolVar(&fix, "fix", false, "auto-repair findings tagged auto_fixable")
	return cmd
}

func runDoctor(ctx context.Context, cmd *cobra.Command, jsonOut, fix bool) error {
	_, root, err := config.LoadCampaignConfigFromCwd(ctx)
	if err != nil {
		return camperrors.Wrap(err, "not in a campaign directory")
	}
	registry, err := links.Load(ctx, root)
	if err != nil {
		return err
	}

	knownIDs, err := workitemIDsOnDisk(ctx, root)
	if err != nil {
		return err
	}

	findings := collectWorkitemFindings(ctx, root, registry, knownIDs)
	if fix {
		applied := autoFixWorkitemFindings(ctx, root, registry, findings)
		if applied > 0 {
			if err := links.Save(ctx, root, registry); err != nil {
				return err
			}
			// Re-run findings after fixes for an accurate post-fix report.
			knownIDs, _ = workitemIDsOnDisk(ctx, root)
			findings = collectWorkitemFindings(ctx, root, registry, knownIDs)
		}
	}

	if jsonOut {
		if err := emitDocJSON(cmd.OutOrStdout(), findings); err != nil {
			return err
		}
	} else {
		emitDocHuman(cmd.OutOrStdout(), findings)
	}

	if hasErrorFinding(findings) {
		return errDoctorIssues
	}
	return nil
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

	primarySeen := make(map[string]string)
	for _, link := range registry.Links {
		if _, known := knownIDs[link.WorkitemID]; !known {
			findings = append(findings, docFinding{
				Code:        codeBrokenLink,
				Severity:    docSeverityError,
				Target:      "link:" + link.ID,
				Message:     "workitem_id " + link.WorkitemID + " is not present on disk",
				FixHint:     "auto-fix removes the link",
				AutoFixable: true,
			})
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
	if err == nil {
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
	if err == nil && cur != nil {
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

func autoFixWorkitemFindings(ctx context.Context, root string, registry *links.Links, findings []docFinding) int {
	applied := 0
	for _, f := range findings {
		if !f.AutoFixable {
			continue
		}
		switch f.Code {
		case codeBrokenLink, codeBrokenScope:
			id := strings.TrimPrefix(f.Target, "link:")
			if registry.RemoveLinkByID(id) {
				applied++
			}
		case codeCurrentMissing:
			if err := links.SaveCurrent(ctx, root, nil); err == nil {
				applied++
			}
		case codeMissingRefField:
			rel := strings.TrimPrefix(f.Target, "workitem:")
			if backfillRef(ctx, root, rel) == nil {
				applied++
			}
		}
	}
	return applied
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

func emitDocHuman(w io.Writer, findings []docFinding) {
	if len(findings) == 0 {
		fmt.Fprintln(w, "doctor: 0 findings")
		return
	}
	fmt.Fprintf(w, "doctor: %d finding(s)\n", len(findings))
	for _, f := range findings {
		fmt.Fprintf(w, "  [%s] %s %s — %s\n", f.Severity, f.Code, f.Target, f.Message)
		if f.FixHint != "" {
			fmt.Fprintln(w, "    hint: "+f.FixHint)
		}
	}
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
		SchemaVersion: "workitem-doctor/v1alpha1",
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
