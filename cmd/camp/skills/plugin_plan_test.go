package skills

import (
	"context"
	"slices"
	"strings"
	"testing"

	intskills "github.com/Obedience-Corp/camp/internal/skills"
)

var planCampSlugs = []string{"camp-navigation", "campaign-commit"}

func TestUnknownPluginBundleError(t *testing.T) {
	today := intskills.UnknownBundleError([]string{"nope"}, planCampSlugs).Error()
	if today != "unknown shipped skill bundle nope; shipped bundles: camp-navigation, campaign-commit" {
		t.Fatalf("today's refusal changed: %q", today)
	}
	cases := map[intskills.ProviderStatus]string{
		intskills.ProviderNotInstalled: today,
		intskills.ProviderOK:           today + "; camp-app offers: festival-app",
		intskills.ProviderTimedOut:     today + "; skipped skills from camp-app: no answer within 5s",
	}
	for status, want := range cases {
		report := intskills.ProviderReport{Plugin: "camp-app", Status: status, Detail: "no answer within 5s"}
		if status == intskills.ProviderOK {
			report.Bundles = []intskills.PluginBundle{{Name: "festival-app"}}
		}
		if got := unknownPluginBundleError([]string{"nope"}, planCampSlugs, report).Error(); got != want {
			t.Errorf("%s: %q, want %q", status, got, want)
		}
	}
}

func TestPluginPlanResolve(t *testing.T) {
	manifest := &intskills.Manifest{Providers: map[string]intskills.ProviderRecord{
		"recorded-app": {Plugin: "camp-app", Version: "0.4.0"},
		"declined-app": {Plugin: "camp-app", Declined: true},
	}}
	failed := intskills.ProviderReport{Plugin: "camp-app", Status: intskills.ProviderNoContract}
	named := []string{"recorded-app", "declined-app", "typo-app"}

	person := pluginPlan{report: &failed}
	if unknown := person.resolve(named, manifest, false); !slices.Equal(unknown, []string{"typo-app"}) {
		t.Fatalf("person: unknown = %v, want [typo-app]", unknown)
	}
	auto := pluginPlan{report: &failed}
	if unknown := auto.resolve(named, manifest, true); len(unknown) != 0 {
		t.Fatalf("auto: unknown = %v, want none", unknown)
	}
	want := []unresolvedBundle{
		{slug: "recorded-app", plugin: "camp-app", state: intskills.ShippedProviderMissing, version: "0.4.0"},
		{slug: "declined-app", plugin: "camp-app", state: intskills.ShippedDeclined},
	}
	if !slices.Equal(auto.unresolved, want) || len(auto.updateSlugs) != 0 {
		t.Fatalf("auto: unresolved = %+v, updateSlugs = %v; want %+v and none", auto.unresolved, auto.updateSlugs, want)
	}
}

func outcome(state intskills.ShippedState, action intskills.FileAction, written bool) intskills.FileOutcome {
	return intskills.FileOutcome{FileStatus: intskills.FileStatus{State: state}, Action: action, Written: written}
}

func TestAggregateBundle(t *testing.T) {
	cases := map[string]struct {
		files  []intskills.FileOutcome
		dryRun bool
		action string
		state  intskills.ShippedState
	}{
		"added":                {[]intskills.FileOutcome{outcome(intskills.ShippedMissing, intskills.ActionAdd, true)}, false, "added", intskills.ShippedCurrent},
		"would add":            {[]intskills.FileOutcome{outcome(intskills.ShippedMissing, intskills.ActionAdd, false)}, true, "added", intskills.ShippedMissing},
		"updated":              {[]intskills.FileOutcome{outcome(intskills.ShippedOutdated, intskills.ActionUpdate, true)}, false, "updated", intskills.ShippedCurrent},
		"replaced":             {[]intskills.FileOutcome{outcome(intskills.ShippedModified, intskills.ActionReplace, true), outcome(intskills.ShippedMissing, intskills.ActionAdd, true)}, false, "replaced", intskills.ShippedCurrent},
		"kept edit":            {[]intskills.FileOutcome{outcome(intskills.ShippedModified, intskills.ActionPreserve, false)}, false, "kept", intskills.ShippedModified},
		"kept ahead":           {[]intskills.FileOutcome{outcome(intskills.ShippedAhead, intskills.ActionKeep, false)}, false, "kept", intskills.ShippedAhead},
		"added beside an edit": {[]intskills.FileOutcome{outcome(intskills.ShippedMissing, intskills.ActionAdd, true), outcome(intskills.ShippedModified, intskills.ActionPreserve, false)}, false, "added", intskills.ShippedModified},
		"current":              {[]intskills.FileOutcome{outcome(intskills.ShippedCurrent, intskills.ActionKeep, false)}, false, "none", intskills.ShippedCurrent},
		"declined":             {[]intskills.FileOutcome{outcome(intskills.ShippedDeclined, intskills.ActionKeep, false)}, false, "none", intskills.ShippedDeclined},
	}
	for name, tc := range cases {
		action, state, _ := aggregateBundle(tc.files, tc.dryRun)
		if action != tc.action || state != tc.state {
			t.Errorf("%s: %s/%s, want %s/%s", name, action, state, tc.action, tc.state)
		}
	}
}

func TestPlanPluginSlugs_RefusesSlugsThatAreNotBundleNames(t *testing.T) {
	asked, read := 0, 0
	src := planSources{
		runner: func(context.Context, []string) ([]byte, error) {
			asked++
			return nil, intskills.ErrProviderNotInstalled
		},
		manifest: func(string) (*intskills.Manifest, error) { read++; return nil, nil },
	}
	shipped := []intskills.ShippedFile{{Bundle: "campaign-commit", Path: "SKILL.md"}, {Bundle: "camp-navigation", Path: "SKILL.md"}}
	for _, slug := range []string{"../foo", "foo/bar", ".hidden", "Foo", "/tmp/abs", "", "foo\\bar", strings.Repeat("a", 65)} {
		for _, auto := range []bool{false, true} {
			_, err := planPluginSlugs(t.Context(), src, "/nonexistent-camp", shipped, []string{"campaign-commit", slug}, auto)
			want := intskills.UnknownBundleError([]string{slug}, planCampSlugs).Error()
			if err == nil || err.Error() != want {
				t.Errorf("slug %q auto=%v: error = %v, want %q", slug, auto, err, want)
			}
		}
	}
	if asked != 0 || read != 0 {
		t.Fatalf("camp-app asked %d times and the manifest read %d times for slugs that are not bundle names", asked, read)
	}
}
