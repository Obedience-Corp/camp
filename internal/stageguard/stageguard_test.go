package stageguard

import (
	"fmt"
	"testing"
)

// testLimits is the shipped campaign-root policy, used unless a case overrides
// a field.
func testLimits() GuardLimits {
	return GuardLimits{
		MaxFileSize:   DefaultMaxFileSize,
		MaxAddedFiles: DefaultMaxAddedFiles,
		LargeFiles:    ModeAuto,
		Bulk:          ModeBlock,
	}
}

func TestCheckPerFileThresholdEdges(t *testing.T) {
	cases := []struct {
		name      string
		size      int64
		untracked bool
		wantKinds []ViolationKind
	}{
		{
			name:      "one byte under the threshold is clean",
			size:      DefaultMaxFileSize - 1,
			untracked: true,
			wantKinds: nil,
		},
		{
			name:      "exactly at the threshold is clean",
			size:      DefaultMaxFileSize,
			untracked: true,
			wantKinds: nil,
		},
		{
			name:      "one byte over the threshold is a violation",
			size:      DefaultMaxFileSize + 1,
			untracked: true,
			wantKinds: []ViolationKind{OverThreshold},
		},
		{
			name:      "tracked file at the threshold is clean",
			size:      DefaultMaxFileSize,
			untracked: false,
			wantKinds: nil,
		},
		{
			name:      "tracked file over the threshold reports growth",
			size:      DefaultMaxFileSize + 1,
			untracked: false,
			wantKinds: []ViolationKind{TrackedGrowth},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			candidates := []Candidate{{Path: "asset.bin", Size: tc.size, Untracked: tc.untracked}}
			violations, _ := checkPerFile(candidates, testLimits())
			assertKinds(t, violations, tc.wantKinds)
		})
	}
}

// A tracked file that grew past the threshold is reported but stays in the
// batch: excluding it would leave the stale blob in git while the new bytes on
// disk belonged to nothing.
func TestCheckPerFileTrackedGrowthIsNeverExcluded(t *testing.T) {
	candidates := []Candidate{
		{Path: "graphs/campaign-graph.png", Size: 13 << 20, Untracked: false},
		{Path: "footage.mp4", Size: 512 << 20, Untracked: true},
	}

	violations, remaining := checkPerFile(candidates, testLimits())
	assertKinds(t, violations, []ViolationKind{TrackedGrowth, OverThreshold})

	if len(remaining) != 1 || remaining[0].Path != "graphs/campaign-graph.png" {
		t.Fatalf("remaining = %v, want only the tracked file", remaining)
	}
}

// Criterion 37: size decides, never extension.
func TestCheckPerFileSizeDecidesNotExtension(t *testing.T) {
	cases := []struct {
		name          string
		path          string
		size          int64
		wantViolation bool
	}{
		{name: "200 KB mp4 is ordinary content", path: "clips/teaser.mp4", size: 200 << 10, wantViolation: false},
		{name: "200 KB mov is ordinary content", path: "clips/teaser.mov", size: 200 << 10, wantViolation: false},
		{name: "large json is a violation", path: "data/dump.json", size: 512 << 20, wantViolation: true},
		{name: "large markdown is a violation", path: "notes/huge.md", size: 64 << 20, wantViolation: true},
		{name: "large mp4 is a violation", path: "clips/footage.mp4", size: 512 << 20, wantViolation: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			candidates := []Candidate{{Path: tc.path, Size: tc.size, Untracked: true}}
			violations, _ := checkPerFile(candidates, testLimits())
			if got := len(violations) > 0; got != tc.wantViolation {
				t.Errorf("violation = %v, want %v (size %d)", got, tc.wantViolation, tc.size)
			}
		})
	}
}

func TestCheckPerFileModeOffDisablesDetection(t *testing.T) {
	limits := testLimits()
	limits.LargeFiles = ModeOff

	candidates := []Candidate{{Path: "footage.mp4", Size: 512 << 20, Untracked: true}}
	violations, remaining := checkPerFile(candidates, limits)

	if len(violations) != 0 {
		t.Errorf("violations = %v, want none when large_files is off", violations)
	}
	if len(remaining) != 1 {
		t.Errorf("remaining = %d candidates, want 1", len(remaining))
	}
}

func TestDetectBulk(t *testing.T) {
	cases := []struct {
		name       string
		candidates []Candidate
		limits     func() GuardLimits
		wantBulk   bool
		wantPrefix string
		wantCount  int
	}{
		{
			name:       "999 files under one prefix is under the trigger",
			candidates: bulkCandidates("node_modules", 999, 1<<10),
			wantBulk:   false,
		},
		{
			name:       "1000 files under one prefix trips the guard",
			candidates: bulkCandidates("node_modules", 1000, 1<<10),
			wantBulk:   true,
			wantPrefix: "node_modules",
			wantCount:  1000,
		},
		{
			name: "split across prefixes does not trip",
			candidates: append(
				bulkCandidates("src", 600, 1<<10),
				bulkCandidates("docs", 600, 1<<10)...,
			),
			wantBulk: false,
		},
		{
			name: "one dominant prefix among stragglers still trips",
			candidates: append(
				bulkCandidates("node_modules", 1200, 1<<10),
				bulkCandidates("src", 20, 1<<10)...,
			),
			wantBulk:   true,
			wantPrefix: "node_modules",
			wantCount:  1200,
		},
		{
			name:       "tracked modifications never count toward bulk",
			candidates: trackedCandidates("src", 2000, 1<<10),
			wantBulk:   false,
		},
		{
			name:       "bulk off disables the guard",
			candidates: bulkCandidates("node_modules", 5000, 1<<10),
			limits: func() GuardLimits {
				l := testLimits()
				l.Bulk = ModeOff
				return l
			},
			wantBulk: false,
		},
		{
			name:       "files at the repo root have no prefix to dominate",
			candidates: rootCandidates(1500, 1<<10),
			wantBulk:   false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			limits := testLimits()
			if tc.limits != nil {
				limits = tc.limits()
			}
			violation, found := detectBulk(tc.candidates, limits)
			if found != tc.wantBulk {
				t.Fatalf("detectBulk() found = %v, want %v", found, tc.wantBulk)
			}
			if !found {
				return
			}
			if violation.CommonPrefix != tc.wantPrefix {
				t.Errorf("CommonPrefix = %q, want %q", violation.CommonPrefix, tc.wantPrefix)
			}
			if violation.Count != tc.wantCount {
				t.Errorf("Count = %d, want %d", violation.Count, tc.wantCount)
			}
			if violation.TotalBytes != int64(tc.wantCount)*(1<<10) {
				t.Errorf("TotalBytes = %d, want %d", violation.TotalBytes, int64(tc.wantCount)*(1<<10))
			}
		})
	}
}

// The per-file guard runs first and the bulk guard counts only what remains.
// Otherwise one auto-handled 512 MB video would swell the batch back over the
// trigger and turn the flagship scenario into a block.
func TestPerFileGuardRunsBeforeBulk(t *testing.T) {
	candidates := append(
		[]Candidate{{Path: "videos/footage.mp4", Size: 512 << 20, Untracked: true}},
		bulkCandidates("videos", 999, 1<<10)...,
	)

	violations, remaining := checkPerFile(candidates, testLimits())
	assertKinds(t, violations, []ViolationKind{OverThreshold})

	if _, found := detectBulk(remaining, testLimits()); found {
		t.Error("detectBulk() tripped on 999 remaining files; the over-threshold file must not count")
	}
}

func TestDetectBulkFindsDeepestDominantPrefix(t *testing.T) {
	candidates := bulkCandidates("projects/webapp/node_modules", 2000, 1<<10)

	violation, found := detectBulk(candidates, testLimits())
	if !found {
		t.Fatal("detectBulk() found = false, want true")
	}
	if violation.CommonPrefix != "projects/webapp/node_modules" {
		t.Errorf("CommonPrefix = %q, want the deepest dominant directory", violation.CommonPrefix)
	}
}

func TestMatchesAllow(t *testing.T) {
	allow := []string{
		"docs/handbook.pdf",
		"testdata/**",
		"*.psd",
		"assets/*.bin",
	}

	cases := []struct {
		name string
		path string
		want bool
	}{
		{name: "exact path matches", path: "docs/handbook.pdf", want: true},
		{name: "exact path does not match a sibling", path: "docs/other.pdf", want: false},
		{name: "double star matches nested content", path: "testdata/corpus/a/b.tar", want: true},
		{name: "double star matches the directory itself", path: "testdata", want: true},
		{name: "double star does not match a prefix sibling", path: "testdata-extra/file.tar", want: false},
		{name: "unanchored extension glob matches at the root", path: "cover.psd", want: true},
		{name: "unanchored extension glob matches at depth", path: "art/layers/cover.psd", want: true},
		{name: "extension glob does not match another extension", path: "art/cover.png", want: false},
		{name: "anchored glob matches its own directory", path: "assets/blob.bin", want: true},
		{name: "anchored glob does not match deeper", path: "assets/nested/blob.bin", want: false},
		{name: "unrelated path", path: "src/main.go", want: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := MatchesAllow(tc.path, allow); got != tc.want {
				t.Errorf("MatchesAllow(%q) = %v, want %v", tc.path, got, tc.want)
			}
		})
	}
}

func TestMatchesAllowEmptyAndBlankPatterns(t *testing.T) {
	if MatchesAllow("any/path.bin", nil) {
		t.Error("MatchesAllow() with no patterns = true, want false")
	}
	if MatchesAllow("any/path.bin", []string{"", "   "}) {
		t.Error("MatchesAllow() with blank patterns = true, want false")
	}
}

// An allowlisted file is exempt from every guard, so it is neither reported nor
// counted toward the bulk trigger.
func TestFilterAllowedRemovesBeforeAnyGuard(t *testing.T) {
	candidates := []Candidate{
		{Path: "releases/camp-darwin-arm64", Size: 60 << 20, Untracked: true},
		{Path: "notes.md", Size: 4 << 10, Untracked: true},
	}

	kept := filterAllowed(candidates, []string{"releases/**"})
	if len(kept) != 1 || kept[0].Path != "notes.md" {
		t.Fatalf("filterAllowed() = %v, want only notes.md", kept)
	}

	violations, _ := checkPerFile(kept, testLimits())
	if len(violations) != 0 {
		t.Errorf("violations = %v, want none for an allowlisted large file", violations)
	}
}

func TestGuardViolationReportOnly(t *testing.T) {
	cases := []struct {
		kind ViolationKind
		want bool
	}{
		{kind: TrackedGrowth, want: true},
		{kind: OverThreshold, want: false},
		{kind: Bulk, want: false},
	}

	for _, tc := range cases {
		t.Run(string(tc.kind), func(t *testing.T) {
			if got := (GuardViolation{Kind: tc.kind}).ReportOnly(); got != tc.want {
				t.Errorf("ReportOnly() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestGuardViolationBlocks(t *testing.T) {
	cases := []struct {
		name   string
		kind   ViolationKind
		limits GuardLimits
		want   bool
	}{
		{
			name:   "bulk blocks in block mode",
			kind:   Bulk,
			limits: GuardLimits{Bulk: ModeBlock},
			want:   true,
		},
		{
			name:   "over threshold does not block in auto mode",
			kind:   OverThreshold,
			limits: GuardLimits{LargeFiles: ModeAuto},
			want:   false,
		},
		{
			name:   "over threshold blocks in block mode",
			kind:   OverThreshold,
			limits: GuardLimits{LargeFiles: ModeBlock},
			want:   true,
		},
		{
			name:   "tracked growth never blocks",
			kind:   TrackedGrowth,
			limits: GuardLimits{LargeFiles: ModeBlock},
			want:   false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := (GuardViolation{Kind: tc.kind}).Blocks(tc.limits); got != tc.want {
				t.Errorf("Blocks() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestModeValid(t *testing.T) {
	cases := []struct {
		mode Mode
		want bool
	}{
		{mode: ModeAuto, want: true},
		{mode: ModeBlock, want: true},
		{mode: ModeOff, want: true},
		{mode: Mode(""), want: false},
		{mode: Mode("warn"), want: false},
	}

	for _, tc := range cases {
		t.Run(string(tc.mode), func(t *testing.T) {
			if got := tc.mode.Valid(); got != tc.want {
				t.Errorf("Mode(%q).Valid() = %v, want %v", tc.mode, got, tc.want)
			}
		})
	}
}

func TestSortViolations(t *testing.T) {
	violations := []GuardViolation{
		{Kind: OverThreshold, Path: "small.bin", Size: 20 << 20},
		{Kind: Bulk, CommonPrefix: "node_modules", Count: 8000},
		{Kind: OverThreshold, Path: "big.bin", Size: 512 << 20},
	}

	SortViolations(violations)

	if violations[0].Kind != Bulk {
		t.Errorf("first = %v, want the blocking bulk violation", violations[0].Kind)
	}
	if violations[1].Path != "big.bin" {
		t.Errorf("second = %q, want the largest per-file violation", violations[1].Path)
	}
}

func assertKinds(t *testing.T, violations []GuardViolation, want []ViolationKind) {
	t.Helper()
	if len(violations) != len(want) {
		t.Fatalf("violations = %v, want %d of kinds %v", violations, len(want), want)
	}
	for i, kind := range want {
		if violations[i].Kind != kind {
			t.Errorf("violations[%d].Kind = %q, want %q", i, violations[i].Kind, kind)
		}
	}
}

func bulkCandidates(prefix string, n int, size int64) []Candidate {
	candidates := make([]Candidate, 0, n)
	for i := range n {
		candidates = append(candidates, Candidate{
			Path:      fmt.Sprintf("%s/file%04d.js", prefix, i),
			Size:      size,
			Untracked: true,
		})
	}
	return candidates
}

func trackedCandidates(prefix string, n int, size int64) []Candidate {
	candidates := bulkCandidates(prefix, n, size)
	for i := range candidates {
		candidates[i].Untracked = false
	}
	return candidates
}

func rootCandidates(n int, size int64) []Candidate {
	candidates := make([]Candidate, 0, n)
	for i := range n {
		candidates = append(candidates, Candidate{
			Path:      fmt.Sprintf("file%04d.js", i),
			Size:      size,
			Untracked: true,
		})
	}
	return candidates
}

func TestValidateAllow(t *testing.T) {
	cases := []struct {
		name    string
		allow   []string
		wantErr bool
	}{
		{name: "unclosed character class", allow: []string{"docs/[a-.pdf"}, wantErr: true},
		{name: "bad pattern among good ones", allow: []string{"*.psd", "[", "docs/**"}, wantErr: true},
		{name: "nil allowlist", allow: nil, wantErr: false},
		{name: "blank entries are skipped", allow: []string{"", "   "}, wantErr: false},
		{name: "exact path", allow: []string{"docs/handbook.pdf"}, wantErr: false},
		{name: "double star suffix", allow: []string{"testdata/**"}, wantErr: false},
		{name: "extension glob", allow: []string{"*.psd"}, wantErr: false},
		{name: "character class", allow: []string{"assets/[abc]*.bin"}, wantErr: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateAllow(tc.allow)
			if tc.wantErr && err == nil {
				t.Fatal("ValidateAllow() = nil, want error")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("ValidateAllow() error = %v", err)
			}
		})
	}
}
