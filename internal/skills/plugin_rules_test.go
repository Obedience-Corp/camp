package skills

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/plugin"
)

func TestPluginVersion_Ordering(t *testing.T) {
	ordered := []string{"0.3.9", "0.4.0", "v0.4.0-dirty", "0.4.0-3-gabc1234", "0.4.0-5-gdef5678-dirty", "0.4.1", "1.0.0"}
	for i := 1; i < len(ordered); i++ {
		a, okA := parsePluginVersion(ordered[i-1])
		b, okB := parsePluginVersion(ordered[i])
		if !okA || !okB {
			t.Fatalf("parse %q or %q failed", ordered[i-1], ordered[i])
		}
		if c := a.compare(b); c > 0 {
			t.Errorf("%s orders after %s", ordered[i-1], ordered[i])
		}
	}
	same := []string{"0.4.0", "v0.4.0", "0.4.0-dirty"}
	for _, s := range same {
		v, _ := parsePluginVersion(s)
		if v != (pluginVersion{0, 4, 0, 0}) {
			t.Errorf("%s = %+v, want 0.4.0", s, v)
		}
	}
	for _, bad := range []string{"", "dev", "0.4", "0.4.0-rc.1", "0.4.0-3", "0.4.0-3-gXYZ", "0.4.0-x-gabc1234", "1.2.3.4", "+1.2.3", "0.4.0+meta"} {
		if _, ok := parsePluginVersion(bad); ok {
			t.Errorf("parsePluginVersion(%q) accepted, want not comparable", bad)
		}
	}
}

const (
	oldSkill = "---\nname: festival-app\ndescription: old\n---\n"
	newSkill = "---\nname: festival-app\ndescription: new\n---\n"
)

func offered(version, content string) PluginBundle {
	return PluginBundle{Name: "festival-app", Plugin: CampAppPlugin, Version: version, Files: []ShippedFile{{
		Bundle: "festival-app", Path: "SKILL.md", Content: []byte(content), Plugin: CampAppPlugin, PluginVersion: version,
	}}}
}

// pluginCamp is a camp holding festival-app with disk content, recorded with
// recordedHash and recordedProvider in its manifest.
func pluginCamp(disk, recordedHash, recordedProvider string) fstest.MapFS {
	data := "schema_version: " + ManifestSchemaVersion + "\ncamp_version: v1\nbundles:\n"
	if recordedHash != "" {
		data += "  festival-app:\n    SKILL.md: " + recordedHash + "\n"
	}
	if recordedProvider != "" {
		data += "providers:\n  festival-app:\n" + recordedProvider
	}
	fsys := fstest.MapFS{ManifestFileName: {Data: []byte(data)}}
	if disk != "" {
		fsys["festival-app/SKILL.md"] = &fstest.MapFile{Data: []byte(disk)}
	}
	return fsys
}

type pluginCase struct {
	fsys       fstest.MapFS
	offer      PluginBundle
	auto       bool
	wantState  ShippedState
	wantRecord ProviderRecord
}

func runPluginCase(t *testing.T, tc pluginCase) (*Inventory, map[string]PluginNote) {
	t.Helper()
	inv, err := Classify(t.Context(), tc.fsys, tc.offer.Files, alwaysWrittenOnce{})
	if err != nil {
		t.Fatalf("Classify() error = %v", err)
	}
	notes := inv.applyPluginRules([]PluginBundle{tc.offer}, tc.auto)
	if got := inv.Files[0].State; got != tc.wantState {
		t.Fatalf("state = %q, want %q", got, tc.wantState)
	}
	written := map[string]bool{}
	if f := inv.Files[0]; actionFor(f.State, false).Writes() {
		written[f.RelPath()] = true
	}
	next := inv.NextManifest("v2", written)
	if got := next.Providers["festival-app"]; got != tc.wantRecord {
		t.Fatalf("provider record = %+v, want %+v", got, tc.wantRecord)
	}
	return inv, notes
}

// alwaysWrittenOnce claims git shows every file unchanged since camp wrote it.
// Plugin files must never take that as evidence.
type alwaysWrittenOnce struct{}

func (alwaysWrittenOnce) WrittenOnce(context.Context, string) (bool, error) { return true, nil }

func TestPluginRules(t *testing.T) {
	oldHash := HashContent([]byte(oldSkill))
	rec := func(v string) string { return "    plugin: camp-app\n    version: " + v + "\n" }
	cases := map[string]pluginCase{
		"missing is added": {
			fsys: pluginCamp("", "", ""), offer: offered("0.4.0", newSkill),
			wantState: ShippedMissing, wantRecord: ProviderRecord{Plugin: CampAppPlugin, Version: "0.4.0"},
		},
		"older plugin wrote it, newer offers: outdated": {
			fsys: pluginCamp(oldSkill, oldHash, rec("0.3.0")), offer: offered("0.4.0", newSkill),
			wantState: ShippedOutdated, wantRecord: ProviderRecord{Plugin: CampAppPlugin, Version: "0.4.0"},
		},
		"identical version, different bytes: outdated": {
			fsys: pluginCamp(oldSkill, oldHash, rec("0.4.0-3-gaaaa")), offer: offered("0.4.0-3-gaaaa", newSkill),
			wantState: ShippedOutdated, wantRecord: ProviderRecord{Plugin: CampAppPlugin, Version: "0.4.0-3-gaaaa"},
		},
		"same count, other commit: preserved as unknown": {
			fsys: pluginCamp(oldSkill, oldHash, rec("0.4.0-3-gaaaa")), offer: offered("0.4.0-3-gbbbb", newSkill),
			wantState: ShippedUnknown, wantRecord: ProviderRecord{Plugin: CampAppPlugin, Version: "0.4.0-3-gaaaa"},
		},
		"release and its dirty build: preserved as unknown": {
			fsys: pluginCamp(oldSkill, oldHash, rec("0.4.0")), offer: offered("0.4.0-dirty", newSkill),
			wantState: ShippedUnknown, wantRecord: ProviderRecord{Plugin: CampAppPlugin, Version: "0.4.0"},
		},
		"newer plugin wrote it: ahead": {
			fsys: pluginCamp(oldSkill, oldHash, rec("0.5.0")), offer: offered("0.4.0", newSkill),
			wantState: ShippedAhead, wantRecord: ProviderRecord{Plugin: CampAppPlugin, Version: "0.5.0"},
		},
		"later development build wrote it: ahead": {
			fsys: pluginCamp(oldSkill, oldHash, rec("0.4.0-5-gdef5678")), offer: offered("0.4.0-3-gabc1234", newSkill),
			wantState: ShippedAhead, wantRecord: ProviderRecord{Plugin: CampAppPlugin, Version: "0.4.0-5-gdef5678"},
		},
		"edited and newer plugin wrote it: ahead": {
			fsys: pluginCamp("mine", oldHash, rec("0.5.0")), offer: offered("0.4.0", newSkill),
			wantState: ShippedAhead, wantRecord: ProviderRecord{Plugin: CampAppPlugin, Version: "0.5.0"},
		},
		"matching bytes beat versions": {
			fsys: pluginCamp(newSkill, HashContent([]byte(newSkill)), rec("0.9.0")), offer: offered("0.4.0", newSkill),
			wantState: ShippedCurrent, wantRecord: ProviderRecord{Plugin: CampAppPlugin, Version: "0.9.0"},
		},
		"dev recorded: preserved as unknown": {
			fsys: pluginCamp(oldSkill, oldHash, rec("dev")), offer: offered("0.4.0", newSkill),
			wantState: ShippedUnknown, wantRecord: ProviderRecord{Plugin: CampAppPlugin, Version: "dev"},
		},
		"dev offered, bundle missing: still added": {
			fsys: pluginCamp("", "", rec("0.4.0")), offer: offered("dev", newSkill),
			wantState: ShippedMissing, wantRecord: ProviderRecord{Plugin: CampAppPlugin, Version: "dev"},
		},
		"edited: modified": {
			fsys: pluginCamp("mine", oldHash, rec("0.3.0")), offer: offered("0.4.0", newSkill),
			wantState: ShippedModified, wantRecord: ProviderRecord{Plugin: CampAppPlugin, Version: "0.3.0"},
		},
		"unrecorded and differs: unknown despite git": {
			fsys: pluginCamp("mine", "", ""), offer: offered("0.4.0", newSkill),
			wantState: ShippedUnknown, wantRecord: ProviderRecord{},
		},
		"declined, automatic: left alone": {
			fsys: pluginCamp("", "", "    plugin: camp-app\n    declined: true\n"), offer: offered("0.4.0", newSkill), auto: true,
			wantState: ShippedDeclined, wantRecord: ProviderRecord{Plugin: CampAppPlugin, Declined: true},
		},
		"declined, named by a person: added and cleared": {
			fsys: pluginCamp("", "", "    plugin: camp-app\n    declined: true\n"), offer: offered("0.4.0", newSkill),
			wantState: ShippedMissing, wantRecord: ProviderRecord{Plugin: CampAppPlugin, Version: "0.4.0"},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) { runPluginCase(t, tc) })
	}
}

func TestPluginRules_IncomparableNoteNamesBothVersions(t *testing.T) {
	_, notes := runPluginCase(t, pluginCase{
		fsys:  pluginCamp(oldSkill, HashContent([]byte(oldSkill)), "    plugin: camp-app\n    version: 0.4.0\n"),
		offer: offered("dev", newSkill), wantState: ShippedUnknown,
		wantRecord: ProviderRecord{Plugin: CampAppPlugin, Version: "0.4.0"},
	})
	if n := notes["festival-app"]; !n.Incomparable || n.Offered != "dev" || n.Recorded != "0.4.0" {
		t.Fatalf("note = %+v, want incomparable dev vs 0.4.0", n)
	}
}

func TestActionFor_PluginStatesNeverWrite(t *testing.T) {
	for _, s := range []ShippedState{ShippedAhead, ShippedDeclined, ShippedProviderMissing} {
		for _, force := range []bool{false, true} {
			if a := actionFor(s, force); a != ActionKeep {
				t.Errorf("actionFor(%s, force=%v) = %s, want keep", s, force, a)
			}
		}
	}
}

func TestScopeBundles_PluginsOnlyWhenNamed(t *testing.T) {
	plugins := []PluginBundle{offered("0.4.0", newSkill)}
	scope, err := scopeBundles(shippedFixture(), plugins, nil)
	if err != nil || scope["festival-app"] {
		t.Fatalf("scope with no slugs = %v, %v; want camp bundles only", scope, err)
	}
	scope, err = scopeBundles(shippedFixture(), plugins, []string{"festival-app"})
	if err != nil || !scope["festival-app"] || scope["alpha"] {
		t.Fatalf("scope = %v, %v; want only festival-app", scope, err)
	}
}

func TestUpdate_AutoRefusesForce(t *testing.T) {
	_, err := Update(t.Context(), UpdateOptions{Root: "/nonexistent", Auto: true, Force: true})
	if err == nil || !strings.Contains(err.Error(), "--auto cannot be combined with --force") {
		t.Fatalf("Update(Auto, Force) error = %v", err)
	}
}

func fixedRunner(out []byte, err error) ProviderRunner {
	return func(context.Context, []string) ([]byte, error) { return out, err }
}

func TestAskProvider_Statuses(t *testing.T) {
	contract := readProviderFixture(t, "camp-app-contract.json")
	exit2 := camperrors.NewCommand("camp-app", 2, "", errors.New("exit status 2"))
	cases := map[string]struct {
		run  ProviderRunner
		want ProviderStatus
	}{
		"ok":                    {fixedRunner(contract, nil), ProviderOK},
		"not installed":         {fixedRunner(nil, ErrProviderNotInstalled), ProviderNotInstalled},
		"0.2.1 refusal, exit 2": {fixedRunner(readProviderFixture(t, "camp-app-0.2.1.json"), exit2), ProviderNoContract},
		"garbage, exit 2":       {fixedRunner([]byte("boom"), exit2), ProviderInvalid},
		"oversize output":       {fixedRunner(contract, plugin.ErrOutputTooLarge), ProviderInvalid},
		"failed to start":       {fixedRunner(nil, errors.New("exec format error")), ProviderInvalid},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			r, err := AskProvider(t.Context(), CampAppPlugin, tc.run, testCampSlugs)
			if err != nil || r.Status != tc.want {
				t.Fatalf("AskProvider() = %q (%s), %v; want %q", r.Status, r.Detail, err, tc.want)
			}
		})
	}
}

func TestAskProvider_AsksForSkillJSON(t *testing.T) {
	var got []string
	run := func(_ context.Context, args []string) ([]byte, error) {
		got = args
		return nil, ErrProviderNotInstalled
	}
	if _, err := AskProvider(t.Context(), CampAppPlugin, run, nil); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, []string{"skill", "--json"}) {
		t.Fatalf("args = %v, want skill --json", got)
	}
}

func blockingRunner(ctx context.Context, _ []string) ([]byte, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestAskProvider_DeadlineIsTimedOut(t *testing.T) {
	prev := providerDeadline
	providerDeadline = 20 * time.Millisecond
	t.Cleanup(func() { providerDeadline = prev })
	r, err := AskProvider(t.Context(), CampAppPlugin, blockingRunner, nil)
	if err != nil || r.Status != ProviderTimedOut {
		t.Fatalf("AskProvider() = %q, %v; want timed-out and no error", r.Status, err)
	}
}

func TestAskProvider_InterruptIsAnError(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	go func() { time.Sleep(20 * time.Millisecond); cancel() }()
	if _, err := AskProvider(ctx, CampAppPlugin, blockingRunner, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("AskProvider() error = %v, want context.Canceled", err)
	}
}

func TestObeyMarkerValid(t *testing.T) {
	cases := map[string]bool{
		"managed_by: obey\nkind: skills_projection\ncampaign_root: /x\n": true,
		"# note\n  managed_by:obey  \nkind: skills_projection":           true,
		"managed_by: obey-fork\nkind: skills_projection\n":               false,
		"managed_by: obey\n": false,
		"":                   false,
	}
	for content, want := range cases {
		if got := obeyMarkerValid(content); got != want {
			t.Errorf("obeyMarkerValid(%q) = %v, want %v", content, got, want)
		}
	}
}
