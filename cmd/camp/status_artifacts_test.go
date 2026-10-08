package main

import (
	"bytes"
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
	roots := []artifacts.UntrackedRoot{{
		Root:  "videos/cut",
		Files: []artifacts.UntrackedFile{{Path: "videos/cut/a.mp4", Size: 3 << 20}, {Path: "videos/cut/b.mp4", Size: 1 << 20}},
		Bytes: 4 << 20,
	}}

	var long bytes.Buffer
	renderStatusArtifacts(&long, roots, statusFormatLong)
	for _, want := range []string{"Artifact content:\n", "\tvideos/cut/a.mp4 (3.0 MB)\n", "\tvideos/cut/b.mp4 (1.0 MB)\n", "camp sync --from <machine>"} {
		if !strings.Contains(long.String(), want) {
			t.Errorf("long output missing %q:\n%s", want, long.String())
		}
	}

	var short bytes.Buffer
	renderStatusArtifacts(&short, roots, statusFormatShort)
	if got, want := short.String(), "artifact content kept out of git: videos/cut/ (2 files, 4.0 MB)\n"; got != want {
		t.Errorf("short output = %q, want %q", got, want)
	}

	var none bytes.Buffer
	renderStatusArtifacts(&none, nil, statusFormatLong)
	if none.Len() != 0 {
		t.Errorf("no artifact content must print nothing, got %q", none.String())
	}
}

func TestRenderStatusArtifactsSummarizesLongLists(t *testing.T) {
	root := artifacts.UntrackedRoot{Root: "renders"}
	for i := 0; i <= statusArtifactListLimit; i++ {
		root.Files = append(root.Files, artifacts.UntrackedFile{Path: fmt.Sprintf("renders/%02d.mov", i), Size: 1 << 20})
		root.Bytes += 1 << 20
	}

	var out bytes.Buffer
	renderStatusArtifacts(&out, []artifacts.UntrackedRoot{root}, statusFormatLong)
	if strings.Contains(out.String(), "renders/00.mov") {
		t.Errorf("past the list limit files must be summarized per root:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "\trenders/ (11 files, 11.0 MB)\n") {
		t.Errorf("missing per-root summary:\n%s", out.String())
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

func TestStatusOptionsPrecedePathspecs(t *testing.T) {
	got := withStatusOptions([]string{"--", "docs"}, "--short", "--ignore-submodules=all")
	want := []string{"--short", "--ignore-submodules=all", "--", "docs"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("args = %q, want %q", got, want)
	}
}
