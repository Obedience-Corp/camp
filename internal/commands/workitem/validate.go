package workitem

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/jsoncontract"
	"github.com/Obedience-Corp/camp/internal/paths"
	"github.com/Obedience-Corp/camp/internal/remote"
	wkitem "github.com/Obedience-Corp/camp/internal/workitem"
)

// Validate finding codes (dotted-domain form, stable). codeMissingRefField is
// shared with `camp workitem doctor` so agents dispatch on one ref code.
const (
	codeMarkerMissing   = "workitem.marker.missing"
	codeMarkerMalformed = "workitem.marker.malformed"
	codeTypeMismatch    = "workitem.type.mismatch"
	codeSchemaOutdated  = "workitem.schema.outdated"
)

type validateFinding struct {
	Code          string `json:"code"`
	Severity      string `json:"severity"`
	Target        string `json:"target"`
	Message       string `json:"message"`
	RepairCommand string `json:"repair_command,omitempty"`
	Repairable    bool   `json:"repairable"`
}

// errValidateIssues triggers a non-zero exit after findings are already
// emitted, mirroring the doctor convention.
var errValidateIssues = camperrors.NewCommand(
	"camp workitem validate",
	2,
	"validate reported error-severity findings",
	nil,
)

func newValidateCommand() *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "validate [path]",
		Short: "Validate workflow work item directories and their .workitem markers",
		Long: `Validate that workflow work item directories carry a correct .workitem marker.

Without an argument, every work item directory under workflow/ is scanned:
builtin doc directories (workflow/design, workflow/explore) are always work
items, custom type directories surface only when they carry a marker, and
dungeon/hidden control areas are ignored. With a path argument, only that
directory is validated.

Each problem prints the exact repair command, for example
"camp workitem repair workflow/design/foo". Use --json for stable finding
codes. The command exits non-zero when any error-severity finding is present.`,
		Args: jsoncontract.Args(WorkitemValidateJSONVersion, func() bool { return jsonOut }, cobra.MaximumNArgs(1)),
		Annotations: map[string]string{
			"agent_allowed": "true",
			"agent_reason":  "Read-only structural validator with --json output",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			target := ""
			if len(args) == 1 {
				target = args[0]
			}
			return runValidate(cmd.Context(), cmd, jsonOut, target)
		},
		SilenceErrors: true,
	}
	cmd.SetFlagErrorFunc(jsoncontract.FlagErrorFunc(WorkitemValidateJSONVersion, func() bool { return jsonOut }))
	cmd.Flags().BoolVar(&jsonOut, "json", false, "emit a structured JSON result")
	return cmd
}

func runValidate(ctx context.Context, cmd *cobra.Command, jsonOut bool, target string) error {
	cfg, root, err := config.LoadCampaignConfigFromCwd(ctx)
	if err != nil {
		return renderValidateError(cmd, jsonOut, camperrors.Wrap(err, "not in a campaign directory"))
	}
	resolver := paths.NewResolverFromConfig(root, cfg)

	var candidates []workflowDirCandidate
	if target != "" {
		rel, pathType, perr := parseWorkflowTarget(root, resolver, target)
		if perr != nil {
			return renderValidateError(cmd, jsonOut, perr)
		}
		if _, statErr := os.Stat(filepath.Join(root, filepath.FromSlash(rel))); statErr != nil {
			return renderValidateError(cmd, jsonOut, camperrors.NewNotFound("directory", rel, statErr))
		}
		candidates = []workflowDirCandidate{{RelPath: rel, PathType: pathType}}
	} else {
		candidates, err = scanWorkflowWorkitemDirs(ctx, root, resolver)
		if err != nil {
			return renderValidateError(cmd, jsonOut, err)
		}
	}

	var findings []validateFinding
	for _, c := range candidates {
		if err := ctx.Err(); err != nil {
			return renderValidateError(cmd, jsonOut, err)
		}
		findings = append(findings, classifyWorkflowDir(root, c.RelPath, c.PathType)...)
	}
	sort.SliceStable(findings, func(i, j int) bool {
		if findings[i].Target != findings[j].Target {
			return findings[i].Target < findings[j].Target
		}
		return findings[i].Code < findings[j].Code
	})

	if jsonOut {
		if err := emitValidateJSON(cmd.OutOrStdout(), findings); err != nil {
			return err
		}
	} else if err := emitValidateHuman(cmd.OutOrStdout(), findings); err != nil {
		return err
	}

	for _, f := range findings {
		if f.Severity == docSeverityError {
			return errValidateIssues
		}
	}
	return nil
}

// classifyWorkflowDir reads the marker for a candidate directory and returns the
// findings for it. The pure decision logic lives in classifyMarker.
func classifyWorkflowDir(root, relPath, pathType string) []validateFinding {
	markerAbs := filepath.Join(root, filepath.FromSlash(relPath), wkitem.MetadataFilename)
	raw, err := os.ReadFile(markerAbs)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return classifyMarker(relPath, pathType, false, nil)
		}
		return []validateFinding{{
			Code:          codeMarkerMalformed,
			Severity:      docSeverityError,
			Target:        relPath,
			Message:       "cannot read .workitem marker: " + err.Error(),
			RepairCommand: repairCommandFor(relPath),
			Repairable:    false,
		}}
	}
	return classifyMarker(relPath, pathType, true, raw)
}

// classifyMarker is the pure structural check: given whether a marker is present
// and its raw bytes, it returns the findings for one workflow directory. It has
// no filesystem dependency so it is unit-tested directly.
func classifyMarker(relPath, pathType string, present bool, raw []byte) []validateFinding {
	repairCmd := repairCommandFor(relPath)
	if !present {
		return []validateFinding{{
			Code:          codeMarkerMissing,
			Severity:      docSeverityError,
			Target:        relPath,
			Message:       "no .workitem marker; directory is treated as a work item by location but is not tracked",
			RepairCommand: repairCmd,
			Repairable:    true,
		}}
	}

	var meta wkitem.Metadata
	if err := yaml.Unmarshal(raw, &meta); err != nil {
		return []validateFinding{{
			Code:          codeMarkerMalformed,
			Severity:      docSeverityError,
			Target:        relPath,
			Message:       "cannot parse .workitem marker as YAML: " + err.Error(),
			RepairCommand: repairCmd,
			Repairable:    false,
		}}
	}

	var findings []validateFinding
	malformed := func(msg string) {
		findings = append(findings, validateFinding{
			Code: codeMarkerMalformed, Severity: docSeverityError, Target: relPath,
			Message: msg, RepairCommand: repairCmd, Repairable: true,
		})
	}

	if problems := requiredFieldProblems(meta); len(problems) > 0 {
		malformed(strings.Join(problems, "; "))
	}
	if meta.Type != "" && meta.Type != pathType {
		findings = append(findings, validateFinding{
			Code: codeTypeMismatch, Severity: docSeverityError, Target: relPath,
			Message:       "marker type " + strconv.Quote(meta.Type) + " does not match path type " + strconv.Quote(pathType),
			RepairCommand: repairCmd, Repairable: true,
		})
	}
	switch {
	case !wkitem.IsAcceptedVersion(meta.Version):
		malformed("unsupported .workitem schema version " + strconv.Quote(meta.Version) + "; current is " + wkitem.WorkitemSchemaVersion)
	case !wkitem.IsCurrentVersion(meta.Version):
		findings = append(findings, validateFinding{
			Code: codeSchemaOutdated, Severity: docSeverityWarning, Target: relPath,
			Message:       "marker uses legacy schema version " + meta.Version + "; current is " + wkitem.WorkitemSchemaVersion,
			RepairCommand: repairCmd, Repairable: true,
		})
	}
	switch {
	case meta.Ref == "":
		findings = append(findings, validateFinding{
			Code: codeMissingRefField, Severity: docSeverityWarning, Target: relPath,
			Message:       "marker is missing the ref field (WI-<6 hex>)",
			RepairCommand: repairCmd, Repairable: true,
		})
	case !wkitem.ValidRef(meta.Ref):
		malformed("ref " + strconv.Quote(meta.Ref) + " is not of the form WI-<6 hex>")
	}
	if meta.QuestID != "" && !wkitem.ValidQuestID(meta.QuestID) {
		malformed("quest_id " + strconv.Quote(meta.QuestID) + " is malformed")
	}
	return findings
}

func requiredFieldProblems(meta wkitem.Metadata) []string {
	var problems []string
	if meta.Kind != wkitem.MetadataKind {
		problems = append(problems, "kind must be "+wkitem.MetadataKind)
	}
	if meta.ID == "" {
		problems = append(problems, "id is required")
	}
	if meta.Type == "" {
		problems = append(problems, "type is required")
	}
	return problems
}

func repairCommandFor(relPath string) string {
	return "camp workitem repair " + remote.ShellQuote(relPath)
}

func renderValidateError(cmd *cobra.Command, jsonOut bool, err error) error {
	if err == nil || !jsonOut {
		return err
	}
	return jsoncontract.RenderError(cmd, WorkitemValidateJSONVersion, err)
}

func emitValidateHuman(w io.Writer, findings []validateFinding) error {
	if len(findings) == 0 {
		_, err := io.WriteString(w, "validate: 0 findings\n")
		return err
	}
	if _, err := io.WriteString(w, "validate: "+strconv.Itoa(len(findings))+" finding(s)\n"); err != nil {
		return err
	}
	for _, f := range findings {
		if _, err := io.WriteString(w, "  ["+f.Severity+"] "+f.Code+" "+f.Target+" — "+f.Message+"\n"); err != nil {
			return err
		}
		if f.RepairCommand != "" {
			if _, err := io.WriteString(w, "    fix: "+f.RepairCommand+"\n"); err != nil {
				return err
			}
		}
	}
	return nil
}

func emitValidateJSON(w io.Writer, findings []validateFinding) error {
	if findings == nil {
		findings = []validateFinding{}
	}
	out := struct {
		SchemaVersion string            `json:"schema_version"`
		GeneratedAt   time.Time         `json:"generated_at"`
		Findings      []validateFinding `json:"findings"`
		ErrorCount    int               `json:"error_count"`
		WarningCount  int               `json:"warning_count"`
		InfoCount     int               `json:"info_count"`
	}{
		SchemaVersion: WorkitemValidateJSONVersion,
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
