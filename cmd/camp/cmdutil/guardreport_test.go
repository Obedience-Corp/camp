package cmdutil

import (
	"strings"
	"testing"

	"github.com/Obedience-Corp/camp/internal/artifacts"
	"github.com/Obedience-Corp/camp/internal/git"
	"github.com/Obedience-Corp/camp/internal/stageguard"
)

func TestViolationNames(t *testing.T) {
	cases := []struct {
		name       string
		violations []stageguard.GuardViolation
		want       string
	}{
		{name: "none", violations: nil, want: ""},
		{
			name:       "one names the file",
			violations: []stageguard.GuardViolation{{Path: "videos/my-video/footage.mp4"}},
			want:       "footage.mp4",
		},
		{
			name: "two are both named",
			violations: []stageguard.GuardViolation{
				{Path: "a/footage.mp4"}, {Path: "a/broll.mp4"},
			},
			want: "footage.mp4, broll.mp4",
		},
		{
			// A root absorbing hundreds of files must not print hundreds of
			// names; the first plus a count carries the same information.
			name: "three or more abbreviate",
			violations: []stageguard.GuardViolation{
				{Path: "a/one.mp4"}, {Path: "a/two.mp4"}, {Path: "a/three.mp4"},
			},
			want: "one.mp4 and 2 more",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := violationNames(tc.violations); got != tc.want {
				t.Errorf("violationNames() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestLFSPattern(t *testing.T) {
	cases := []struct {
		name string
		path string
		want string
	}{
		{name: "keeps the directory, widens the name", path: "testdata/corpus.tar", want: "testdata/*.tar"},
		{name: "nested directory", path: "a/b/c/blob.bin", want: "a/b/c/*.bin"},
		{name: "root-level file", path: "corpus.tar", want: "*.tar"},
		{name: "no extension falls back to the path", path: "testdata/corpus", want: "testdata/corpus"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := lfsPattern(tc.path); got != tc.want {
				t.Errorf("lfsPattern(%q) = %q, want %q", tc.path, got, tc.want)
			}
		})
	}
}

// Each refusal explains the specific boundary that was hit. A generic message
// would leave the user guessing which rule applied, and therefore what to do.
func TestRefusalExplanation(t *testing.T) {
	cases := []struct {
		reason artifacts.RefusalReason
		want   string
	}{
		{reason: artifacts.RefusalCampaignRoot, want: "camp an artifact root"},
		{reason: artifacts.RefusalCampaignState, want: ".campaign/"},
		{reason: artifacts.RefusalRepoRoot, want: "repo boundary"},
		{reason: artifacts.RefusalEscapesCampaign, want: "camp will not create an artifact root"},
	}

	for _, tc := range cases {
		t.Run(string(tc.reason), func(t *testing.T) {
			got := refusalExplanation(tc.reason)
			if !strings.Contains(got, tc.want) {
				t.Errorf("refusalExplanation(%q) = %q, want it to mention %q", tc.reason, got, tc.want)
			}
		})
	}
}

// Tracked growth is reported but always committed, so it must not count as an
// exclusion; treating it as one would make camp claim a no-op commit while
// there was real content to commit.
func TestGuardHandlingExcludedAnything(t *testing.T) {
	cases := []struct {
		name     string
		handling *GuardHandling
		want     bool
	}{
		{name: "nil", handling: nil, want: false},
		{name: "empty", handling: &GuardHandling{}, want: false},
		{
			name: "tracked growth alone is not an exclusion",
			handling: &GuardHandling{
				TrackedGrowth: []stageguard.GuardViolation{{Path: "graph.png"}},
			},
			want: false,
		},
		{
			name: "a declared root with no files is not an exclusion",
			handling: &GuardHandling{
				Declared: []DeclaredRoot{{Root: "videos"}},
			},
			want: false,
		},
		{
			name: "a declared root holding files is",
			handling: &GuardHandling{
				Declared: []DeclaredRoot{{Root: "videos", Files: []stageguard.GuardViolation{{Path: "v.mp4"}}}},
			},
			want: true,
		},
		{
			name:     "a refusal is",
			handling: &GuardHandling{Refused: []RefusedFile{{Reason: reasonNeedsRootDecision}}},
			want:     true,
		},
		{
			name:     "a project exclusion is",
			handling: &GuardHandling{ProjectExcluded: []RefusedFile{{Reason: reasonProjectLargeFile}}},
			want:     true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.handling.ExcludedAnything(); got != tc.want {
				t.Errorf("ExcludedAnything() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestPluralFiles(t *testing.T) {
	cases := []struct {
		n    int
		want string
	}{
		{n: 1, want: "1 file"},
		{n: 2, want: "2 files"},
		{n: 8432, want: "8,432 files"},
	}
	for _, tc := range cases {
		if got := pluralFiles(tc.n); got != tc.want {
			t.Errorf("pluralFiles(%d) = %q, want %q", tc.n, got, tc.want)
		}
	}
}

// The bulk refusal must never name a directory camp was taught to recognize:
// the prefix comes from detection, so the same copy works for any vendor tree.
func TestRenderBulkRefusalInterpolatesDetectedPrefix(t *testing.T) {
	var out strings.Builder
	blocked := &git.GuardBlockedError{
		Kind: stageguard.Bulk,
		Violations: []stageguard.GuardViolation{{
			Kind:         stageguard.Bulk,
			CommonPrefix: "some/deep/vendor_dir",
			Count:        8432,
			TotalBytes:   61 << 20,
		}},
	}
	renderBulkRefusal(&out, blocked, "camp commit")

	got := out.String()
	for _, want := range []string{
		"8,432 untracked files",
		"some/deep/vendor_dir",
		"echo 'some/deep/vendor_dir/' >> .gitignore",
		"camp artifacts add some/deep/vendor_dir",
		"camp commit --commit-large",
		"Nothing was staged",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("bulk refusal missing %q; got:\n%s", want, got)
		}
	}
}
