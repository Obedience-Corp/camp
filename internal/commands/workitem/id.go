package workitem

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/jsoncontract"
	wkitem "github.com/Obedience-Corp/camp/internal/workitem"
	"github.com/Obedience-Corp/camp/internal/workitem/resolver"
)

// selectorCompleteTimeout bounds workitem discovery on the shell completion
// path so a large campaign cannot stall a keystroke.
const selectorCompleteTimeout = 200 * time.Millisecond

type idOptions struct {
	JSON bool
	Key  bool
}

func newIDCommand() *cobra.Command {
	var opts idOptions
	cmd := &cobra.Command{
		Use:   "id [selector-or-path]",
		Short: "Print the identifier of a workitem",
		Long: `Print the stable identifier of a workitem.

With no argument, the workitem is detected from the current context using the
same tiered resolution as ` + "`camp workitem resolve`" + ` (explicit selector, cwd
ancestor, linked scope, festival, current-workitem pointer). With an argument,
the workitem is resolved through the shared selector family: workitem ref,
stable id, key, campaign-relative path, directory slug, or festival id. A
filesystem path (absolute or relative to the current directory) is accepted and
translated to the campaign-relative form the selector expects.

The bare stable id is written to stdout for shell scripting; it is the id sibling
of ` + "`camp workitem --print`" + `, which prints a path. Use --key for the
path-derived key instead, or --json for a structured object.

Examples:
  camp workitem id                       # id of the workitem for the cwd
  camp workitem id design-x-2026-05-24   # echoes back the stable id
  camp workitem id ./workflow/design/x   # resolves a filesystem path
  camp workitem id --key                 # print the <type>:<path> key form
  camp workitem id --json SC0001         # structured object for a festival`,
		Args:              jsoncontract.Args(WorkitemIDJSONVersion, func() bool { return opts.JSON }, cobra.MaximumNArgs(1)),
		ValidArgsFunction: completeWorkitemSelector,
		Annotations: map[string]string{
			"agent_allowed": "true",
			"agent_reason":  "Read-only identifier lookup with a bare-id stdout contract and --json for automation",
		},
		RunE: jsoncontract.RunE(WorkitemIDJSONVersion, func() bool { return opts.JSON }, func(cmd *cobra.Command, args []string) error {
			return runID(cmd.Context(), cmd, args, opts)
		}),
	}
	cmd.SetFlagErrorFunc(jsoncontract.FlagErrorFunc(WorkitemIDJSONVersion, func() bool { return opts.JSON }))
	cmd.Flags().BoolVar(&opts.JSON, "json", false, "emit a structured JSON result")
	cmd.Flags().BoolVar(&opts.Key, "key", false, "print the path-derived key instead of the stable id")
	return cmd
}

func runID(ctx context.Context, cmd *cobra.Command, args []string, opts idOptions) error {
	_, root, err := config.LoadCampaignConfigFromCwd(ctx)
	if err != nil {
		return camperrors.Wrap(err, "not in a campaign directory")
	}
	resolveOpts := resolver.Options{}
	if len(args) == 1 {
		selectorArg, serr := selectorFromArg(root, args[0])
		if serr != nil {
			return serr
		}
		resolveOpts.Explicit = selectorArg
	}
	res, err := resolver.Resolve(ctx, root, resolveOpts)
	if err != nil {
		return err
	}
	if res.Workitem == nil {
		return camperrors.NewValidation("workitem",
			"no workitem in the current context; pass a selector or run inside a workitem directory", nil)
	}
	id, kind := durableID(res.Workitem)
	if opts.JSON {
		return emitIDJSON(cmd.OutOrStdout(), res, id, kind)
	}
	out := id
	if opts.Key {
		out = res.Workitem.Key
	}
	_, err = fmt.Fprintln(cmd.OutOrStdout(), out)
	return err
}

// idKind records which identifier form durableID selected, so --json can flag
// when an item without a stable id falls back to its key.
type idKind string

const (
	idKindStable   idKind = "stable"
	idKindFestival idKind = "festival"
	idKindKey      idKind = "key"
)

// durableID returns the single-segment identifier the selector can resolve back
// to wi, plus which form it is. The value is delegated to LinkWorkitemID so the
// id printed here and the id stored on a link cannot drift; the kind is derived
// from the same precedence (stable .workitem id, then a festival's fest.yaml id,
// then the path-derived key fallback).
func durableID(wi *wkitem.WorkItem) (string, idKind) {
	id := wkitem.LinkWorkitemID(wi)
	switch {
	case wi.StableID != "" && id == wi.StableID:
		return id, idKindStable
	case wi.WorkflowType == wkitem.WorkflowTypeFestival && wi.SourceID != "" && id == wi.SourceID:
		return id, idKindFestival
	default:
		return id, idKindKey
	}
}

// selectorFromArg translates a positional argument into a selector string. An
// argument that names an existing filesystem path (absolute or relative to the
// cwd) is rewritten to the campaign-relative form the selector's path matcher
// expects; anything else is passed through unchanged as an id/key/slug/festival
// selector. Both sides are canonicalized the same way resolver.Resolve
// canonicalizes its root so a symlinked campaign resolves consistently.
func selectorFromArg(root, arg string) (string, error) {
	trimmed := strings.TrimSpace(arg)
	if trimmed == "" {
		return "", camperrors.NewValidation("selector", "selector must not be empty", nil)
	}
	abs := trimmed
	if !filepath.IsAbs(abs) {
		cwd, err := os.Getwd()
		if err != nil {
			return "", camperrors.Wrap(err, "get cwd")
		}
		abs = filepath.Join(cwd, abs)
	}
	if _, err := os.Stat(abs); err != nil {
		return trimmed, nil
	}
	rel, err := filepath.Rel(canonicalPath(root), canonicalPath(abs))
	if err != nil || escapesRoot(rel) {
		return trimmed, nil
	}
	return filepath.ToSlash(strings.TrimRight(rel, "/")), nil
}

func canonicalPath(p string) string {
	if abs, err := filepath.Abs(p); err == nil {
		p = abs
	}
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		return resolved
	}
	return p
}

func escapesRoot(rel string) bool {
	return rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel)
}

func emitIDJSON(w io.Writer, res *resolver.Resolution, id string, kind idKind) error {
	wi := res.Workitem
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(struct {
		SchemaVersion string          `json:"schema_version"`
		GeneratedAt   time.Time       `json:"generated_at"`
		ID            string          `json:"id"`
		IDKind        idKind          `json:"id_kind"`
		Key           string          `json:"key"`
		StableID      string          `json:"stable_id,omitempty"`
		Source        resolver.Source `json:"source"`
		RelativePath  string          `json:"relative_path"`
	}{
		SchemaVersion: WorkitemIDJSONVersion,
		GeneratedAt:   time.Now().UTC(),
		ID:            id,
		IDKind:        kind,
		Key:           wi.Key,
		StableID:      wi.StableID,
		Source:        res.Source,
		RelativePath:  wi.RelativePath,
	})
}

func completeWorkitemSelector(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	ctx, cancel := context.WithTimeout(cmd.Context(), selectorCompleteTimeout)
	defer cancel()
	state, err := discoverWorkitems(ctx)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return workitemSelectorCandidates(state.items, toComplete), cobra.ShellCompDirectiveNoFileComp
}

// workitemSelectorCandidates builds the completion list from the same gather
// that serves `camp workitem --json`: one durable id per item (which covers
// festival ids), prefix-filtered, deduplicated, sorted, and annotated with the
// item title as a cobra completion description.
func workitemSelectorCandidates(items []wkitem.WorkItem, toComplete string) []string {
	seen := make(map[string]bool, len(items))
	out := make([]string, 0, len(items))
	for i := range items {
		id, _ := durableID(&items[i])
		if id == "" || seen[id] {
			continue
		}
		if toComplete != "" && !strings.HasPrefix(id, toComplete) {
			continue
		}
		seen[id] = true
		if title := strings.TrimSpace(items[i].Title); title != "" {
			out = append(out, id+"\t"+title)
			continue
		}
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}
