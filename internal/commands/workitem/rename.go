package workitem

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/jsoncontract"
	"github.com/Obedience-Corp/camp/internal/pathutil"
	wkitem "github.com/Obedience-Corp/camp/internal/workitem"
)

type runWorkitemRenameOptions struct {
	Selector string
	NewName  string
	DryRun   bool
	NoCommit bool
	JSON     bool
}

type workitemRenameResult struct {
	ID             string   `json:"id"`
	Key            string   `json:"key"`
	Type           string   `json:"type"`
	ItemKind       string   `json:"item_kind"`
	From           string   `json:"from"`
	To             string   `json:"to"`
	Committed      bool     `json:"committed"`
	CommitMessage  string   `json:"commit_message,omitempty"`
	LinksUpdated   bool     `json:"links_updated"`
	PriorityMoved  bool     `json:"priority_migrated"`
	CurrentUpdated bool     `json:"current_updated"`
	RewrittenFiles []string `json:"rewritten_files,omitempty"`
	Warnings       []string `json:"warnings,omitempty"`
}

// renamePlan is the validated, side-effect-free description of a rename,
// computed before any filesystem mutation runs.
type renamePlan struct {
	item    *wkitem.WorkItem
	srcPath string
	dstPath string
	oldRel  string
	newRel  string
	oldKey  string
	newKey  string
	isFile  bool
}

func newRenameCommand() *cobra.Command {
	var (
		dryRun   bool
		noCommit bool
		jsonOut  bool
	)

	cmd := &cobra.Command{
		Use:   "rename <selector> <new-name>",
		Short: "Rename a workitem and repair references",
		Long: `Rename the workitem matched by <selector> so its directory (or file)
basename becomes <new-name>. Identity is preserved: the stable id, ref, title,
type, and lifecycle status do not change; only the path basename moves.

References are repaired in the same commit as the move:
  - relative markdown links pointing at the workitem are rewritten
  - the workitem link registry (links.yaml) key and any scope paths under the
    renamed directory are updated
  - manual priority and attention-stage entries are re-keyed on disk
  - the current-workitem pointer is updated when it referenced the old path key

Festivals and intents are managed by their own tooling and cannot be renamed
here. For file workitems, pass the full new filename; the original extension is
kept when omitted.`,
		Args: jsoncontract.Args(WorkitemRenameJSONVersion, func() bool { return jsonOut }, cobra.ExactArgs(2)),
		Annotations: map[string]string{
			"agent_allowed": "true",
			"agent_reason":  "Fully specified by positional args and flags; no interactive prompts",
		},
		RunE: jsoncontract.RunE(WorkitemRenameJSONVersion, func() bool { return jsonOut }, func(cmd *cobra.Command, args []string) error {
			return runWorkitemRename(cmd, runWorkitemRenameOptions{
				Selector: args[0],
				NewName:  args[1],
				DryRun:   dryRun,
				NoCommit: noCommit,
				JSON:     jsonOut,
			})
		}),
	}
	cmd.SetFlagErrorFunc(jsoncontract.FlagErrorFunc(WorkitemRenameJSONVersion, func() bool { return jsonOut }))

	f := cmd.Flags()
	f.BoolVar(&dryRun, "dry-run", false, "Print the planned rename, change nothing")
	f.BoolVar(&noCommit, "no-commit", false, "Skip the auto-commit")
	f.BoolVar(&jsonOut, "json", false, "Output result as a single JSON object")
	return cmd
}

func runWorkitemRename(cmd *cobra.Command, opts runWorkitemRenameOptions) error {
	ctx := cmd.Context()

	cfg, root, err := config.LoadCampaignConfigFromCwd(ctx)
	if err != nil {
		return camperrors.Wrap(err, "not in a campaign directory")
	}

	plan, err := planRename(ctx, root, opts)
	if err != nil {
		return err
	}

	result := workitemRenameResult{
		ID:       plan.item.StableID,
		Key:      plan.newKey,
		Type:     string(plan.item.WorkflowType),
		ItemKind: string(plan.item.ItemKind),
		From:     filepath.ToSlash(plan.oldRel),
		To:       filepath.ToSlash(plan.newRel),
	}

	if opts.DryRun {
		if opts.JSON {
			return emitRenameJSON(cmd.OutOrStdout(), result)
		}
		_, err := fmt.Fprintf(cmd.OutOrStdout(),
			"dry-run: would rename workitem %s (%s) to %s\n",
			filepath.Base(plan.oldRel), plan.item.ItemKind, filepath.ToSlash(plan.newRel))
		return err
	}

	return applyRename(ctx, cmd, cfg, root, plan, opts, result)
}

// planRename resolves the selector, validates the new name, and computes the
// source/destination paths and keys without touching the filesystem.
func planRename(ctx context.Context, root string, opts runWorkitemRenameOptions) (*renamePlan, error) {
	if err := pathutil.ValidateSegment("new-name", opts.NewName); err != nil {
		return nil, err
	}

	item, err := resolveSelector(ctx, root, opts.Selector, false)
	if err != nil {
		return nil, err
	}
	if item.RelativePath == "" {
		return nil, camperrors.New("resolved workitem has no path on disk")
	}
	if err := ensureRenamable(item); err != nil {
		return nil, err
	}

	oldRel := filepath.Clean(item.RelativePath)
	isFile := item.ItemKind == wkitem.ItemKindFile
	newBase, err := renameBasename(opts.NewName, oldRel, isFile)
	if err != nil {
		return nil, err
	}
	newRel := filepath.Join(filepath.Dir(oldRel), newBase)
	if newRel == oldRel {
		return nil, camperrors.NewValidation("new-name",
			"new name is identical to the current name: "+newBase, nil)
	}

	newKey, err := renameKey(item.Key, oldRel, newRel)
	if err != nil {
		return nil, err
	}

	srcPath := filepath.Join(root, oldRel)
	dstPath := filepath.Join(root, newRel)
	if _, statErr := os.Lstat(dstPath); statErr == nil {
		return nil, camperrors.Wrap(camperrors.ErrAlreadyExists,
			"a sibling named "+newBase+" already exists at "+filepath.ToSlash(newRel))
	} else if !os.IsNotExist(statErr) {
		return nil, camperrors.Wrapf(statErr, "checking destination %s", newRel)
	}

	return &renamePlan{
		item:    item,
		srcPath: srcPath,
		dstPath: dstPath,
		oldRel:  oldRel,
		newRel:  newRel,
		oldKey:  item.Key,
		newKey:  newKey,
		isFile:  isFile,
	}, nil
}

// ensureRenamable rejects workitem kinds whose identity is owned by other
// tooling. Festivals are managed by the fest CLI and intents carry their own
// title-based rename; renaming their directories here would desync that state.
func ensureRenamable(item *wkitem.WorkItem) error {
	switch item.WorkflowType {
	case wkitem.WorkflowTypeFestival:
		return camperrors.New("cannot rename a festival with camp workitem rename; festival identity is managed by the fest CLI")
	case wkitem.WorkflowTypeIntent:
		return camperrors.New("cannot rename an intent with camp workitem rename; intents are renamed by title through the intent tooling")
	default:
		return nil
	}
}

// renameBasename returns the destination basename. For a directory the new name
// is used verbatim; for a file the original extension is preserved (appended
// when the new name omits it) and a conflicting extension is rejected.
func renameBasename(newName, oldRel string, isFile bool) (string, error) {
	if !isFile {
		return newName, nil
	}
	origExt := filepath.Ext(oldRel)
	if origExt == "" {
		return newName, nil
	}
	switch filepath.Ext(newName) {
	case "":
		return newName + origExt, nil
	case origExt:
		return newName, nil
	default:
		return "", camperrors.NewValidation("new-name",
			"file workitem extension must stay "+origExt+"; rename to a name ending in "+origExt, nil)
	}
}

// renameKey rebuilds the path-derived workitem key for the new location. The
// key is always "<prefix>:" + RelativePath, so swapping the trailing path
// yields the key discovery will produce after the move.
func renameKey(oldKey, oldRel, newRel string) (string, error) {
	if !strings.HasSuffix(oldKey, oldRel) {
		return "", camperrors.New("cannot derive new key: workitem key " + oldKey + " does not end with its path " + oldRel)
	}
	return oldKey[:len(oldKey)-len(oldRel)] + newRel, nil
}

func emitRenameJSON(w io.Writer, result workitemRenameResult) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(struct {
		SchemaVersion string               `json:"schema_version"`
		GeneratedAt   time.Time            `json:"generated_at"`
		Workitem      workitemRenameResult `json:"workitem"`
	}{
		SchemaVersion: WorkitemRenameJSONVersion,
		GeneratedAt:   time.Now().UTC(),
		Workitem:      result,
	}); err != nil {
		return camperrors.Wrap(err, "encoding JSON output")
	}
	return nil
}
