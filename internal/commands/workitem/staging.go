package workitem

import (
	"context"
	"fmt"
	"io"
	"os"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	wkitem "github.com/Obedience-Corp/camp/internal/workitem"
	"github.com/Obedience-Corp/camp/internal/workitem/resolver"
	"github.com/Obedience-Corp/camp/pkg/commitkit"
)

const (
	linkRegistryRelPath = ".campaign/workitems/links.yaml"

	skipReasonOutOfScope          = "(out of scope)"
	skipReasonExcludeFlag         = "(--exclude)"
	skipReasonPointerOffByDefault = "(submodule pointer; use --include-submodule-pointer)"

	stageAnnotationLinkRegistry = "link registry auto-included"
)

// PlanContext labels which staging-matrix row a plan came from. Stable
// strings: used by both the human plan output and the integration tests.
type PlanContext string

const (
	PlanContextWorkitemDir   PlanContext = "workitem directory"
	PlanContextCampaignRoot  PlanContext = "campaign root"
	PlanContextLinkedProject PlanContext = "linked project"
	PlanContextFestival      PlanContext = "festival"
	PlanContextStagedOnly    PlanContext = "staged-only"
)

// PlanOptions inputs to ComputePlan. Mirrors the camp workitem commit flag
// surface so the planner can be exercised from tests without spinning up cobra.
type PlanOptions struct {
	Cwd                     string
	Explicit                string   // --workitem <selector>
	Project                 string   // --project <name> override
	Includes                []string // --include <path> (repeatable)
	Excludes                []string // --exclude <path> (repeatable)
	StagedOnly              bool     // --staged
	IncludeSubmodulePointer bool     // --include-submodule-pointer
	FestivalID              string   // --festival <id>, or inferred from cwd
	CampaignID              string
	Stderr                  io.Writer // for warning lines from ref backfill (default: os.Stderr)
}

// SkippedEntry pairs a path with a stable reason string.
type SkippedEntry struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

// StagingPlan is the contract between ComputePlan and the commit runner. Stage
// is what the planner intends to `git add`. PreStaged is what is already in
// the index (used by --staged so we do not re-stage). Skip explains paths
// intentionally left out.
type StagingPlan struct {
	Workitem         *wkitem.WorkItem
	WorkitemRef      string
	QuestID          string
	FestivalRef      string
	Context          PlanContext
	ContextNote      string
	RepoRoot         string
	Stage            []string
	StageAnnotations map[string]string
	PreStaged        []string
	Skip             []SkippedEntry
	Tag              string
	Warnings         []string
}

func (p *StagingPlan) addStageNote(path, note string) {
	if p.StageAnnotations == nil {
		p.StageAnnotations = make(map[string]string)
	}
	p.StageAnnotations[path] = note
}

// ComputePlan resolves a workitem from the current context, branches on the
// matrix row that applies, and returns a StagingPlan that the commit runner
// can hand to commit.Workitem. Refusal modes (no workitem, empty plan with no
// override) surface as typed errors so the CLI can map them to exit codes.
func ComputePlan(ctx context.Context, campaignRoot string, opts PlanOptions) (*StagingPlan, error) {
	if campaignRoot == "" {
		return nil, camperrors.NewValidation("root", "campaign root required", nil)
	}

	festivalID := opts.FestivalID
	if festivalID == "" {
		festivalID = inferFestivalIDFromCwd(campaignRoot, opts.Cwd)
	}

	res, err := resolver.Resolve(ctx, campaignRoot, resolver.Options{
		Explicit:   opts.Explicit,
		Cwd:        opts.Cwd,
		FestivalID: festivalID,
	})
	if err != nil {
		return nil, camperrors.Wrap(err, "resolve workitem")
	}
	if res == nil || res.Workitem == nil {
		return nil, ErrNoWorkitemContext
	}

	wi := res.Workitem
	errw := opts.Stderr
	if errw == nil {
		errw = os.Stderr
	}
	ref, err := ensureRefForCommit(ctx, campaignRoot, wi, errw)
	if err != nil {
		return nil, err
	}
	festivalRef := ""
	if res.Source == resolver.SourceFestival {
		festivalRef = festivalRefFromString(festivalID)
	}
	plan := &StagingPlan{
		Workitem:    wi,
		WorkitemRef: ref,
		QuestID:     res.QuestID,
		FestivalRef: festivalRef,
		Tag:         commitkit.FormatContextTagsFull(opts.CampaignID, res.QuestID, festivalRef, ref),
	}
	if ref == "" && wi != nil && wi.ItemKind == wkitem.ItemKindDirectory && wi.StableID != "" {
		plan.Warnings = append(plan.Warnings,
			"workitem "+wi.Key+" has no ref field (pre-v1alpha6); commit tag will omit the WI- segment. Run `camp workitem doctor --fix` to backfill.")
	}

	if opts.StagedOnly {
		repoRoot := campaignRoot
		if sub, ok := cwdSubGitRepo(opts.Cwd, campaignRoot); ok {
			repoRoot = sub
		}
		plan.Context = PlanContextStagedOnly
		plan.ContextNote = "using current git index"
		plan.RepoRoot = repoRoot
		staged, err := listStagedFiles(ctx, repoRoot)
		if err != nil {
			return nil, camperrors.Wrap(err, "list staged files")
		}
		plan.PreStaged = staged
		stage, skip, err := applyIncludes(repoRoot, nil, opts.Includes)
		if err != nil {
			return nil, err
		}
		plan.Stage = applyExcludes(stage, opts.Excludes, &plan.Skip)
		plan.Skip = append(plan.Skip, skip...)
		return plan, nil
	}

	if opts.Project != "" {
		return computeProjectPlan(ctx, campaignRoot, opts, plan)
	}

	// cwd-first routing: when the working directory is inside a sub-git-repo
	// (typically projects/<name>/...), stage from that sub-repo regardless of
	// which resolver tier matched. This keeps "I'm in the project, commit my
	// workitem changes here" the natural one-liner.
	if subRepo, ok := cwdSubGitRepo(opts.Cwd, campaignRoot); ok {
		return computeDetectedProjectPlan(ctx, opts, plan, subRepo)
	}

	switch res.Source {
	case resolver.SourceLink:
		return computeLinkPlan(ctx, campaignRoot, opts, plan)
	case resolver.SourceFestival:
		return computeFestivalPlan(ctx, campaignRoot, opts, plan)
	default:
		// SourceExplicit, SourceAncestor, SourceCurrent — all stage from the
		// campaign root scoped to the workitem directory.
		return computeWorkitemDirPlan(ctx, campaignRoot, opts, plan, res.Source)
	}
}

// PrintPlan writes the human-readable plan summary to w. It keeps the stable
// staging-plan lines that integration tests grep while allowing per-path
// annotations for planner-included files.
func PrintPlan(w io.Writer, plan *StagingPlan) error {
	if plan == nil || plan.Workitem == nil {
		return camperrors.NewValidation("plan", "nil plan", nil)
	}
	if _, err := fmt.Fprintf(w, "workitem: %s (ref: %s)\n", plan.Workitem.StableID, plan.WorkitemRef); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "context:  %s", plan.Context); err != nil {
		return err
	}
	if plan.ContextNote != "" {
		if _, err := fmt.Fprintf(w, " (%s)", plan.ContextNote); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintln(w); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, "staging:"); err != nil {
		return err
	}
	if len(plan.PreStaged) > 0 {
		for _, p := range plan.PreStaged {
			if _, err := fmt.Fprintf(w, "  S  %s\n", p); err != nil {
				return err
			}
		}
	}
	for _, p := range plan.Stage {
		if note := plan.StageAnnotations[p]; note != "" {
			if _, err := fmt.Fprintf(w, "  A  %s (%s)\n", p, note); err != nil {
				return err
			}
			continue
		}
		if _, err := fmt.Fprintf(w, "  A  %s\n", p); err != nil {
			return err
		}
	}
	if len(plan.Skip) > 0 {
		if _, err := fmt.Fprintln(w, "skipped:"); err != nil {
			return err
		}
		for _, s := range plan.Skip {
			if _, err := fmt.Fprintf(w, "  %s %s\n", s.Path, s.Reason); err != nil {
				return err
			}
		}
	}
	_, err := fmt.Fprintf(w, "tag:    %s\n", plan.Tag)
	return err
}

// ErrNoWorkitemContext is returned when ComputePlan cannot resolve any
// workitem context. The CLI maps this to exit code 2 with a help hint.
var ErrNoWorkitemContext = camperrors.NewValidation(
	"workitem",
	"no workitem context resolved",
	nil,
)
