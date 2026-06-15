package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/jsoncontract"
	navindex "github.com/Obedience-Corp/camp/internal/nav/index"
)

// Doctor finding severities.
const (
	severityInfo    = "info"
	severityWarning = "warning"
	severityError   = "error"
)

// Doctor finding codes. The dotted-domain form is part of the stable contract
// consumers can dispatch on.
const (
	codeShortcutMissingTarget = "workflow.shortcut.missing-target"
	codeConceptMissingDir     = "workflow.concept.missing-dir"
	codeDirMissingConcept     = "workflow.dir.missing-concept"
	codeDirMissingShortcut    = "workflow.dir.missing-shortcut"
	codeShortcutDuplicate     = "workflow.shortcut.duplicate"
	codeCacheStale            = "workflow.cache.stale"
)

// finding is the stable doctor output unit.
type finding struct {
	Code        string `json:"code"`
	Severity    string `json:"severity"`
	Target      string `json:"target,omitempty"`
	Message     string `json:"message"`
	FixHint     string `json:"fix_hint,omitempty"`
	AutoFixable bool   `json:"auto_fixable"`
}

// errDoctorIssuesFound is returned by runDoctor when any finding has severity
// error. main propagates the exit code without re-printing the findings because
// runDoctor already emitted them.
var errDoctorIssuesFound = camperrors.NewCommand(
	"camp workflow doctor",
	2,
	"doctor reported error-severity findings",
	nil,
)

func newDoctorCommand() *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Report workflow surface inconsistencies",
		Args:  jsoncontract.Args(JSONSchemaVersion, func() bool { return jsonOut }, cobra.NoArgs),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDoctor(cmd.Context(), cmd, jsonOut)
		},
		SilenceErrors: true,
	}
	cmd.SetFlagErrorFunc(jsoncontract.FlagErrorFunc(JSONSchemaVersion, func() bool { return jsonOut }))
	cmd.Flags().BoolVar(&jsonOut, "json", false, "emit a structured JSON result")
	return cmd
}

func runDoctor(ctx context.Context, cmd *cobra.Command, jsonOut bool) error {
	cfg, campaignRoot, err := config.LoadCampaignConfigFromCwd(ctx)
	if err != nil {
		return renderWorkflowDoctorError(cmd, jsonOut, camperrors.Wrap(err, "not in a campaign directory"))
	}

	findings, err := collectFindings(ctx, campaignRoot, cfg)
	if err != nil {
		return renderWorkflowDoctorError(cmd, jsonOut, err)
	}

	if jsonOut {
		if err := emitDoctorJSON(cmd.OutOrStdout(), findings); err != nil {
			return err
		}
	} else {
		if err := emitDoctorHuman(cmd.OutOrStdout(), findings); err != nil {
			return err
		}
	}

	if hasErrorFindings(findings) {
		return errDoctorIssuesFound
	}
	return nil
}

func renderWorkflowDoctorError(cmd *cobra.Command, jsonOut bool, err error) error {
	if err == nil || !jsonOut {
		return err
	}
	return jsoncontract.RenderError(cmd, JSONSchemaVersion, err)
}

func collectFindings(ctx context.Context, campaignRoot string, cfg *config.CampaignConfig) ([]finding, error) {
	var findings []finding

	shortcuts := cfg.Shortcuts()
	concepts := flattenConcepts(cfg.Concepts())

	// workflow.shortcut.missing-target: shortcut points to nonexistent
	// workflow/<type>/ directory.
	keys := make([]string, 0, len(shortcuts))
	for k := range shortcuts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, key := range keys {
		sc := shortcuts[key]
		if !strings.HasPrefix(sc.Path, "workflow/") {
			continue
		}
		typeName := workflowTypeFromPath(sc.Path)
		if typeName == "" || builtinWorkflowTypes[typeName] {
			continue
		}
		abs := filepath.Join(campaignRoot, filepath.FromSlash(sc.Path))
		if _, err := os.Stat(abs); err != nil {
			if !errors.Is(err, fs.ErrNotExist) {
				return nil, camperrors.Wrapf(err, "stat %s", sc.Path)
			}
			findings = append(findings, finding{
				Code:        codeShortcutMissingTarget,
				Severity:    severityError,
				Target:      "shortcut:" + key,
				Message:     fmt.Sprintf("shortcut %q points to missing %s", key, sc.Path),
				FixHint:     "remove the shortcut from .campaign/settings/jumps.yaml or restore the directory; auto-fix removes the shortcut",
				AutoFixable: true,
			})
		}
	}

	// workflow.concept.missing-dir: concept entry references missing
	// workflow/<type>/ directory.
	for _, concept := range concepts {
		if !strings.HasPrefix(concept.Path, "workflow/") {
			continue
		}
		typeName := workflowTypeFromPath(concept.Path)
		if typeName == "" || builtinWorkflowTypes[typeName] {
			continue
		}
		abs := filepath.Join(campaignRoot, filepath.FromSlash(concept.Path))
		if _, err := os.Stat(abs); err != nil {
			if !errors.Is(err, fs.ErrNotExist) {
				return nil, camperrors.Wrapf(err, "stat %s", concept.Path)
			}
			findings = append(findings, finding{
				Code:        codeConceptMissingDir,
				Severity:    severityError,
				Target:      "concept:" + concept.Name,
				Message:     fmt.Sprintf("concept %q points to missing %s", concept.Name, concept.Path),
				FixHint:     "remove the concept from .campaign/campaign.yaml or restore the directory; auto-fix removes the concept",
				AutoFixable: true,
			})
		}
	}

	// workflow.dir.missing-concept and workflow.dir.missing-shortcut: walk
	// workflow/ on disk and check coverage.
	workflowRoot := filepath.Join(campaignRoot, "workflow")
	dirEntries, err := os.ReadDir(workflowRoot)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, camperrors.Wrap(err, "read workflow root")
	}
	conceptByPath := make(map[string]string)
	for _, c := range concepts {
		conceptByPath[strings.TrimRight(c.Path, "/")] = c.Name
	}
	shortcutByPath := make(map[string]string)
	for k, sc := range shortcuts {
		shortcutByPath[strings.TrimRight(sc.Path, "/")] = k
	}
	for _, de := range dirEntries {
		if !de.IsDir() {
			continue
		}
		typeName := de.Name()
		if strings.HasPrefix(typeName, ".") || builtinWorkflowTypes[typeName] {
			continue
		}
		rel := path.Join("workflow", typeName)
		if _, ok := conceptByPath[rel]; !ok {
			findings = append(findings, finding{
				Code:        codeDirMissingConcept,
				Severity:    severityWarning,
				Target:      "dir:" + rel + "/",
				Message:     fmt.Sprintf("workflow %s has no concept entry", rel+"/"),
				FixHint:     "auto-fix adds a concept entry derived from the directory name",
				AutoFixable: true,
			})
		}
		if _, ok := shortcutByPath[rel]; !ok {
			findings = append(findings, finding{
				Code:        codeDirMissingShortcut,
				Severity:    severityInfo,
				Target:      "dir:" + rel + "/",
				Message:     fmt.Sprintf("workflow %s has no shortcut", rel+"/"),
				FixHint:     "add one with: camp workflow shortcut add " + typeName + " <key>",
				AutoFixable: false,
			})
		}
	}

	// workflow.shortcut.duplicate: two shortcut keys normalize to the same
	// value.
	for normalized, dupes := range duplicateShortcutKeys(shortcuts) {
		findings = append(findings, finding{
			Code:        codeShortcutDuplicate,
			Severity:    severityError,
			Target:      "shortcut:" + normalized,
			Message:     fmt.Sprintf("duplicate shortcut keys normalize to %q: %s", normalized, strings.Join(dupes, ", ")),
			FixHint:     "manually consolidate the entries in .campaign/settings/jumps.yaml; auto-fix keeps the normalized (lowercase) form when present, otherwise the lexicographically first variant",
			AutoFixable: true,
		})
	}

	// workflow.cache.stale: nav cache mtime older than newest workflow dir
	// mtime.
	if stale, err := isNavCacheStaleForWorkflow(ctx, campaignRoot); err != nil {
		return nil, err
	} else if stale {
		findings = append(findings, finding{
			Code:        codeCacheStale,
			Severity:    severityWarning,
			Target:      "cache:nav",
			Message:     "navigation index is older than recent workflow changes",
			FixHint:     "auto-fix deletes the cache (it rebuilds on next use)",
			AutoFixable: true,
		})
	}

	sort.SliceStable(findings, func(i, j int) bool {
		if findings[i].Code != findings[j].Code {
			return findings[i].Code < findings[j].Code
		}
		return findings[i].Target < findings[j].Target
	})
	return findings, nil
}

const maxWorkflowWalkEntries = 10_000

func isNavCacheStaleForWorkflow(ctx context.Context, campaignRoot string) (bool, error) {
	idx, err := navindex.Load(campaignRoot)
	if err != nil || idx == nil {
		return false, nil
	}
	workflowRoot := filepath.Join(campaignRoot, "workflow")
	if _, err := os.Stat(workflowRoot); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return false, nil
		}
		return false, camperrors.Wrap(err, "stat workflow root")
	}

	stale := false
	entries := 0
	walkErr := filepath.WalkDir(workflowRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		entries++
		if entries > maxWorkflowWalkEntries {
			return filepath.SkipAll
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		if info.ModTime().After(idx.BuildTime) {
			stale = true
			return filepath.SkipAll
		}
		return nil
	})
	if walkErr != nil && !errors.Is(walkErr, filepath.SkipAll) {
		return false, camperrors.Wrap(walkErr, "walk workflow tree")
	}
	return stale, nil
}

func hasErrorFindings(findings []finding) bool {
	for _, f := range findings {
		if f.Severity == severityError {
			return true
		}
	}
	return false
}

func emitDoctorHuman(w io.Writer, findings []finding) error {
	if len(findings) == 0 {
		_, err := fmt.Fprintln(w, "doctor: 0 findings")
		return err
	}
	if _, err := fmt.Fprintf(w, "doctor: %d finding(s)\n", len(findings)); err != nil {
		return err
	}
	for _, f := range findings {
		fixHint := ""
		if f.FixHint != "" {
			fixHint = "  hint: " + f.FixHint
		}
		if _, err := fmt.Fprintf(w, "  [%s] %s %s — %s\n", f.Severity, f.Code, f.Target, f.Message); err != nil {
			return err
		}
		if fixHint != "" {
			if _, err := fmt.Fprintln(w, fixHint); err != nil {
				return err
			}
		}
	}
	return nil
}

func emitDoctorJSON(w io.Writer, findings []finding) error {
	out := struct {
		SchemaVersion string    `json:"schema_version"`
		GeneratedAt   time.Time `json:"generated_at"`
		Findings      []finding `json:"findings"`
		ErrorCount    int       `json:"error_count"`
		WarningCount  int       `json:"warning_count"`
		InfoCount     int       `json:"info_count"`
	}{
		SchemaVersion: JSONSchemaVersion,
		GeneratedAt:   time.Now().UTC(),
		Findings:      findings,
	}
	if out.Findings == nil {
		out.Findings = []finding{}
	}
	for _, f := range findings {
		switch f.Severity {
		case severityError:
			out.ErrorCount++
		case severityWarning:
			out.WarningCount++
		case severityInfo:
			out.InfoCount++
		}
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}
