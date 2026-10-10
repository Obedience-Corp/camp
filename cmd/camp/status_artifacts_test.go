package main

import (
	"bytes"
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/Obedience-Corp/camp/internal/artifacts"
)

func TestDetectStatusFormat(t *testing.T) {
	cases := []struct {
		args []string
		want statusFormat
	}{
		{nil, statusFormatLong},
		{[]string{"--short"}, statusFormatShort},
		{[]string{"-sb"}, statusFormatShort},
		{[]string{"-s", "--long"}, statusFormatLong},
		{[]string{"--porcelain"}, statusFormatMachine},
		{[]string{"--porcelain=v2", "--branch"}, statusFormatMachine},
		{[]string{"-s", "-z"}, statusFormatMachine},
		{[]string{"--null"}, statusFormatMachine},
		{[]string{"-uall"}, statusFormatLong},
		{[]string{"--ignore-submodules=all"}, statusFormatLong},
		{[]string{"--", "-z"}, statusFormatLong},
	}
	for _, c := range cases {
		if got := detectStatusFormat(c.args); got != c.want {
			t.Errorf("detectStatusFormat(%q) = %d, want %d", c.args, got, c.want)
		}
	}
}

func TestWithArtifactExclusions(t *testing.T) {
	paths := []string{"videos/a.mp4", "videos/[take] 1.mov"}

	got, separated := withArtifactExclusions([]string{"--short"}, paths)
	want := []string{"--short", "--", ":(exclude,literal)videos/a.mp4", ":(exclude,literal)videos/[take] 1.mov"}
	if !separated || !reflect.DeepEqual(got, want) {
		t.Errorf("without a user dash-dash: got %q, want %q", got, want)
	}

	got, separated = withArtifactExclusions([]string{"--", "docs"}, paths[:1])
	want = []string{"--", "docs", ":(exclude,literal)videos/a.mp4"}
	if !separated || !reflect.DeepEqual(got, want) {
		t.Errorf("a user dash-dash must be reused: got %q, want %q", got, want)
	}

	in := []string{"--short"}
	if got, separated := withArtifactExclusions(in, nil); !separated || !reflect.DeepEqual(got, in) {
		t.Errorf("no artifact content must leave args untouched: got %q", got)
	}
}

func TestRenderStatusArtifacts(t *testing.T) {
	for _, count := range []int{0, 1, 2, 1500} {
		roots := []artifacts.UntrackedRoot{}
		for i := 0; i < count; i++ {
			roots = append(roots, artifacts.UntrackedRoot{Root: fmt.Sprintf("renders/%d", i), Files: []artifacts.UntrackedFile{{Path: fmt.Sprintf("renders/%d/clip.mp4", i), Size: 1 << 20}}, Bytes: 1 << 20})
		}
		for _, format := range []statusFormat{statusFormatLong, statusFormatShort, statusFormatMachine} {
			var out bytes.Buffer
			renderStatusArtifacts(&out, roots, format)
			got := out.String()
			if count == 0 {
				if got != "" {
					t.Errorf("empty artifacts: %q", got)
				}
				continue
			}
			if !strings.Contains(got, "camp artifacts") || !strings.Contains(got, "kept out of git") {
				t.Errorf("missing discovery hint: %q", got)
			}
			if strings.Contains(got, "renders/") || strings.Count(strings.TrimSpace(got), "\n") != 0 {
				t.Errorf("status must stay one summary line: %q", got)
			}
			if count == 1 && !strings.Contains(got, "1 artifact · 1.0 MB") {
				t.Errorf("singular count: %q", got)
			}
			if count == 1500 && !strings.Contains(got, "1,500 artifacts") {
				t.Errorf("aggregate count: %q", got)
			}
		}
	}
}

func TestArtifactExclusionsBoundedFallback(t *testing.T) {
	args := []string{"--porcelain=v2", "--", "docs"}
	paths := []string{strings.Repeat("x", statusExclusionBudget)}
	got, separated := withArtifactExclusions(args, paths)
	if separated || !reflect.DeepEqual(got, args) {
		t.Fatalf("fallback = %q, %v; want original args", got, separated)
	}
}

func TestStatusHasPathspec(t *testing.T) {
	roots := []artifacts.UntrackedRoot{{
		Root:  "videos",
		Files: []artifacts.UntrackedFile{{Path: "videos/a.mp4", Size: 10}},
		Bytes: 10,
	}}
	skip := [][]string{
		nil,
		{"--ignore-submodules=all"},
		{"--short", "--ignore-submodules=all"},
		{"-uno"},
		{"--untracked-files=no"},
		{"--porcelain"},
		{"--porcelain=v2"},
		{"--"},
	}
	for _, args := range skip {
		if statusHasPathspec(args) {
			t.Errorf("statusHasPathspec(%q) = true, want false", args)
		}
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		got, err := scopedStatusArtifacts(ctx, t.TempDir(), args, roots)
		if err != nil {
			t.Errorf("scopedStatusArtifacts(%q) error = %v, want the candidate list", args, err)
		}
		if !reflect.DeepEqual(got, roots) {
			t.Errorf("scopedStatusArtifacts(%q) = %#v, want the candidate list", args, got)
		}
	}

	keep := [][]string{
		{"docs"},
		{"--", "docs"},
		{"--", ":(glob)videos/**/*.mp4"},
		{"--", "videos", ":(exclude)videos/my-video/takes"},
		{"--porcelain=v2", "-z", "videos/my-video/takes"},
		{"-s", "--", "docs"},
		{"--", "-z"},
	}
	for _, args := range keep {
		if !statusHasPathspec(args) {
			t.Errorf("statusHasPathspec(%q) = false, want true", args)
		}
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		_, err := scopedStatusArtifacts(ctx, t.TempDir(), args, roots)
		if err == nil {
			t.Errorf("scopedStatusArtifacts(%q) skipped git, want a status of the pathspec", args)
		}
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	got, err := scopedStatusArtifacts(ctx, t.TempDir(), nil, nil)
	if err != nil || got != nil {
		t.Errorf("empty candidates = %#v, %v; want nil, nil", got, err)
	}
}

func TestStatusOptionsPrecedePathspecs(t *testing.T) {
	got := withStatusOptions([]string{"--", "docs"}, "--short", "--ignore-submodules=all")
	want := []string{"--short", "--ignore-submodules=all", "--", "docs"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("args = %q, want %q", got, want)
	}
}
