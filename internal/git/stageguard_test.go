package git

import (
	"testing"

	"github.com/Obedience-Corp/camp/internal/stageguard"
)

// The guard only applies to sweep forms. An explicit `git add path/to/big.mp4`
// is an intent camp honors, so naming a file must switch the guard off.
func TestIsStageEverything(t *testing.T) {
	cases := []struct {
		name  string
		files []string
		want  bool
	}{
		{name: "nil is stage-everything", files: nil, want: true},
		{name: "empty slice is stage-everything", files: []string{}, want: true},
		{name: "bare dot", files: []string{"."}, want: true},
		{name: "separator and dot", files: []string{"--", "."}, want: true},
		{
			name:  "StageAllExcluding form is still a sweep",
			files: []string{"--", ".", ":(exclude,literal)projects/alpha"},
			want:  true,
		},
		{
			name:  "several exclusions are still a sweep",
			files: []string{"--", ".", ":(exclude,literal)a", ":(exclude,literal)b"},
			want:  true,
		},
		{name: "one explicit file is not a sweep", files: []string{"notes.md"}, want: false},
		{
			name:  "explicit file after separator is not a sweep",
			files: []string{"--", "videos/footage.mp4"},
			want:  false,
		},
		{
			name:  "explicit file mixed with exclusions is not a sweep",
			files: []string{"--", ".", "notes.md", ":(exclude,literal)a"},
			want:  false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isStageEverything(tc.files); got != tc.want {
				t.Errorf("isStageEverything(%q) = %v, want %v", tc.files, got, tc.want)
			}
		})
	}
}

func TestApplyGuardExclusions(t *testing.T) {
	cases := []struct {
		name     string
		files    []string
		excluded []string
		want     []string
	}{
		{
			name:     "no exclusions leaves the pathspec alone",
			files:    nil,
			excluded: nil,
			want:     nil,
		},
		{
			name:     "exclusions on a bare sweep materialize the pathspec",
			files:    nil,
			excluded: []string{"videos/footage.mp4"},
			want:     []string{"--", ".", ":(exclude,literal)videos/footage.mp4"},
		},
		{
			name:     "guard exclusions compose with caller exclusions",
			files:    []string{"--", ".", ":(exclude,literal)projects/alpha"},
			excluded: []string{"videos/footage.mp4"},
			want: []string{
				"--", ".",
				":(exclude,literal)projects/alpha",
				":(exclude,literal)videos/footage.mp4",
			},
		},
		{
			name:     "several guard exclusions all land",
			files:    nil,
			excluded: []string{"a.bin", "b.bin"},
			want:     []string{"--", ".", ":(exclude,literal)a.bin", ":(exclude,literal)b.bin"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := applyGuardExclusions(tc.files, tc.excluded)
			if len(got) != len(tc.want) {
				t.Fatalf("applyGuardExclusions() = %q, want %q", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("applyGuardExclusions()[%d] = %q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

// Composition is the property that matters at the campaign root, where the
// commit path always excludes submodule refs: the caller's exclusions must
// survive the guard adding its own.
func TestApplyGuardExclusionsPreservesCallerExclusions(t *testing.T) {
	files := []string{"--", ".", ":(exclude,literal)projects/alpha", ":(exclude,literal)projects/beta"}
	got := applyGuardExclusions(files, []string{"media/huge.bin"})

	var sawAlpha, sawBeta, sawGuard bool
	for _, f := range got {
		switch f {
		case ":(exclude,literal)projects/alpha":
			sawAlpha = true
		case ":(exclude,literal)projects/beta":
			sawBeta = true
		case ":(exclude,literal)media/huge.bin":
			sawGuard = true
		}
	}
	if !sawAlpha || !sawBeta || !sawGuard {
		t.Errorf("applyGuardExclusions() = %q; want all caller and guard exclusions present", got)
	}
}

func TestStageOutcomeEmptyAndPaths(t *testing.T) {
	var nilOutcome *StageOutcome
	if !nilOutcome.Empty() {
		t.Error("nil outcome Empty() = false, want true")
	}
	if paths := nilOutcome.ExcludedPaths(); paths != nil {
		t.Errorf("nil outcome ExcludedPaths() = %v, want nil", paths)
	}

	empty := &StageOutcome{}
	if !empty.Empty() {
		t.Error("zero outcome Empty() = false, want true")
	}

	reportedOnly := &StageOutcome{
		Reported: []stageguard.GuardViolation{{Kind: stageguard.TrackedGrowth, Path: "graph.png"}},
	}
	if reportedOnly.Empty() {
		t.Error("outcome with a tracked-growth report Empty() = true, want false")
	}
	if paths := reportedOnly.ExcludedPaths(); len(paths) != 0 {
		t.Errorf("ExcludedPaths() = %v; tracked growth is never excluded", paths)
	}

	excluded := &StageOutcome{
		Excluded: []stageguard.GuardViolation{
			{Kind: stageguard.OverThreshold, Path: "a.bin"},
			{Kind: stageguard.OverThreshold, Path: "b.bin"},
		},
	}
	paths := excluded.ExcludedPaths()
	if len(paths) != 2 || paths[0] != "a.bin" || paths[1] != "b.bin" {
		t.Errorf("ExcludedPaths() = %v, want [a.bin b.bin]", paths)
	}
}

func TestGuardBlockedErrorMessage(t *testing.T) {
	cases := []struct {
		name string
		err  *GuardBlockedError
		want string
	}{
		{
			name: "bulk names the directory and count",
			err: &GuardBlockedError{
				Kind: stageguard.Bulk,
				Violations: []stageguard.GuardViolation{
					{Kind: stageguard.Bulk, CommonPrefix: "node_modules", Count: 8432},
				},
			},
			want: "staging refused: node_modules would add 8432 untracked files",
		},
		{
			name: "over threshold lists the paths",
			err: &GuardBlockedError{
				Kind: stageguard.OverThreshold,
				Violations: []stageguard.GuardViolation{
					{Kind: stageguard.OverThreshold, Path: "a.bin"},
					{Kind: stageguard.OverThreshold, Path: "b.bin"},
				},
			},
			want: "staging refused: files over the size limit: a.bin, b.bin",
		},
		{
			name: "bulk kind with no bulk violation still renders",
			err:  &GuardBlockedError{Kind: stageguard.Bulk},
			want: "staging refused: bulk directory",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.err.Error(); got != tc.want {
				t.Errorf("Error() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestItoa(t *testing.T) {
	cases := []struct {
		in   int
		want string
	}{
		{in: 0, want: "0"},
		{in: 7, want: "7"},
		{in: 1000, want: "1000"},
		{in: 8432, want: "8432"},
	}
	for _, tc := range cases {
		if got := itoa(tc.in); got != tc.want {
			t.Errorf("itoa(%d) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
