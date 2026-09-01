package workitem

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/jsoncontract"
	"github.com/Obedience-Corp/camp/internal/paths"
	wkitem "github.com/Obedience-Corp/camp/internal/workitem"
)

type runWorkitemConvertOptions struct {
	Selector string
	NewType  string
	DryRun   bool
	NoCommit bool
	JSON     bool
}

type workitemConvertResult struct {
	ID             string   `json:"id"`
	Key            string   `json:"key"`
	FromType       string   `json:"from_type"`
	ToType         string   `json:"to_type"`
	ItemKind       string   `json:"item_kind"`
	From           string   `json:"from"`
	To             string   `json:"to"`
	Committed      bool     `json:"committed"`
	CommitMessage  string   `json:"commit_message,omitempty"`
	LinksUpdated   bool     `json:"links_updated"`
	PriorityMoved  bool     `json:"priority_migrated"`
	MarkerUpdated  bool     `json:"marker_updated"`
	RewrittenFiles []string `json:"rewritten_files,omitempty"`
	Warnings       []string `json:"warnings,omitempty"`
}

// convertPlan is the validated, side-effect-free description of a type-root
// conversion, computed before any filesystem mutation runs.
type convertPlan struct {
	item    *wkitem.WorkItem
	srcPath string
	dstPath string
	oldRel  string
	newRel  string
	oldKey  string
	newKey  string
	oldType string
	newType string
	isFile  bool
}

func (p *convertPlan) asRenamePlan() *renamePlan {
	return &renamePlan{
		item:    p.item,
		srcPath: p.srcPath,
		dstPath: p.dstPath,
		oldRel:  p.oldRel,
		newRel:  p.newRel,
		oldKey:  p.oldKey,
		newKey:  p.newKey,
		isFile:  p.isFile,
	}
}

func newConvertCommand() *cobra.Command {
	var (
		newType  string
		dryRun   bool
		noCommit bool
		jsonOut  bool
	)

	cmd := &cobra.Command{
		Use:     "convert <selector>",
		Aliases: []string{"retype"},
		Short:   "Move a workitem to another workflow type",
		Long: `Convert the workitem matched by <selector> from its current workflow
type root to --type, keeping the same basename. Identity is preserved: the
stable id, ref, and title do not change. The directory (or file) moves from
workflow/<old-type>/<name> to workflow/<new-type>/<name>, and the marker
(or file frontmatter) type field is updated to match.

References are repaired in the same commit as the move, using the same
rewrites as camp workitem rename:
  - relative markdown links pointing at the workitem are rewritten
  - the workitem link registry (links.yaml) key and any scope paths under the
    moved directory are updated
  - manual priority and attention-stage entries are re-keyed on disk

Festivals and intents are managed by their own tooling and cannot be converted
here. Workitems already in a dungeon cannot be converted; restore them first.
This is not a rename: the basename stays put. Use camp workitem rename to
change the name without changing type.`,
		Example: `  camp workitem convert camp-triage --type design
  camp workitem convert WI-36e2a6 --type design --json
  camp workitem convert explore-notes --type design --dry-run`,
		Args: jsoncontract.Args(WorkitemConvertJSONVersion, func() bool { return jsonOut }, cobra.ExactArgs(1)),
		Annotations: map[string]string{
			"agent_allowed": "true",
			"agent_reason":  "Fully specified by positional args and flags; no interactive prompts",
		},
		RunE: jsoncontract.RunE(WorkitemConvertJSONVersion, func() bool { return jsonOut }, func(cmd *cobra.Command, args []string) error {
			return runWorkitemConvert(cmd, runWorkitemConvertOptions{
				Selector: args[0],
				NewType:  newType,
				DryRun:   dryRun,
				NoCommit: noCommit,
				JSON:     jsonOut,
			})
		}),
	}
	cmd.SetFlagErrorFunc(jsoncontract.FlagErrorFunc(WorkitemConvertJSONVersion, func() bool { return jsonOut }))

	f := cmd.Flags()
	f.StringVar(&newType, "type", "", "Destination workflow type")
	f.BoolVar(&dryRun, "dry-run", false, "Print the planned conversion, change nothing")
	f.BoolVar(&noCommit, "no-commit", false, "Skip the auto-commit")
	f.BoolVar(&jsonOut, "json", false, "Output result as a single JSON object")
	_ = cmd.MarkFlagRequired("type")
	return cmd
}

func runWorkitemConvert(cmd *cobra.Command, opts runWorkitemConvertOptions) error {
	ctx := cmd.Context()

	cfg, root, err := config.LoadCampaignConfigFromCwd(ctx)
	if err != nil {
		return camperrors.Wrap(err, "not in a camp directory")
	}

	plan, err := planConvert(ctx, root, cfg, opts)
	if err != nil {
		return err
	}

	result := workitemConvertResult{
		ID:       plan.item.StableID,
		Key:      plan.newKey,
		FromType: plan.oldType,
		ToType:   plan.newType,
		ItemKind: string(plan.item.ItemKind),
		From:     filepath.ToSlash(plan.oldRel),
		To:       filepath.ToSlash(plan.newRel),
	}

	if opts.DryRun {
		if opts.JSON {
			return emitConvertJSON(cmd.OutOrStdout(), result)
		}
		_, err := fmt.Fprintf(cmd.OutOrStdout(),
			"dry-run: would convert workitem %s (%s) from %s to %s at %s\n",
			filepath.Base(plan.oldRel), plan.item.ItemKind, plan.oldType, plan.newType,
			filepath.ToSlash(plan.newRel))
		return err
	}

	return applyConvert(ctx, cmd, cfg, root, plan, opts, result)
}

func planConvert(ctx context.Context, root string, cfg *config.CampaignConfig, opts runWorkitemConvertOptions) (*convertPlan, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := validateSlug(opts.NewType); err != nil {
		return nil, camperrors.NewValidation("type", "invalid --type slug: "+err.Error(), nil)
	}
	if !isWorkflowWorkitemType(opts.NewType) {
		return nil, camperrors.NewValidation("type",
			"--type must name a workflow document or custom type; got "+strconv.Quote(opts.NewType), nil)
	}

	item, err := resolveSelector(ctx, root, opts.Selector, false)
	if err != nil {
		return nil, err
	}
	if item.RelativePath == "" {
		return nil, camperrors.New("resolved workitem has no path on disk")
	}
	if err := ensureConvertible(item); err != nil {
		return nil, err
	}

	resolver := paths.NewResolverFromConfig(root, cfg)
	oldRel := filepath.Clean(item.RelativePath)
	newRelSlash, oldType, err := convertRelPath(oldRel, resolver.RelativeWorkflow(), opts.NewType)
	if err != nil {
		return nil, err
	}
	if oldType == opts.NewType {
		return nil, camperrors.NewValidation("type",
			"workitem is already type "+opts.NewType+" at "+filepath.ToSlash(oldRel), nil)
	}

	newRel := filepath.FromSlash(newRelSlash)
	newKey, err := convertKey(item.Key, oldRel, newRel, opts.NewType, item.ItemKind == wkitem.ItemKindFile)
	if err != nil {
		return nil, err
	}

	srcPath := filepath.Join(root, oldRel)
	dstPath := filepath.Join(root, newRel)
	if _, statErr := os.Lstat(dstPath); statErr == nil {
		return nil, camperrors.Wrap(camperrors.ErrAlreadyExists,
			"a workitem already exists at "+filepath.ToSlash(newRel))
	} else if !os.IsNotExist(statErr) {
		return nil, camperrors.Wrapf(statErr, "checking destination %s", newRel)
	}

	return &convertPlan{
		item:    item,
		srcPath: srcPath,
		dstPath: dstPath,
		oldRel:  oldRel,
		newRel:  newRel,
		oldKey:  item.Key,
		newKey:  newKey,
		oldType: oldType,
		newType: opts.NewType,
		isFile:  item.ItemKind == wkitem.ItemKindFile,
	}, nil
}

// ensureConvertible rejects workitem kinds whose identity is owned by other
// tooling and workitems that have already left the live type root.
func ensureConvertible(item *wkitem.WorkItem) error {
	switch item.WorkflowType {
	case wkitem.WorkflowTypeFestival:
		return camperrors.New("cannot convert a festival with camp workitem convert; festival identity is managed by the fest CLI")
	case wkitem.WorkflowTypeIntent:
		return camperrors.New("cannot convert an intent with camp workitem convert; intents are converted by type through the intent tooling")
	}
	if wkitem.InDungeonPath(item.RelativePath) {
		return camperrors.NewValidation("selector",
			"cannot convert a workitem that is already in a dungeon: "+item.RelativePath, nil)
	}
	return nil
}

// convertRelPath swaps the workflow type segment of a campaign-relative path.
// oldRel must be workflowRel/<type>/<name>[/...].
func convertRelPath(oldRel, workflowRel, newType string) (newRel, oldType string, err error) {
	oldSlash := filepath.ToSlash(filepath.Clean(oldRel))
	prefix := filepath.ToSlash(filepath.Clean(workflowRel)) + "/"
	if !strings.HasPrefix(oldSlash, prefix) {
		return "", "", camperrors.NewValidation("selector",
			"cannot convert a workitem that is not under "+prefix+"<type>/; got "+oldSlash, nil)
	}
	rest := strings.TrimPrefix(oldSlash, prefix)
	parts := strings.Split(rest, "/")
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return "", "", camperrors.NewValidation("selector",
			"expected "+prefix+"<type>/<name>; got "+oldSlash, nil)
	}
	oldType = parts[0]
	parts[0] = newType
	return prefix + strings.Join(parts, "/"), oldType, nil
}

// convertKey rebuilds the path-derived workitem key for the new type root.
// File workitems keep the "file:" prefix; directory keys take the new type.
func convertKey(oldKey, oldRel, newRel, newType string, isFile bool) (string, error) {
	oldRel = filepath.ToSlash(oldRel)
	newRel = filepath.ToSlash(newRel)
	if !strings.HasSuffix(oldKey, oldRel) {
		return "", camperrors.New("cannot derive new key: workitem key " + oldKey + " does not end with its path " + oldRel)
	}
	if isFile {
		return oldKey[:len(oldKey)-len(oldRel)] + newRel, nil
	}
	return newType + ":" + newRel, nil
}

func emitConvertJSON(w io.Writer, result workitemConvertResult) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(struct {
		SchemaVersion string                `json:"schema_version"`
		GeneratedAt   time.Time             `json:"generated_at"`
		Workitem      workitemConvertResult `json:"workitem"`
	}{
		SchemaVersion: WorkitemConvertJSONVersion,
		GeneratedAt:   time.Now().UTC(),
		Workitem:      result,
	}); err != nil {
		return camperrors.Wrap(err, "encoding JSON output")
	}
	return nil
}
