package stageguard

import (
	"strings"
	"testing"
)

// nulJoin builds porcelain -z output: every record is NUL-terminated.
func nulJoin(records ...string) []byte {
	return []byte(strings.Join(records, "\x00") + "\x00")
}

func TestParseStatusPorcelainZMalformed(t *testing.T) {
	cases := []struct {
		name string
		out  []byte
	}{
		{name: "empty output", out: nil},
		{name: "only NULs", out: []byte("\x00\x00\x00")},
		{name: "record shorter than a status code", out: nulJoin("?")},
		{name: "record with a code but no path", out: nulJoin("?? ")},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseStatusPorcelainZ(tc.out); len(got) != 0 {
				t.Errorf("parseStatusPorcelainZ() = %v, want no entries", got)
			}
		})
	}
}

func TestParseStatusPorcelainZ(t *testing.T) {
	cases := []struct {
		name      string
		out       []byte
		wantCodes []string
		wantPaths []string
	}{
		{
			name:      "untracked file",
			out:       nulJoin("?? videos/footage.mp4"),
			wantCodes: []string{"??"},
			wantPaths: []string{"videos/footage.mp4"},
		},
		{
			name:      "worktree modification keeps its leading space",
			out:       nulJoin(" M internal/git/commit.go"),
			wantCodes: []string{" M"},
			wantPaths: []string{"internal/git/commit.go"},
		},
		{
			name:      "staged modification",
			out:       nulJoin("M  internal/git/commit.go"),
			wantCodes: []string{"M "},
			wantPaths: []string{"internal/git/commit.go"},
		},
		{
			name: "rename consumes the trailing source path",
			// -z emits the destination first, then the source as its own record.
			out:       nulJoin("R  new/name.go", "old/name.go", "?? after.txt"),
			wantCodes: []string{"R ", "??"},
			wantPaths: []string{"new/name.go", "after.txt"},
		},
		{
			name:      "copy also consumes its source path",
			out:       nulJoin("C  copy.go", "origin.go", "?? after.txt"),
			wantCodes: []string{"C ", "??"},
			wantPaths: []string{"copy.go", "after.txt"},
		},
		{
			name:      "mixed records",
			out:       nulJoin("?? a.txt", " M b.txt", "D  c.txt"),
			wantCodes: []string{"??", " M", "D "},
			wantPaths: []string{"a.txt", "b.txt", "c.txt"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			entries := parseStatusPorcelainZ(tc.out)
			if len(entries) != len(tc.wantCodes) {
				t.Fatalf("parseStatusPorcelainZ() = %v, want %d entries", entries, len(tc.wantCodes))
			}
			for i := range entries {
				if entries[i].Code != tc.wantCodes[i] {
					t.Errorf("entries[%d].Code = %q, want %q", i, entries[i].Code, tc.wantCodes[i])
				}
				if entries[i].Path != tc.wantPaths[i] {
					t.Errorf("entries[%d].Path = %q, want %q", i, entries[i].Path, tc.wantPaths[i])
				}
			}
		})
	}
}

// In -z mode git never quotes or escapes a path, which is the reason this
// package asks for it. A path that git would render as "\"odd name\"" in the
// default format arrives here verbatim and must not be unquoted or trimmed.
func TestParseStatusPorcelainZDoesNotUnquotePaths(t *testing.T) {
	cases := []struct {
		name string
		path string
	}{
		{name: "spaces", path: "my videos/final cut.mp4"},
		{name: "double quotes", path: `weird/"quoted".txt`},
		{name: "backslash", path: `weird/back\slash.txt`},
		{name: "non-ascii", path: "notes/café.md"},
		{name: "leading dash", path: "-dashfile.txt"},
		{name: "newline in name", path: "weird/two\nlines.txt"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			entries := parseStatusPorcelainZ(nulJoin("?? " + tc.path))
			if len(entries) != 1 {
				t.Fatalf("parseStatusPorcelainZ() = %v, want 1 entry", entries)
			}
			if entries[0].Path != tc.path {
				t.Errorf("Path = %q, want %q verbatim", entries[0].Path, tc.path)
			}
		})
	}
}

func TestClassifyEntry(t *testing.T) {
	cases := []struct {
		name          string
		code          string
		path          string
		wantOK        bool
		wantUntracked bool
	}{
		{name: "empty path is not a candidate", code: "??", path: "", wantOK: false},
		{name: "short code is not a candidate", code: "?", path: "a.txt", wantOK: false},
		{name: "deleted from worktree", code: " D", path: "a.txt", wantOK: false},
		{name: "deleted from index", code: "D ", path: "a.txt", wantOK: false},
		{name: "unmerged both modified", code: "UU", path: "a.txt", wantOK: false},
		{name: "unmerged both added", code: "AA", path: "a.txt", wantOK: false},
		{name: "unmerged both deleted", code: "DD", path: "a.txt", wantOK: false},
		{name: "unmerged added by us", code: "AU", path: "a.txt", wantOK: false},
		{name: "ignored", code: "!!", path: "a.txt", wantOK: false},
		{name: "untracked", code: "??", path: "a.txt", wantOK: true, wantUntracked: true},
		{name: "worktree modification is tracked", code: " M", path: "a.txt", wantOK: true, wantUntracked: false},
		{name: "staged modification is tracked", code: "M ", path: "a.txt", wantOK: true, wantUntracked: false},
		{name: "staged and worktree modification is tracked", code: "MM", path: "a.txt", wantOK: true, wantUntracked: false},
		{name: "added to index is tracked", code: "A ", path: "a.txt", wantOK: true, wantUntracked: false},
		{name: "rename is tracked", code: "R ", path: "a.txt", wantOK: true, wantUntracked: false},
		{name: "type change is tracked", code: " T", path: "a.txt", wantOK: true, wantUntracked: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			untracked, ok := classifyEntry(statusEntry{Code: tc.code, Path: tc.path})
			if ok != tc.wantOK {
				t.Fatalf("classifyEntry(%q) ok = %v, want %v", tc.code, ok, tc.wantOK)
			}
			if ok && untracked != tc.wantUntracked {
				t.Errorf("classifyEntry(%q) untracked = %v, want %v", tc.code, untracked, tc.wantUntracked)
			}
		})
	}
}

func TestParseCheckAttrZ(t *testing.T) {
	cases := []struct {
		name string
		out  []byte
		want map[string]bool
	}{
		{name: "empty output", out: nil, want: map[string]bool{}},
		{name: "truncated triple is ignored", out: nulJoin("a.psd", "filter"), want: map[string]bool{}},
		{
			name: "unspecified filter is not lfs",
			out:  nulJoin("notes.md", "filter", "unspecified"),
			want: map[string]bool{},
		},
		{
			name: "lfs managed path",
			out:  nulJoin("assets/a.psd", "filter", "lfs"),
			want: map[string]bool{"assets/a.psd": true},
		},
		{
			name: "mixed triples",
			out:  nulJoin("a.psd", "filter", "lfs", "b.md", "filter", "unspecified", "c.bin", "filter", "lfs"),
			want: map[string]bool{"a.psd": true, "c.bin": true},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseCheckAttrZ(tc.out)
			if len(got) != len(tc.want) {
				t.Fatalf("parseCheckAttrZ() = %v, want %v", got, tc.want)
			}
			for path := range tc.want {
				if !got[path] {
					t.Errorf("parseCheckAttrZ() missing %q", path)
				}
			}
		})
	}
}

func TestGitEnvForcesCLocale(t *testing.T) {
	env := gitEnv([]string{"PATH=/usr/bin", "LC_ALL=fr_FR.UTF-8", "LANG=fr_FR.UTF-8", "HOME=/home/u"})

	var sawLCAll, sawLang int
	for _, item := range env {
		switch item {
		case "LC_ALL=C":
			sawLCAll++
		case "LANG=C":
			sawLang++
		}
		if strings.HasPrefix(item, "LC_ALL=") && item != "LC_ALL=C" {
			t.Errorf("gitEnv() kept a non-C locale: %q", item)
		}
		if strings.HasPrefix(item, "LANG=") && item != "LANG=C" {
			t.Errorf("gitEnv() kept a non-C language: %q", item)
		}
	}
	if sawLCAll != 1 || sawLang != 1 {
		t.Errorf("gitEnv() set LC_ALL %d times and LANG %d times, want 1 each", sawLCAll, sawLang)
	}
}
