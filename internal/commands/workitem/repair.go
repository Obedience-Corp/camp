package workitem

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"time"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/fsutil"
	"github.com/Obedience-Corp/camp/internal/jsoncontract"
	"github.com/Obedience-Corp/camp/internal/paths"
	wkitem "github.com/Obedience-Corp/camp/internal/workitem"
)

const (
	repairActionSet     = "set"
	repairActionCleared = "cleared"
	repairActionCreated = "created"
)

type repairChange struct {
	Field  string `json:"field"`
	Action string `json:"action"`
	From   string `json:"from,omitempty"`
	To     string `json:"to,omitempty"`
}

type markerRepair struct {
	meta    wkitem.Metadata
	changes []repairChange
	created bool
}

func newRepairCommand() *cobra.Command {
	var jsonOut, dryRun bool
	var typeOverride string
	cmd := &cobra.Command{
		Use:   "repair <path>",
		Short: "Repair a workflow directory into a current-schema work item",
		Long: `Repair a workflow directory so it carries a valid current-schema .workitem marker.

The directory is never moved or renamed and document contents are never touched.
When no marker exists one is created; when a legacy or incomplete marker exists
its schema version, kind, id, type, ref, and title are brought up to the current
shape. The workflow type is inferred from the path segment after workflow/, the
title from the first markdown H1 (else the humanized directory name), and id/ref
from the same rules as create and adopt. Repair is idempotent: a directory that
is already valid reports no changes. Use --dry-run to preview and --json for a
machine-readable result.`,
		Args: jsoncontract.Args(WorkitemRepairJSONVersion, func() bool { return jsonOut }, cobra.ExactArgs(1)),
		Annotations: map[string]string{
			"agent_allowed": "true",
			"agent_reason":  "Idempotent, non-destructive repair with --json and --dry-run",
		},
		RunE: jsoncontract.RunE(WorkitemRepairJSONVersion, func() bool { return jsonOut }, func(cmd *cobra.Command, args []string) error {
			return runRepair(cmd.Context(), cmd, args[0], typeOverride, dryRun, jsonOut)
		}),
	}
	cmd.SetFlagErrorFunc(jsoncontract.FlagErrorFunc(WorkitemRepairJSONVersion, func() bool { return jsonOut }))
	cmd.Flags().BoolVar(&jsonOut, "json", false, "emit a structured JSON result")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "report what would change without writing")
	cmd.Flags().StringVar(&typeOverride, "type", "", "override the workflow type inferred from the path")
	return cmd
}

func runRepair(ctx context.Context, cmd *cobra.Command, target, typeOverride string, dryRun, jsonOut bool) error {
	cfg, root, err := config.LoadCampaignConfigFromCwd(ctx)
	if err != nil {
		return camperrors.Wrap(err, "not in a campaign directory")
	}
	resolver := paths.NewResolverFromConfig(root, cfg)

	relPath, pathType, err := parseWorkflowTarget(root, resolver, target)
	if err != nil {
		return err
	}
	if typeOverride != "" {
		if err := validateSlug(typeOverride); err != nil {
			return camperrors.NewValidation("type", "invalid --type slug: "+err.Error(), nil)
		}
		pathType = typeOverride
	}

	absDir := filepath.Join(root, filepath.FromSlash(relPath))
	info, err := os.Stat(absDir)
	if err != nil {
		return camperrors.Wrap(err, "stat target directory")
	}
	if !info.IsDir() {
		return camperrors.NewValidation("path", "target must be a directory: "+relPath, nil)
	}

	plan, err := planRepair(ctx, root, cfg, relPath, pathType)
	if err != nil {
		return err
	}

	if !dryRun && len(plan.changes) > 0 {
		if err := writeMarker(absDir, plan.meta); err != nil {
			return err
		}
		invalidateNavigationCache(cmd, root)
	}

	if jsonOut {
		return emitRepairJSON(cmd.OutOrStdout(), relPath, dryRun, plan)
	}
	return emitRepairHuman(cmd.OutOrStdout(), relPath, dryRun, plan)
}

// planRepair reads the current marker (if any) and computes the canonical marker
// plus the changes needed to reach it. Filesystem-backed id/ref/title generators
// are injected into the pure computeRepair so its decision logic stays testable.
func planRepair(ctx context.Context, root string, cfg *config.CampaignConfig, relPath, pathType string) (markerRepair, error) {
	markerAbs := filepath.Join(root, filepath.FromSlash(relPath), wkitem.MetadataFilename)
	absDir := filepath.Dir(markerAbs)
	slug := path.Base(relPath)

	raw, err := os.ReadFile(markerAbs)
	exists := true
	var current wkitem.Metadata
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			return markerRepair{}, camperrors.Wrapf(err, "read %s", markerAbs)
		}
		exists = false
	} else if uerr := yaml.Unmarshal(raw, &current); uerr != nil {
		return markerRepair{}, camperrors.NewValidation("marker",
			"cannot parse existing .workitem as YAML ("+uerr.Error()+"); fix or remove it manually before repairing", nil)
	}

	genID := func() (string, error) { return generateID(ctx, pathType, slug, "", root) }
	genRef := func(id string) (string, error) { return deriveUniqueRef(ctx, root, cfg, id) }
	inferTitle := func() string { return wkitem.InferTitle(absDir) }

	return computeRepair(current, exists, pathType, genID, genRef, inferTitle)
}

// computeRepair is the pure repair planner. Given the current parsed marker
// (zero value when creating), the authoritative path type, and generators for
// the id, ref, and title, it returns the canonical marker and the ordered
// changes. Generators run only when a value must be produced.
func computeRepair(
	current wkitem.Metadata,
	exists bool,
	pathType string,
	genID func() (string, error),
	genRef func(id string) (string, error),
	inferTitle func() string,
) (markerRepair, error) {
	meta := current
	var changes []repairChange
	set := func(field, from, to string) {
		changes = append(changes, repairChange{Field: field, Action: repairActionSet, From: from, To: to})
	}

	if !wkitem.IsCurrentVersion(meta.Version) {
		from := meta.Version
		meta.Version = wkitem.WorkitemSchemaVersion
		set("version", from, meta.Version)
	}
	if meta.Kind != wkitem.MetadataKind {
		from := meta.Kind
		meta.Kind = wkitem.MetadataKind
		set("kind", from, meta.Kind)
	}
	if meta.ID == "" {
		id, err := genID()
		if err != nil {
			return markerRepair{}, err
		}
		meta.ID = id
		set("id", "", id)
	}
	if meta.Type != pathType {
		from := meta.Type
		meta.Type = pathType
		set("type", from, pathType)
	}
	if meta.QuestID != "" && !wkitem.ValidQuestID(meta.QuestID) {
		from := meta.QuestID
		meta.QuestID = ""
		changes = append(changes, repairChange{Field: "quest_id", Action: repairActionCleared, From: from})
	}
	if meta.Ref == "" || !wkitem.ValidRef(meta.Ref) {
		from := meta.Ref
		ref, err := genRef(meta.ID)
		if err != nil {
			return markerRepair{}, err
		}
		meta.Ref = ref
		set("ref", from, ref)
	}
	if meta.Title == "" {
		if title := inferTitle(); title != "" {
			meta.Title = title
			set("title", "", title)
		}
	}

	return markerRepair{meta: meta, changes: changes, created: !exists}, nil
}

func writeMarker(absDir string, meta wkitem.Metadata) error {
	buf, err := yaml.Marshal(&meta)
	if err != nil {
		return camperrors.Wrap(err, "marshal marker")
	}
	return fsutil.WriteFileAtomically(filepath.Join(absDir, wkitem.MetadataFilename), buf, 0o644)
}

func emitRepairHuman(w io.Writer, relPath string, dryRun bool, plan markerRepair) error {
	verb := "repaired"
	if dryRun {
		verb = "would repair"
	}
	switch {
	case plan.created:
		verb = "created marker for"
		if dryRun {
			verb = "would create marker for"
		}
	case len(plan.changes) == 0:
		_, err := w.Write([]byte(relPath + " is already valid; no changes\n"))
		return err
	}

	header := verb + " " + relPath + "\n" +
		"  id: " + plan.meta.ID + "\n" +
		"  ref: " + plan.meta.Ref + "\n" +
		"  type: " + plan.meta.Type + "\n"
	if plan.meta.Title != "" {
		header += "  title: " + plan.meta.Title + "\n"
	}
	if _, err := w.Write([]byte(header)); err != nil {
		return err
	}
	for _, c := range plan.changes {
		line := "  - " + c.Field + " " + c.Action
		if c.Action == repairActionSet {
			line += ": " + quoteOrEmpty(c.From) + " -> " + quoteOrEmpty(c.To)
		} else if c.From != "" {
			line += ": " + quoteOrEmpty(c.From)
		}
		if _, err := w.Write([]byte(line + "\n")); err != nil {
			return err
		}
	}
	return nil
}

func emitRepairJSON(w io.Writer, relPath string, dryRun bool, plan markerRepair) error {
	changes := plan.changes
	if changes == nil {
		changes = []repairChange{}
	}
	out := struct {
		SchemaVersion string    `json:"schema_version"`
		GeneratedAt   time.Time `json:"generated_at"`
		Target        string    `json:"target"`
		DryRun        bool      `json:"dry_run"`
		CreatedMarker bool      `json:"created_marker"`
		Changed       bool      `json:"changed"`
		Changes       []repairChange `json:"changes"`
		Workitem      struct {
			ID            string `json:"id"`
			Ref           string `json:"ref"`
			Type          string `json:"type"`
			Title         string `json:"title,omitempty"`
			RelativePath  string `json:"relative_path"`
			MarkerVersion string `json:"marker_version"`
		} `json:"workitem"`
	}{
		SchemaVersion: WorkitemRepairJSONVersion,
		GeneratedAt:   time.Now().UTC(),
		Target:        relPath,
		DryRun:        dryRun,
		CreatedMarker: plan.created,
		Changed:       len(plan.changes) > 0,
		Changes:       changes,
	}
	out.Workitem.ID = plan.meta.ID
	out.Workitem.Ref = plan.meta.Ref
	out.Workitem.Type = plan.meta.Type
	out.Workitem.Title = plan.meta.Title
	out.Workitem.RelativePath = relPath
	out.Workitem.MarkerVersion = plan.meta.Version

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

func quoteOrEmpty(s string) string {
	if s == "" {
		return `""`
	}
	return strconv.Quote(s)
}
