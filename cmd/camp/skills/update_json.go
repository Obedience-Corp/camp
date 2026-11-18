package skills

import (
	"encoding/json"
	"fmt"
	"io"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	intskills "github.com/Obedience-Corp/camp/internal/skills"
)

const sourceCamp = "camp"

type skillsUpdateResult struct {
	SchemaVersion string `json:"schema_version"`
	// DryRun marks a dry run: nothing was written or linked.
	DryRun    bool                   `json:"dry_run,omitempty"`
	Bundles   []skillsUpdateBundle   `json:"bundles"`
	Links     []skillsUpdateLink     `json:"links"`
	Providers []skillsUpdateProvider `json:"providers"`
}

type skillsUpdateBundle struct {
	Slug            string `json:"slug"`
	Source          string `json:"source"`
	ProviderVersion string `json:"provider_version,omitempty"`
	// PreviousProviderVersion is the version recorded before an update or
	// replace that wrote a different one.
	PreviousProviderVersion string `json:"previous_provider_version,omitempty"`
	Action                  string `json:"action"`
	State                   string `json:"state"`
	Undo                    string `json:"undo,omitempty"`
}

type skillsUpdateLink struct {
	Tool   string `json:"tool"`
	Path   string `json:"path"`
	Result string `json:"result"`
}

type skillsUpdateProvider struct {
	Plugin  string `json:"plugin"`
	Status  string `json:"status"`
	Version string `json:"version,omitempty"`
	Detail  string `json:"detail,omitempty"`
}

func (o updateOutput) printJSON(out io.Writer) error {
	result := skillsUpdateResult{
		SchemaVersion: SkillsUpdateJSONVersion,
		DryRun:        o.res.DryRun,
		Bundles:       o.bundleRows(),
		Links:         []skillsUpdateLink{},
		Providers:     []skillsUpdateProvider{},
	}
	for _, l := range o.links {
		result.Links = append(result.Links, skillsUpdateLink{Tool: l.Tool, Path: l.Path, Result: l.Result})
	}
	if r := o.plan.report; r != nil {
		result.Providers = append(result.Providers, skillsUpdateProvider{
			Plugin: r.Plugin, Status: string(r.Status), Version: r.Version, Detail: r.Detail,
		})
	}
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return camperrors.Wrap(err, "marshal JSON")
	}
	_, err = fmt.Fprintln(out, string(data))
	return err
}

// bundleRows is one row per bundle in scope, in the order Update classified
// them, then the named plugin bundles camp-app could not refresh.
func (o updateOutput) bundleRows() []skillsUpdateBundle {
	rows := []skillsUpdateBundle{}
	index := make(map[string]int)
	var files [][]intskills.FileOutcome
	for _, f := range o.res.Files {
		i, seen := index[f.Bundle]
		if !seen {
			i = len(files)
			index[f.Bundle] = i
			files = append(files, nil)
		}
		files[i] = append(files[i], f)
	}
	for _, bundle := range files {
		rows = append(rows, o.bundleRow(bundle))
	}
	for _, u := range o.plan.unresolved {
		rows = append(rows, skillsUpdateBundle{
			Slug: u.slug, Source: "plugin:" + u.plugin, ProviderVersion: u.version,
			Action: "none", State: string(u.state),
		})
	}
	return rows
}

func (o updateOutput) bundleRow(files []intskills.FileOutcome) skillsUpdateBundle {
	first := files[0]
	row := skillsUpdateBundle{Slug: first.Bundle, Source: sourceCamp}
	action, state, wrote := aggregateBundle(files, o.res.DryRun)
	row.Action, row.State = action, string(state)
	if first.Plugin != "" {
		note := o.res.Plugins[first.Bundle]
		row.Source = "plugin:" + first.Plugin
		row.ProviderVersion = note.Recorded
		if wrote || (row.ProviderVersion == "" && state != intskills.ShippedDeclined) {
			row.ProviderVersion = note.Offered
		}
		if (action == "updated" || action == "replaced") && note.Recorded != "" && note.Recorded != row.ProviderVersion {
			row.PreviousProviderVersion = note.Recorded
		}
		if action == "added" && !o.res.DryRun {
			row.Undo = "camp skills decline " + first.Bundle
		}
	}
	for _, u := range o.res.Undo {
		if u.Bundle == first.Bundle {
			row.Undo = u.Command
		}
	}
	return row
}

// aggregateBundle folds file outcomes into one bundle action and the state
// the bundle is in after the run (before it, under a dry run).
func aggregateBundle(files []intskills.FileOutcome, dryRun bool) (string, intskills.ShippedState, bool) {
	writes := map[intskills.FileAction]bool{}
	state := intskills.ShippedCurrent
	kept := false
	for _, f := range files {
		wrote := f.Written || (dryRun && f.Action.Writes())
		writes[f.Action] = writes[f.Action] || wrote
		after := f.State
		if f.Written {
			after = intskills.ShippedCurrent
		}
		if bundleRank(after) > bundleRank(state) {
			state = after
		}
		kept = kept || f.Action == intskills.ActionPreserve || f.State == intskills.ShippedAhead
	}
	switch {
	case writes[intskills.ActionReplace]:
		return "replaced", state, true
	case writes[intskills.ActionUpdate]:
		return "updated", state, true
	case writes[intskills.ActionAdd]:
		return "added", state, true
	case kept:
		return "kept", state, false
	default:
		return "none", state, false
	}
}

func bundleRank(s intskills.ShippedState) int {
	for i, ranked := range []intskills.ShippedState{
		intskills.ShippedCurrent, intskills.ShippedMissing, intskills.ShippedOutdated,
		intskills.ShippedSymlink, intskills.ShippedUnknown, intskills.ShippedModified,
		intskills.ShippedAhead, intskills.ShippedProviderMissing, intskills.ShippedDeclined,
	} {
		if s == ranked {
			return i
		}
	}
	return 0
}
