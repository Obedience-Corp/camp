package skills

import (
	"context"
	"os"
	"slices"
	"testing"

	intskills "github.com/Obedience-Corp/camp/internal/skills"
)

// coreCall records what updateCore handed to Update, if it called it.
type coreCall struct {
	called bool
	opts   intskills.UpdateOptions
}

func coreRunner(t *testing.T, answer string) intskills.ProviderRunner {
	t.Helper()
	if answer == "" {
		return func(context.Context, []string) ([]byte, error) { return nil, intskills.ErrProviderNotInstalled }
	}
	data, err := os.ReadFile("../../../internal/skills/testdata/provider/" + answer)
	if err != nil {
		t.Fatal(err)
	}
	return func(context.Context, []string) ([]byte, error) { return data, nil }
}

func runCore(t *testing.T, answer string, manifest *intskills.Manifest, req updateRequest) (coreCall, pluginPlan, error) {
	t.Helper()
	var call coreCall
	core := updateCore{
		sources: planSources{
			runner:   coreRunner(t, answer),
			manifest: func(string) (*intskills.Manifest, error) { return manifest, nil },
		},
		update: func(_ context.Context, opts intskills.UpdateOptions) (*intskills.UpdateResult, error) {
			call = coreCall{called: true, opts: opts}
			return &intskills.UpdateResult{}, nil
		},
	}
	shipped := []intskills.ShippedFile{{Bundle: "campaign-commit", Path: "SKILL.md"}, {Bundle: "camp-navigation", Path: "SKILL.md"}}
	plan, res, err := core.run(t.Context(), "/nonexistent-camp", shipped, req)
	if err == nil && res == nil {
		t.Fatal("run() returned no result and no error")
	}
	return call, plan, err
}

// The scope Update gets, and whether it runs at all, for every kind of
// request. Update reads an empty Slugs as every camp bundle, so a request
// whose plugin names did not resolve must never call it.
func TestUpdateCore_ScopeAndWhetherUpdateRuns(t *testing.T) {
	recorded := &intskills.Manifest{Providers: map[string]intskills.ProviderRecord{
		"festival-app": {Plugin: "camp-app", Version: "0.3.0"},
	}}
	declined := &intskills.Manifest{Providers: map[string]intskills.ProviderRecord{
		"festival-app": {Plugin: "camp-app", Declined: true},
	}}
	const contract = "camp-app-contract.json"
	cases := []struct {
		name      string
		answer    string
		manifest  *intskills.Manifest
		req       updateRequest
		wantCall  bool
		wantSlugs []string
		wantErr   bool
	}{
		{"no args refreshes every camp bundle", "", nil, updateRequest{}, true, nil, false},
		{"camp slug only", "", nil, updateRequest{args: []string{"campaign-commit"}}, true, []string{"campaign-commit"}, false},
		{"offered plugin slug only", contract, nil, updateRequest{args: []string{"festival-app"}}, true, []string{"festival-app"}, false},
		{"camp and unresolved plugin, auto", "", nil, updateRequest{args: []string{"campaign-commit", "festival-app"}, auto: true}, true, []string{"campaign-commit"}, false},
		{"camp and unresolved plugin, person", "", nil, updateRequest{args: []string{"campaign-commit", "festival-app"}}, false, nil, true},
		{"only unresolved, unrecorded, auto", "", nil, updateRequest{args: []string{"festival-app"}, auto: true}, false, nil, false},
		{"only unresolved, unrecorded, person", "", nil, updateRequest{args: []string{"festival-app"}}, false, nil, true},
		{"only unresolved, recorded, auto", "", recorded, updateRequest{args: []string{"festival-app"}, auto: true}, false, nil, false},
		{"only unresolved, recorded, person", "", recorded, updateRequest{args: []string{"festival-app"}}, false, nil, false},
		{"old camp-app, recorded, auto", "camp-app-0.2.1.json", recorded, updateRequest{args: []string{"festival-app"}, auto: true}, false, nil, false},
		{"declined, offered, auto", contract, declined, updateRequest{args: []string{"festival-app"}, auto: true}, true, []string{"festival-app"}, false},
		{"declined, unavailable, person", "", declined, updateRequest{args: []string{"festival-app"}}, false, nil, false},
		{"duplicates", contract, nil, updateRequest{args: []string{"festival-app", "festival-app", "campaign-commit", "campaign-commit"}}, true, []string{"campaign-commit", "festival-app"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			call, _, err := runCore(t, tc.answer, tc.manifest, tc.req)
			if (err != nil) != tc.wantErr {
				t.Fatalf("error = %v, want error %v", err, tc.wantErr)
			}
			if call.called != tc.wantCall {
				t.Fatalf("Update called = %v, want %v", call.called, tc.wantCall)
			}
			if call.called && !slices.Equal(call.opts.Slugs, tc.wantSlugs) {
				t.Fatalf("Update Slugs = %q, want %q", call.opts.Slugs, tc.wantSlugs)
			}
			if call.called && (call.opts.Auto != tc.req.auto || call.opts.Root != "/nonexistent-camp") {
				t.Fatalf("Update options = %+v", call.opts)
			}
		})
	}
}

func TestUpdateCore_NotOfferedAnyMoreIsTold(t *testing.T) {
	gone := &intskills.Manifest{Providers: map[string]intskills.ProviderRecord{
		"old-app": {Plugin: "camp-app", Version: "0.2.0"},
	}}
	call, plan, err := runCore(t, "camp-app-contract.json", gone, updateRequest{args: []string{"old-app"}, auto: true})
	if err != nil || call.called {
		t.Fatalf("error = %v, Update called = %v; want neither", err, call.called)
	}
	if plan.report.Detail != "camp-app no longer offers old-app" || !plan.unresolved[0].notOffered {
		t.Fatalf("detail = %q, unresolved = %+v", plan.report.Detail, plan.unresolved)
	}
	out := updateOutput{res: &intskills.UpdateResult{}, plan: plan}
	row := out.bundleRows()[0]
	if row.State != "provider-missing" || row.Source != "plugin:camp-app" {
		t.Fatalf("row = %+v", row)
	}
	if lines := out.lines(); lines[0] != "left old-app alone: camp-app no longer offers it (written by camp-app 0.2.0)" {
		t.Fatalf("line = %q", lines[0])
	}
}

func TestUnresolvedRow_UsesTheRecordedPlugin(t *testing.T) {
	other := &intskills.Manifest{Providers: map[string]intskills.ProviderRecord{
		"festival-app": {Plugin: "camp-other", Version: "1.0.0"},
	}}
	_, plan, err := runCore(t, "", other, updateRequest{args: []string{"festival-app"}, auto: true})
	if err != nil {
		t.Fatal(err)
	}
	out := updateOutput{res: &intskills.UpdateResult{}, plan: plan}
	if row := out.bundleRows()[0]; row.Source != "plugin:camp-other" {
		t.Fatalf("source = %q, want the recorded plugin", row.Source)
	}
}
