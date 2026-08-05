package workitem

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/fsutil"
	"github.com/Obedience-Corp/camp/internal/paths"
	wkitem "github.com/Obedience-Corp/camp/internal/workitem"
	"github.com/Obedience-Corp/camp/internal/workitem/selector"
)

// WorkitemSplitJSONVersion is the schema version of `camp workitem split --json`.
const WorkitemSplitJSONVersion = "workitem-split/v1alpha1"

type splitResult struct {
	SchemaVersion string `json:"schema_version"`
	DryRun        bool   `json:"dry_run"`
	Parent        struct {
		StableID     string `json:"stable_id"`
		Ref          string `json:"ref,omitempty"`
		Type         string `json:"type"`
		Title        string `json:"title,omitempty"`
		RelativePath string `json:"relative_path"`
	} `json:"parent"`
	Successors []splitSuccessorJSON `json:"successors"`
	SplitAt    string               `json:"split_at"`
	// Gate is the retirement gate's state after this split.
	Gate      splitGateJSON `json:"gate"`
	Committed bool          `json:"committed"`
}

type splitSuccessorJSON struct {
	// Name is the successor as named on the command line. On a dry-run it is
	// all that is known: an id is generated at creation, and generating one
	// to preview it would consume it.
	Name string `json:"name"`
	// StableID is empty on a dry-run for exactly that reason.
	StableID     string `json:"stable_id"`
	Ref          string `json:"ref,omitempty"`
	Type         string `json:"type"`
	RelativePath string `json:"relative_path"`
	Created      bool   `json:"created"`
	Adopted      bool   `json:"adopted"`
}

type splitGateJSON struct {
	// Blocked reports whether the parent's terminal promotion is refused.
	Blocked bool     `json:"blocked"`
	Missing []string `json:"missing"`
}

func newSplitCommand() *cobra.Command {
	var (
		into     []string
		adopt    []string
		dryRun   bool
		jsonOut  bool
		noCommit bool
	)

	cmd := &cobra.Command{
		Use:   "split <selector>",
		Short: "Split a workitem into successors with lineage",
		Long: `Split an umbrella workitem into the focused successors that replace it.

A workitem that accumulated three years of scope is not one decision, and
retiring it whole loses the parts still live. Split names the successors,
creates or adopts them, and records the lineage in both directions so the trail
is readable from either end.

  --into <name>[:<type>]    create a successor under the type root
  --adopt <path>[:<type>]   declare an existing workitem or directory as one

Type defaults to the parent's. At least one successor is required.

No content is moved. Deciding which part of a parent's scope belongs in which
successor is judgment, and a tool that guessed would produce successors nobody
trusts. Each created successor gets a README seeded with the back-link and an
empty scope section for the author to fill.

Lineage is stamped into the markers, not links.yaml: that registry attaches
workitems to scopes, not to each other.

Splitting arms the retirement gate. The parent then refuses terminal promotion
until every declared successor exists, which is the successors-before-archive
rule made mechanical rather than remembered.`,
		Args: cobra.ExactArgs(1),
		Annotations: map[string]string{
			"agent_allowed": "false",
			"agent_reason":  "Creates workitems and arms a retirement gate; splits require recorded human approval (D5)",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSplit(cmd, splitOptions{
				selectorArg: args[0], into: into, adopt: adopt,
				dryRun: dryRun, jsonOut: jsonOut, noCommit: noCommit,
			})
		},
	}

	f := cmd.Flags()
	f.StringArrayVar(&into, "into", nil, "Create a successor: <name>[:<type>] (repeatable)")
	f.StringArrayVar(&adopt, "adopt", nil, "Declare an existing workitem or directory as a successor: <path>[:<type>] (repeatable)")
	f.BoolVar(&dryRun, "dry-run", false, "Print what the split would do, change nothing")
	f.BoolVar(&jsonOut, "json", false, "Output result as a single JSON object")
	f.BoolVar(&noCommit, "no-commit", false, "Skip the auto-commit")
	return cmd
}

type splitOptions struct {
	selectorArg string
	into        []string
	adopt       []string
	dryRun      bool
	jsonOut     bool
	noCommit    bool
}

func runSplit(cmd *cobra.Command, opts splitOptions) error {
	ctx := cmd.Context()

	cfg, campaignRoot, err := config.LoadCampaignConfigFromCwd(ctx)
	if err != nil {
		return camperrors.Wrap(err, "not in a campaign directory")
	}

	parent, err := selector.Resolve(ctx, campaignRoot, opts.selectorArg, selector.ResolveOptions{})
	if err != nil {
		return err
	}
	if wkitem.InDungeonPath(parent.RelativePath) {
		return camperrors.NewValidation("selector",
			"cannot split a workitem that is already in a dungeon: "+parent.RelativePath,
			camperrors.ErrInvalidInput)
	}

	intoSpecs, err := wkitem.ParseSplitSpecs(opts.into)
	if err != nil {
		return err
	}
	adoptSpecs, err := wkitem.ParseSplitSpecs(opts.adopt)
	if err != nil {
		return err
	}
	parentID := wkitem.StableIDOf(parent)
	if err := wkitem.ValidateSplitSuccessors(parentID, append(append([]wkitem.SplitSpec{}, intoSpecs...), adoptSpecs...)); err != nil {
		return err
	}

	at := time.Now().UTC()
	result := splitResult{
		SchemaVersion: WorkitemSplitJSONVersion,
		DryRun:        opts.dryRun,
		SplitAt:       at.Format(time.RFC3339),
	}
	result.Parent.StableID = parentID
	result.Parent.Ref, _ = parent.SourceMetadata["ref"].(string)
	result.Parent.Type = string(parent.WorkflowType)
	result.Parent.Title = parent.Title
	result.Parent.RelativePath = parent.RelativePath

	if opts.dryRun {
		result.Successors = plannedSuccessors(parent, intoSpecs, adoptSpecs)
		// Every planned successor is "missing" by definition on a dry-run:
		// nothing has been created. Named, not id'd, because the ids do not
		// exist yet.
		result.Gate = splitGateJSON{Blocked: true, Missing: namesOfPlanned(result.Successors)}
		if opts.jsonOut {
			return encodeSplitJSON(cmd, result)
		}
		return printSplitDryRun(cmd, result)
	}

	successors, err := performSplit(ctx, campaignRoot, cfg, parent, intoSpecs, adoptSpecs, at)
	if err != nil {
		// Successors created before the failure are named rather than left
		// for the operator to discover: a half-finished split is recoverable
		// only if you know what it made.
		return splitFailure(err, successors)
	}

	lineage := wkitem.SplitLineage{
		ParentStableID: parentID,
		ParentPath:     parent.RelativePath,
		SuccessorIDs:   idsOf(successors),
		At:             at,
	}
	if err := wkitem.RecordSplitLineage(ctx, campaignRoot, lineage, successors); err != nil {
		return splitFailure(err, successors)
	}

	for _, successor := range successors {
		result.Successors = append(result.Successors, splitSuccessorJSON{
			Name:     filepath.Base(successor.RelativePath),
			StableID: successor.StableID, Ref: successor.Ref, Type: successor.Type,
			RelativePath: successor.RelativePath,
			Created:      successor.Created, Adopted: successor.Adopted,
		})
	}
	// Every successor exists, since this call just made or adopted them, so
	// the gate is armed but satisfied. It becomes blocking only if one is
	// later removed.
	result.Gate = splitGateJSON{Blocked: false, Missing: []string{}}

	if !opts.noCommit {
		if err := commitSplit(ctx, cmd, cfg, campaignRoot, parent, successors); err != nil {
			return err
		}
		result.Committed = true
	}

	if opts.jsonOut {
		return encodeSplitJSON(cmd, result)
	}
	return printSplitResult(cmd, result)
}

// performSplit creates and adopts every successor, returning what it made so
// far even on failure.
func performSplit(
	ctx context.Context, campaignRoot string, cfg *config.CampaignConfig,
	parent *wkitem.WorkItem, intoSpecs, adoptSpecs []wkitem.SplitSpec, at time.Time,
) ([]wkitem.SplitSuccessor, error) {
	var successors []wkitem.SplitSuccessor
	parentRef, _ := parent.SourceMetadata["ref"].(string)

	for _, spec := range intoSpecs {
		if err := ctx.Err(); err != nil {
			return successors, err
		}
		successorType := spec.Type
		if successorType == "" {
			successorType = string(parent.WorkflowType)
		}
		created, err := CreateWorkitemDir(ctx, campaignRoot, cfg, CreateWorkitemRequest{
			Slug: spec.Value, Type: successorType, Title: spec.Value,
		})
		if err != nil {
			return successors, err
		}
		readme := wkitem.SplitReadme(spec.Value, parent.Title, parentRef, at)
		if err := fsutil.WriteFileAtomically(
			filepath.Join(created.AbsPath, "README.md"), readme, 0o644); err != nil {
			return successors, err
		}
		successors = append(successors, wkitem.SplitSuccessor{
			StableID: created.ID, Ref: created.Ref, Type: created.Type,
			RelativePath: created.RelativePath, Created: true,
		})
	}

	for _, spec := range adoptSpecs {
		if err := ctx.Err(); err != nil {
			return successors, err
		}
		successor, err := adoptSuccessor(ctx, campaignRoot, parent, spec)
		if err != nil {
			return successors, err
		}
		successors = append(successors, successor)
	}
	return successors, nil
}

// adoptSuccessor records an existing workitem as a successor, adopting the
// directory first when it is not one yet.
func adoptSuccessor(
	ctx context.Context, campaignRoot string, parent *wkitem.WorkItem, spec wkitem.SplitSpec,
) (wkitem.SplitSuccessor, error) {
	rel := filepath.ToSlash(filepath.Clean(spec.Value))
	abs := filepath.Join(campaignRoot, filepath.FromSlash(rel))
	info, err := os.Stat(abs)
	if err != nil {
		return wkitem.SplitSuccessor{}, camperrors.NewValidation("adopt",
			"no such path in the campaign: "+rel, camperrors.ErrInvalidInput)
	}
	if !info.IsDir() {
		return wkitem.SplitSuccessor{}, camperrors.NewValidation("adopt",
			"only a directory can be adopted as a successor: "+rel, camperrors.ErrInvalidInput)
	}

	// Already a workitem: record it as-is rather than re-adopting, which
	// would rewrite an id other things already reference.
	if meta, err := wkitem.LoadMetadata(ctx, abs); err == nil && meta != nil {
		return wkitem.SplitSuccessor{
			StableID: meta.ID, Ref: meta.Ref, Type: meta.Type, RelativePath: rel,
		}, nil
	}

	successorType := spec.Type
	if successorType == "" {
		successorType = string(parent.WorkflowType)
	}
	meta, err := wkitem.AdoptDirectory(ctx, campaignRoot, wkitem.AdoptRequest{
		RelPath: rel, Type: successorType,
	})
	if err != nil {
		return wkitem.SplitSuccessor{}, err
	}
	return wkitem.SplitSuccessor{
		StableID: meta.ID, Ref: meta.Ref, Type: meta.Type,
		RelativePath: rel, Adopted: true,
	}, nil
}

// commitSplit records every write in one commit: the successors, their
// lineage, and the parent's stamp belong to one decision.
func commitSplit(
	ctx context.Context, cmd *cobra.Command, cfg *config.CampaignConfig,
	campaignRoot string, parent *wkitem.WorkItem, successors []wkitem.SplitSuccessor,
) error {
	paths := []string{parent.RelativePath}
	for _, successor := range successors {
		paths = append(paths, successor.RelativePath)
	}
	result := &workitemPromoteResult{}
	return commitWorkitemMove(ctx, cmd, cfg, campaignRoot, &commitInputs{
		description: "split " + wkitem.StableIDOf(parent) + " into " +
			joinIDs(idsOf(successors)),
		destPaths: paths,
	}, result, false)
}

// splitFailure reports a failure with whatever the split already created.
func splitFailure(err error, successors []wkitem.SplitSuccessor) error {
	if len(successors) == 0 {
		return err
	}
	return camperrors.Wrapf(err,
		"split stopped after creating %s (they were left in place, not rolled back)",
		joinIDs(idsOf(successors)))
}

// idsOf lists successors by stable id.
func idsOf(successors []wkitem.SplitSuccessor) []string {
	out := make([]string, 0, len(successors))
	for _, successor := range successors {
		out = append(out, successor.StableID)
	}
	return out
}

// namesOfPlanned lists planned successors by the name they were given.
func namesOfPlanned(successors []splitSuccessorJSON) []string {
	out := make([]string, 0, len(successors))
	for _, successor := range successors {
		out = append(out, successor.Name)
	}
	return out
}

// joinIDs renders an id list for a message.
func joinIDs(ids []string) string {
	out := ""
	for i, id := range ids {
		if i > 0 {
			out += ", "
		}
		out += id
	}
	return out
}

// plannedSuccessors describes what a dry-run would produce. Ids are not
// generated, because generating one would consume it.
func plannedSuccessors(parent *wkitem.WorkItem, intoSpecs, adoptSpecs []wkitem.SplitSpec) []splitSuccessorJSON {
	var out []splitSuccessorJSON
	for _, spec := range intoSpecs {
		successorType := spec.Type
		if successorType == "" {
			successorType = string(parent.WorkflowType)
		}
		out = append(out, splitSuccessorJSON{
			Name: spec.Value, Type: successorType,
			RelativePath: filepath.ToSlash(filepath.Join("workflow", successorType, spec.Value)),
			Created:      true,
		})
	}
	for _, spec := range adoptSpecs {
		successorType := spec.Type
		if successorType == "" {
			successorType = string(parent.WorkflowType)
		}
		out = append(out, splitSuccessorJSON{
			Name: spec.Value, Type: successorType,
			RelativePath: filepath.ToSlash(filepath.Clean(spec.Value)),
			Adopted:      true,
		})
	}
	return out
}

func encodeSplitJSON(cmd *cobra.Command, result splitResult) error {
	if result.Successors == nil {
		result.Successors = []splitSuccessorJSON{}
	}
	if result.Gate.Missing == nil {
		result.Gate.Missing = []string{}
	}
	enc := json.NewEncoder(cmd.OutOrStdout())
	enc.SetIndent("", "  ")
	return enc.Encode(result)
}

func printSplitDryRun(cmd *cobra.Command, result splitResult) error {
	out := cmd.OutOrStdout()
	if _, err := fmt.Fprintf(out, "dry-run: would split %s into %d successor(s)\n",
		result.Parent.StableID, len(result.Successors)); err != nil {
		return err
	}
	for _, successor := range result.Successors {
		verb := "adopt"
		if successor.Created {
			verb = "create"
		}
		if _, err := fmt.Fprintf(out, "  %-7s %s (%s) at %s\n",
			verb, successor.Name, successor.Type, successor.RelativePath); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintf(out,
		"\nWould stamp:\n  %s: split_into + split_at\n  each successor: split_from\n\n"+
			"The retirement gate would then refuse\n"+
			"  camp workitem promote %s --target completed\n"+
			"until every successor exists.\n\nNothing was changed.\n",
		result.Parent.StableID, result.Parent.StableID)
	return err
}

func printSplitResult(cmd *cobra.Command, result splitResult) error {
	out := cmd.OutOrStdout()
	if _, err := fmt.Fprintf(out, "split %s into %d successor(s)\n",
		result.Parent.StableID, len(result.Successors)); err != nil {
		return err
	}
	for _, successor := range result.Successors {
		verb := "adopted"
		if successor.Created {
			verb = "created"
		}
		if _, err := fmt.Fprintf(out, "  %s %s (%s) at %s\n",
			verb, successor.StableID, successor.Ref, successor.RelativePath); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintf(out,
		"\nlineage stamped both ways. %s now retires only after every\n"+
			"successor exists — camp workitem promote reports any that do not.\n",
		result.Parent.StableID)
	return err
}

// discoveredIDs indexes a discovery walk by stable id, for the gate.
func discoveredIDs(ctx context.Context, campaignRoot string, cfg *config.CampaignConfig) (map[string]bool, error) {
	resolver := paths.NewResolverFromConfig(campaignRoot, cfg)
	items, err := wkitem.Discover(ctx, campaignRoot, resolver)
	if err != nil {
		return nil, camperrors.Wrap(err, "discovering work items")
	}
	out := make(map[string]bool, len(items))
	for _, item := range items {
		out[wkitem.StableIDOf(item)] = true
	}
	return out, nil
}
