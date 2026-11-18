package skills

import (
	"regexp"
	"strings"
	"testing"

	intskills "github.com/Obedience-Corp/camp/internal/skills"
)

// emptyLabel matches a human line that prints a label with nothing after it:
// a trailing colon, an empty or space-padded parenthesis, a dangling arrow, or
// a doubled or trailing space where a value was left out.
var emptyLabel = regexp.MustCompile(`:\s*$|\(\s*\)|\s\)|->\s*[);]|\s{2,}|\s$`)

func pluginFile(state intskills.ShippedState, action intskills.FileAction, written bool) intskills.FileOutcome {
	return intskills.FileOutcome{
		FileStatus: intskills.FileStatus{
			ShippedFile: intskills.ShippedFile{Bundle: "festival-app", Path: "SKILL.md", Plugin: "camp-app"},
			State:       state,
		},
		Action:  action,
		Written: written,
	}
}

func TestUpdateLines_NoLabelWithoutAValue(t *testing.T) {
	notes := func(n intskills.PluginNote) map[string]intskills.PluginNote {
		n.Plugin = "camp-app"
		return map[string]intskills.PluginNote{"festival-app": n}
	}
	cases := map[string]updateOutput{
		"dry run add": {res: &intskills.UpdateResult{DryRun: true,
			Files: []intskills.FileOutcome{pluginFile(intskills.ShippedMissing, intskills.ActionAdd, false)}, Plugins: notes(intskills.PluginNote{Offered: "0.3.0-1-gabc1234"})}},
		"add": {res: &intskills.UpdateResult{
			Files: []intskills.FileOutcome{pluginFile(intskills.ShippedMissing, intskills.ActionAdd, true)}, Plugins: notes(intskills.PluginNote{Offered: "0.4.0"})}},
		"add without a version": {res: &intskills.UpdateResult{
			Files: []intskills.FileOutcome{pluginFile(intskills.ShippedMissing, intskills.ActionAdd, true)}, Plugins: notes(intskills.PluginNote{})}},
		"update without a recorded version": {res: &intskills.UpdateResult{
			Files: []intskills.FileOutcome{pluginFile(intskills.ShippedOutdated, intskills.ActionUpdate, true)}, Plugins: notes(intskills.PluginNote{Offered: "0.4.0"})}},
		"ahead without versions": {res: &intskills.UpdateResult{
			Files: []intskills.FileOutcome{pluginFile(intskills.ShippedAhead, intskills.ActionKeep, false)}, Plugins: notes(intskills.PluginNote{})}},
		"incomparable without versions": {res: &intskills.UpdateResult{
			Files: []intskills.FileOutcome{pluginFile(intskills.ShippedUnknown, intskills.ActionPreserve, false)}, Plugins: notes(intskills.PluginNote{Incomparable: true})}},
		"declined": {res: &intskills.UpdateResult{
			Files: []intskills.FileOutcome{pluginFile(intskills.ShippedDeclined, intskills.ActionKeep, false)}, Plugins: notes(intskills.PluginNote{})}},
		"unresolved without a version": {res: &intskills.UpdateResult{},
			plan: pluginPlan{unresolved: []unresolvedBundle{{slug: "festival-app", state: intskills.ShippedProviderMissing}}}},
		"links": {res: &intskills.UpdateResult{}, link: true, links: []intskills.BundleLink{
			{Tool: "claude", Path: ".claude/skills/festival-app", Result: intskills.LinkLinked},
			{Tool: "agents", Path: ".agents/skills", Result: intskills.LinkLeftToObey},
		}},
	}
	for name, out := range cases {
		lines := out.lines()
		for _, line := range lines {
			if emptyLabel.MatchString(line) {
				t.Errorf("%s: line with an empty value: %q", name, line)
			}
		}
		if out.res.DryRun && strings.Contains(strings.Join(lines, "\n"), "undo ") {
			t.Errorf("%s: a dry run printed an undo:\n%s", name, strings.Join(lines, "\n"))
		}
	}
}

func TestBundleRow_PreviousProviderVersion(t *testing.T) {
	note := map[string]intskills.PluginNote{"festival-app": {Plugin: "camp-app", Offered: "0.4.0", Recorded: "0.3.0"}}
	cases := map[string]struct {
		file     intskills.FileOutcome
		dryRun   bool
		previous string
	}{
		"updated":          {pluginFile(intskills.ShippedOutdated, intskills.ActionUpdate, true), false, "0.3.0"},
		"replaced":         {pluginFile(intskills.ShippedModified, intskills.ActionReplace, true), false, "0.3.0"},
		"would update":     {pluginFile(intskills.ShippedOutdated, intskills.ActionUpdate, false), true, "0.3.0"},
		"added":            {pluginFile(intskills.ShippedMissing, intskills.ActionAdd, true), false, ""},
		"kept":             {pluginFile(intskills.ShippedModified, intskills.ActionPreserve, false), false, ""},
		"current, nothing": {pluginFile(intskills.ShippedCurrent, intskills.ActionKeep, false), false, ""},
	}
	for name, tc := range cases {
		out := updateOutput{res: &intskills.UpdateResult{DryRun: tc.dryRun, Files: []intskills.FileOutcome{tc.file}, Plugins: note}}
		if got := out.bundleRows()[0].PreviousProviderVersion; got != tc.previous {
			t.Errorf("%s: previous_provider_version = %q, want %q", name, got, tc.previous)
		}
	}
	same := map[string]intskills.PluginNote{"festival-app": {Plugin: "camp-app", Offered: "0.4.0", Recorded: "0.4.0"}}
	out := updateOutput{res: &intskills.UpdateResult{Files: []intskills.FileOutcome{pluginFile(intskills.ShippedOutdated, intskills.ActionUpdate, true)}, Plugins: same}}
	if got := out.bundleRows()[0].PreviousProviderVersion; got != "" {
		t.Errorf("same recorded version: previous_provider_version = %q, want omitted", got)
	}
}

func TestUpdateLines_DeclinedWithoutCampAppNeverLoops(t *testing.T) {
	for _, report := range []intskills.ProviderReport{
		{Plugin: "camp-app", Status: intskills.ProviderNotInstalled, Detail: "camp-app is not installed"},
		{Plugin: "camp-app", Status: intskills.ProviderNoContract, Detail: "this camp-app does not offer skills to camp; update camp-app"},
	} {
		out := updateOutput{res: &intskills.UpdateResult{}, plan: pluginPlan{
			report:     &report,
			unresolved: []unresolvedBundle{{slug: "festival-app", state: intskills.ShippedDeclined}},
		}}
		text := strings.Join(out.lines(), "\n")
		if strings.Contains(text, "run 'camp skills update festival-app'") {
			t.Errorf("%s: the advice repeats the command just run:\n%s", report.Status, text)
		}
		if !strings.Contains(text, "adding it back needs camp-app, and "+report.Detail) {
			t.Errorf("%s: the line does not say camp-app is needed and why:\n%s", report.Status, text)
		}
	}
}

func TestUpdateJSON_DryRunFlag(t *testing.T) {
	for _, dryRun := range []bool{false, true} {
		var buf strings.Builder
		out := updateOutput{res: &intskills.UpdateResult{DryRun: dryRun}}
		if err := out.printJSON(&buf); err != nil {
			t.Fatal(err)
		}
		if got := strings.Contains(buf.String(), `"dry_run": true`); got != dryRun {
			t.Errorf("dry run %v: JSON = %s", dryRun, buf.String())
		}
	}
}

func TestUpdateLines_ProblemIsLeftAlone(t *testing.T) {
	f := pluginFile(intskills.ShippedUnknown, intskills.ActionPreserve, false)
	f.Problem = ".campaign/skills/festival-app is a file, not a folder"
	out := updateOutput{res: &intskills.UpdateResult{Files: []intskills.FileOutcome{f},
		Plugins: map[string]intskills.PluginNote{"festival-app": {Plugin: "camp-app", Offered: "0.4.0"}}}}
	lines := out.lines()
	if lines[0] != "left festival-app alone: .campaign/skills/festival-app is a file, not a folder; camp does not replace it" {
		t.Fatalf("lines = %q", lines)
	}
	row := out.bundleRows()[0]
	if row.State != "unknown" || row.Action != "kept" || row.Undo != "" {
		t.Fatalf("row = %+v, want unknown, kept, no undo", row)
	}
}
