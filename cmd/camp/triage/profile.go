package triage

import (
	"fmt"
	"io"
	"sort"

	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/jsoncontract"
	"github.com/Obedience-Corp/camp/internal/triage"
)

// ProfileJSONVersion is the schema version of `camp triage profile --json`.
const ProfileJSONVersion = "triage-profile-resolved/v1alpha1"

type profileResult struct {
	SchemaVersion string                       `json:"schema_version"`
	Name          string                       `json:"name"`
	FromFile      bool                         `json:"from_file"`
	Resolved      triage.ResolvedProfile       `json:"resolved"`
	TypePolicies  map[string]triage.TypePolicy `json:"type_policies"`
}

func newProfileCommand() *cobra.Command {
	var (
		jsonOut  bool
		resolved bool
		name     string
	)

	cmd := &cobra.Command{
		Use:   "profile",
		Short: "Show the resolved triage profile",
		Long: `Print the profile a run would use, fully merged.

Resolution is: the camp's .campaign/triage/profile.yaml when it exists,
otherwise the named built-in. Keys the file omits inherit the built-in default.
A type's policy is types/<type>.yaml, else types/_default.yaml, else camp's
built-in, and a type policy that declares dispositions replaces the inherited
vocabulary rather than adding to it, so a type can genuinely restrict what it
may be decided into.

This is the same object embedded in every run manifest, which is what keeps a
verdict explainable after the profile moves on.`,
		Args: jsoncontract.Args(ProfileJSONVersion, func() bool { return jsonOut }, cobra.NoArgs),
		Annotations: map[string]string{
			"agent_allowed": "true",
			"agent_reason":  "Read-only resolved configuration; writes nothing",
		},
		RunE: jsoncontract.RunE(ProfileJSONVersion, func() bool { return jsonOut },
			func(cmd *cobra.Command, _ []string) error {
				return runProfile(cmd, jsonOut, resolved, name)
			}),
	}
	cmd.SetFlagErrorFunc(jsoncontract.FlagErrorFunc(ProfileJSONVersion, func() bool { return jsonOut }))

	cmd.Flags().BoolVar(&jsonOut, "json", false, "Output result as a single JSON object")
	cmd.Flags().BoolVar(&resolved, "resolved", false,
		"Print the fully merged profile (the default and only mode today)")
	cmd.Flags().StringVar(&name, "profile", "",
		"Resolve a named built-in instead of the camp's: default, sweep, or deep")
	return cmd
}

func runProfile(cmd *cobra.Command, jsonOut, resolved bool, name string) error {
	ctx := cmd.Context()
	_ = resolved // the merged view is the only view; the flag reads as intent

	_, root, err := config.LoadCampaignConfigFromCwd(ctx)
	if err != nil {
		return camperrors.Wrap(err, "not in a camp directory")
	}

	resolution, err := triage.ResolveProfileNamed(ctx, root, name)
	if err != nil {
		return err
	}

	result := profileResult{
		SchemaVersion: ProfileJSONVersion,
		Name:          resolution.Name,
		FromFile:      resolution.FromFile,
		Resolved:      resolution.Profile,
		TypePolicies:  resolution.TypePolicies,
	}
	if result.TypePolicies == nil {
		result.TypePolicies = map[string]triage.TypePolicy{}
	}

	if jsonOut {
		return writeJSON(cmd.OutOrStdout(), result)
	}
	return printProfile(cmd.OutOrStdout(), result)
}

func printProfile(w io.Writer, result profileResult) error {
	source := "built-in"
	if result.FromFile {
		source = ".campaign/triage/profile.yaml"
	}
	if _, err := fmt.Fprintf(w, "Profile %s (%s)\n\n", result.Name, source); err != nil {
		return err
	}

	p := result.Resolved
	if _, err := fmt.Fprintf(w,
		"  runs.mode              %s\n"+
			"  runs.stale_after_days  %d\n"+
			"  review.group_by        %s\n"+
			"  review.batch_size      %d\n"+
			"  review.approval        %s\n"+
			"  anchors.recheck_minutes %d\n"+
			"  apply.attention_changes %s\n",
		p.Runs.Mode, p.Runs.StaleAfterDays, p.Review.GroupBy, p.Review.BatchSize,
		p.Review.Approval, p.Anchors.RecheckMinutes, p.Apply.AttentionChanges); err != nil {
		return err
	}

	if _, err := fmt.Fprintf(w, "\n  evidence depth by stage\n"); err != nil {
		return err
	}
	for _, stage := range sortedStages(p.Evidence.DepthByStage) {
		if _, err := fmt.Fprintf(w, "    %-8s %s\n", stage, p.Evidence.DepthByStage[stage]); err != nil {
			return err
		}
	}

	if len(result.TypePolicies) == 0 {
		_, err := fmt.Fprintf(w,
			"\nNo type policies on disk; every type uses camp's built-in vocabulary.\n")
		return err
	}

	if _, err := fmt.Fprintf(w, "\n  type policies\n"); err != nil {
		return err
	}
	for _, wfType := range sortedPolicyNames(result.TypePolicies) {
		policy := result.TypePolicies[wfType]
		if _, err := fmt.Fprintf(w, "    %-10s evidence=%-8s %v\n",
			wfType, policy.Evidence, policy.Labels()); err != nil {
			return err
		}
	}
	return nil
}

func sortedStages(m map[string]triage.EvidenceDepth) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sortedPolicyNames(m map[string]triage.TypePolicy) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func init() {
	Cmd.AddCommand(newProfileCommand())
}
