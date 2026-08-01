package stageguard

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

// rawRecord builds one `git diff --raw -z` record: metadata, NUL, path, NUL.
func rawRecord(srcMode, dstMode, srcOID, dstOID, status, path string) string {
	return ":" + srcMode + " " + dstMode + " " + srcOID + " " + dstOID + " " + status +
		"\x00" + path + "\x00"
}

const (
	oidA = "b6fc4c620b67d95f953a5c1c1230aaab5db5a1b0"
	oidB = "dc984463190c2b199b6003bd2e7589251742ea05"
	oidZ = "0000000000000000000000000000000000000000"
)

func TestParseDiffRawZ(t *testing.T) {
	tests := []struct {
		name string
		out  string
		want []stagedEntry
	}{
		{
			name: "modification and addition",
			out: rawRecord("100644", "100644", oidA, oidB, "M", "notes/one.md") +
				rawRecord("000000", "100644", oidZ, oidA, "A", "notes/two.md"),
			want: []stagedEntry{
				{Path: "notes/one.md", OID: oidB},
				{Path: "notes/two.md", OID: oidA},
			},
		},
		{
			name: "an executable file is still a regular file",
			out:  rawRecord("000000", "100755", oidZ, oidA, "A", "bin/run.sh"),
			want: []stagedEntry{{Path: "bin/run.sh", OID: oidA}},
		},
		{
			// A deletion has no destination content to size.
			name: "deletion is skipped",
			out:  rawRecord("100644", "000000", oidA, oidZ, "D", "gone.md"),
			want: []stagedEntry{},
		},
		{
			// The gitlink's object id names a commit in the submodule's object
			// database, not this repository's. Sizing it here is meaningless,
			// and the campaign root stages these routinely.
			name: "submodule gitlink is skipped",
			out:  rawRecord("000000", "160000", oidZ, oidA, "A", "projects/camp"),
			want: []stagedEntry{},
		},
		{
			// A symlink's blob is the target path, not the target's bytes.
			name: "symlink is skipped",
			out:  rawRecord("000000", "120000", oidZ, oidA, "A", "link"),
			want: []stagedEntry{},
		},
		{
			// -z output never quotes or escapes, so awkward names arrive whole.
			name: "path with a space and a quote survives verbatim",
			out:  rawRecord("000000", "100644", oidZ, oidA, "A", `my notes/it's "here".md`),
			want: []stagedEntry{{Path: `my notes/it's "here".md`, OID: oidA}},
		},
		{
			name: "empty output",
			out:  "",
			want: []stagedEntry{},
		},
		{
			name: "record with no leading colon is skipped",
			out:  "100644 100644 " + oidA + " " + oidB + " M\x00notes/one.md\x00",
			want: []stagedEntry{},
		},
		{
			name: "truncated record is skipped",
			out:  ":100644 100644\x00notes/one.md\x00",
			want: []stagedEntry{},
		},
		{
			name: "empty path is skipped",
			out:  rawRecord("000000", "100644", oidZ, oidA, "A", ""),
			want: []stagedEntry{},
		},
		{
			// One unusable record must not desynchronize the ones after it.
			name: "a skipped record does not lose the next one",
			out: rawRecord("000000", "160000", oidZ, oidA, "A", "projects/camp") +
				rawRecord("000000", "100644", oidZ, oidB, "A", "notes/two.md"),
			want: []stagedEntry{{Path: "notes/two.md", OID: oidB}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseDiffRawZ([]byte(tt.out))
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parseDiffRawZ() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestParseBatchCheck(t *testing.T) {
	tests := []struct {
		name string
		out  string
		want map[string]int64
	}{
		{
			name: "sizes are read per object",
			out:  oidA + " blob 31\n" + oidB + " blob 13891584\n",
			want: map[string]int64{oidA: 31, oidB: 13891584},
		},
		{
			// Absent is not zero. Recording it as zero bytes would silently
			// size an object camp cannot see, and it would pass every check.
			name: "a missing object is left out, not sized zero",
			out:  oidA + " missing\n" + oidB + " blob 40\n",
			want: map[string]int64{oidB: 40},
		},
		{
			name: "non-blob objects are ignored",
			out:  oidA + " commit 214\n" + oidB + " tree 96\n",
			want: map[string]int64{},
		},
		{
			name: "an unparseable size is ignored",
			out:  oidA + " blob notanumber\n",
			want: map[string]int64{},
		},
		{
			name: "empty output",
			out:  "",
			want: map[string]int64{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseBatchCheck([]byte(tt.out))
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parseBatchCheck() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestUniqueOIDs(t *testing.T) {
	entries := []stagedEntry{
		{Path: "a.md", OID: oidA},
		{Path: "b.md", OID: oidB},
		{Path: "copy-of-a.md", OID: oidA},
		{Path: "another-copy.md", OID: oidA},
	}

	got := uniqueOIDs(entries)
	want := []string{oidA, oidB}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("uniqueOIDs() = %v, want %v", got, want)
	}
}

func TestUniqueOIDsEmpty(t *testing.T) {
	if got := uniqueOIDs(nil); len(got) != 0 {
		t.Errorf("uniqueOIDs(nil) = %v, want empty", got)
	}
}

// The mode gate is what keeps a configured-off guard from paying for two git
// processes on every scoped commit. It must return before touching the
// repository at all, which a path that does not exist proves.
func TestCheckStagedIndexModeOffRunsNothing(t *testing.T) {
	limits := GuardLimits{MaxFileSize: 1, LargeFiles: ModeOff, Bulk: ModeBlock}

	violations, err := CheckStagedIndex(context.Background(), "/nonexistent/repo", limits)
	if err != nil {
		t.Fatalf("CheckStagedIndex() error = %v, want nil", err)
	}
	if len(violations) != 0 {
		t.Errorf("CheckStagedIndex() = %+v, want no violations", violations)
	}
}

func TestCheckStagedIndexHonorsContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	limits := GuardLimits{MaxFileSize: 1, LargeFiles: ModeAuto, Bulk: ModeBlock}
	if _, err := CheckStagedIndex(ctx, "/nonexistent/repo", limits); err == nil {
		t.Fatal("CheckStagedIndex() error = nil, want a cancellation error")
	}

	if _, err := EnumerateStaged(ctx, "/nonexistent/repo"); err == nil {
		t.Fatal("EnumerateStaged() error = nil, want a cancellation error")
	}
}

// Everything an index holds is tracked, which is the whole reason this pass can
// only ever report. If a staged entry could arrive as untracked, checkPerFile
// would classify it OverThreshold and the caller would exclude a file the user
// staged on purpose.
func TestStagedCandidatesAreAlwaysTrackedGrowth(t *testing.T) {
	limits := GuardLimits{MaxFileSize: 1 << 20, LargeFiles: ModeAuto, Bulk: ModeOff}
	candidates := []Candidate{
		{Path: "media/big.png", Size: 13 << 20},
		{Path: "notes/small.md", Size: 12},
	}

	violations, remaining := checkPerFile(candidates, limits)
	if len(violations) != 1 {
		t.Fatalf("checkPerFile() = %+v, want exactly one violation", violations)
	}
	if violations[0].Kind != TrackedGrowth {
		t.Errorf("violation kind = %q, want %q", violations[0].Kind, TrackedGrowth)
	}
	if !violations[0].ReportOnly() {
		t.Error("a staged entry's violation must be report-only")
	}
	if len(remaining) != len(candidates) {
		t.Errorf("remaining = %d, want %d: a reported entry is still committed",
			len(remaining), len(candidates))
	}
}

// The allowlist is the one line that makes a legitimate large file permanent,
// and it has to reach this pass too or a user who allowed a path still gets
// told about it on every scoped commit.
func TestStagedCandidatesRespectAllowlist(t *testing.T) {
	limits := GuardLimits{
		MaxFileSize: 1 << 20,
		LargeFiles:  ModeAuto,
		Bulk:        ModeOff,
		Allow:       []string{"*.psd"},
	}
	candidates := []Candidate{
		{Path: "art/cover.psd", Size: 40 << 20},
		{Path: "art/cover.png", Size: 13 << 20},
	}

	violations, _ := checkPerFile(filterAllowed(candidates, limits.Allow), limits)
	if len(violations) != 1 {
		t.Fatalf("checkPerFile() = %+v, want exactly one violation", violations)
	}
	if !strings.HasSuffix(violations[0].Path, ".png") {
		t.Errorf("violation path = %q, want the file the allowlist does not cover",
			violations[0].Path)
	}
}
