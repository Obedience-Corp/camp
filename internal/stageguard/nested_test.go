package stageguard

import (
	"testing"

	"github.com/Obedience-Corp/camp/internal/config"
)

// Mode.Valid recognizes every mode the package defines, and each guard is
// responsible for narrowing that to its own subset. These are separate
// concerns and this test pins the first one only.
func TestModeValidRecognizesExclude(t *testing.T) {
	for _, mode := range []Mode{ModeAuto, ModeExclude, ModeBlock, ModeOff} {
		if !mode.Valid() {
			t.Errorf("Mode(%q).Valid() = false, want true", mode)
		}
	}
	for _, mode := range []Mode{"", "auto ", "EXCLUDE", "warn"} {
		if mode.Valid() {
			t.Errorf("Mode(%q).Valid() = true, want false", mode)
		}
	}
}

// A nested-repository violation blocks only under block mode. Under the
// shipped default it is excluded and reported, which is not a block.
func TestNestedRepoBlocksOnlyUnderBlockMode(t *testing.T) {
	violation := GuardViolation{Kind: NestedRepo, Path: "review/app-pr136"}

	tests := []struct {
		mode Mode
		want bool
	}{
		{ModeExclude, false},
		{ModeOff, false},
		{ModeBlock, true},
	}
	for _, tt := range tests {
		got := violation.Blocks(GuardLimits{NestedRepos: tt.mode})
		if got != tt.want {
			t.Errorf("Blocks() under %q = %v, want %v", tt.mode, got, tt.want)
		}
	}
}

// A nested repository is excluded, never merely reported. ReportOnly is what
// distinguishes tracked growth, which is always committed.
func TestNestedRepoIsNotReportOnly(t *testing.T) {
	if (GuardViolation{Kind: NestedRepo}).ReportOnly() {
		t.Error("NestedRepo must be excluded, not report-only")
	}
}

// Nested repositories sort above per-file findings despite carrying no size.
// Ordering by size alone would file the finding most likely to break the
// repository below every large file in the report.
func TestSortViolationsPlacesNestedReposAboveFiles(t *testing.T) {
	violations := []GuardViolation{
		{Kind: OverThreshold, Path: "big.bin", Size: 900},
		{Kind: NestedRepo, Path: "review/app-pr136"},
		{Kind: Bulk, CommonPrefix: "vendor", Count: 1200},
		{Kind: OverThreshold, Path: "bigger.bin", Size: 5000},
	}
	SortViolations(violations)

	want := []ViolationKind{Bulk, NestedRepo, OverThreshold, OverThreshold}
	for i, kind := range want {
		if violations[i].Kind != kind {
			t.Fatalf("position %d = %q, want %q (order: %v)", i, violations[i].Kind, kind, kinds(violations))
		}
	}
	// Within the per-file group the existing descending-size rule still holds.
	if violations[2].Path != "bigger.bin" {
		t.Errorf("per-file findings lost their size ordering: got %q first", violations[2].Path)
	}
}

// Multiple nested repositories keep a deterministic order, so the report does
// not reshuffle between runs on the same working tree.
func TestSortViolationsOrdersNestedReposByPath(t *testing.T) {
	violations := []GuardViolation{
		{Kind: NestedRepo, Path: "review/c"},
		{Kind: NestedRepo, Path: "review/a"},
		{Kind: NestedRepo, Path: "review/b"},
	}
	SortViolations(violations)

	for i, want := range []string{"review/a", "review/b", "review/c"} {
		if violations[i].Path != want {
			t.Errorf("position %d = %q, want %q", i, violations[i].Path, want)
		}
	}
}

// The allowlist applies to nested repositories too, so a workflow that
// deliberately keeps one checkout in place can exempt that path alone rather
// than turning the whole guard off.
func TestNestedRepoRespectsAllowGlobs(t *testing.T) {
	tests := []struct {
		name  string
		path  string
		allow []string
		want  bool
	}{
		{"exact path", "review/app-pr136", []string{"review/app-pr136"}, true},
		{"directory tree", "review/app-pr136", []string{"review/**"}, true},
		{"unrelated glob", "review/app-pr136", []string{"vendor/**"}, false},
		{"no allowlist", "review/app-pr136", nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := MatchesAllow(tt.path, tt.allow); got != tt.want {
				t.Errorf("MatchesAllow(%q, %v) = %v, want %v", tt.path, tt.allow, got, tt.want)
			}
		})
	}
}

// .gitmodules is hand-edited, and git config hands back whatever spelling it
// finds there, while git status always reports the normalized one. A declared
// submodule written "./vendor/lib" must still match the "vendor/lib" status
// reports, or the guard excludes a submodule the user deliberately declared.
func TestNormalizeDeclaredPathMatchesStatusSpelling(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  string
	}{
		{"already normalized", "vendor/lib", "vendor/lib"},
		{"dot-slash prefix", "./vendor/lib", "vendor/lib"},
		{"trailing slash", "vendor/lib/", "vendor/lib"},
		{"doubled separator", "vendor//lib", "vendor/lib"},
		{"surrounding space", "  vendor/lib  ", "vendor/lib"},
		{"interior dot segment", "vendor/./lib", "vendor/lib"},
		{"repository root declares nothing", "./", ""},
		{"empty declares nothing", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeDeclaredPath(tt.value); got != tt.want {
				t.Errorf("normalizeDeclaredPath(%q) = %q, want %q", tt.value, got, tt.want)
			}
		})
	}
}

// The shipped default is exclude in both scopes. A regression that left this
// unset would resolve to the zero Mode, which is neither off nor exclude and
// would silently change what the guard does, so it is pinned here rather than
// only exercised end to end.
func TestNestedReposDefaultsToExcludeInBothScopes(t *testing.T) {
	for _, scopeProject := range []bool{false, true} {
		limits := defaultLimits(scopeProject)
		if limits.NestedRepos != ModeExclude {
			t.Errorf("defaultLimits(scopeProject=%v).NestedRepos = %q, want %q",
				scopeProject, limits.NestedRepos, ModeExclude)
		}
	}
}

// Each guard validates its own subset. A mode another guard accepts is a
// configuration error here, not a value that quietly resolves to something
// else.
func TestNestedReposModeValidation(t *testing.T) {
	tests := []struct {
		mode    string
		wantErr bool
		want    Mode
	}{
		{"", false, ModeExclude}, // unset keeps the default
		{"exclude", false, ModeExclude},
		{"block", false, ModeBlock},
		{"off", false, ModeOff},
		{"auto", true, ""}, // valid for large_files, meaningless here
		{"nonsense", true, ""},
	}
	for _, tt := range tests {
		t.Run("nested_repos="+tt.mode, func(t *testing.T) {
			limits, err := applyGuardsConfig(guardsConfigWithNested(tt.mode), false, "")
			if tt.wantErr {
				if err == nil {
					t.Fatalf("applyGuardsConfig(%q) = nil error, want a validation error", tt.mode)
				}
				return
			}
			if err != nil {
				t.Fatalf("applyGuardsConfig(%q) returned %v, want nil", tt.mode, err)
			}
			if limits.NestedRepos != tt.want {
				t.Errorf("NestedRepos = %q, want %q", limits.NestedRepos, tt.want)
			}
		})
	}
}

// large_files must keep rejecting exclude. Adding ModeExclude to Mode.Valid
// would otherwise let it through to a guard that has no handling for it.
func TestLargeFilesRejectsExcludeMode(t *testing.T) {
	cfg := config.GuardsConfig{LargeFiles: string(ModeExclude)}
	if _, err := applyGuardsConfig(cfg, false, ""); err == nil {
		t.Fatal("large_files accepted \"exclude\", want a validation error")
	}
}

func guardsConfigWithNested(mode string) config.GuardsConfig {
	return config.GuardsConfig{NestedRepos: mode}
}

func kinds(violations []GuardViolation) []ViolationKind {
	out := make([]ViolationKind, 0, len(violations))
	for _, v := range violations {
		out = append(out, v.Kind)
	}
	return out
}
