package workitem

import "testing"

func TestPlanCreateDir(t *testing.T) {
	cases := []struct {
		name         string
		cwdRel       string
		typeFlag     string
		typeExplicit bool
		dir          string
		wantType     string
		wantFrom     string
		wantParent   string
	}{
		{name: "inside workflow/explore", cwdRel: "workflow/explore", typeFlag: "feature",
			wantType: "explore", wantFrom: "workflow/explore", wantParent: "workflow/explore"},
		{name: "inside a nested workitem becomes a sibling", cwdRel: "workflow/explore/foo/notes", typeFlag: "feature",
			wantType: "explore", wantFrom: "workflow/explore", wantParent: "workflow/explore"},
		{name: "underscore type dir", cwdRel: "workflow/code_reviews/pr-12", typeFlag: "feature",
			wantType: "code_reviews", wantFrom: "workflow/code_reviews", wantParent: "workflow/code_reviews"},
		{name: "type dungeon still targets the active type dir", cwdRel: "workflow/explore/dungeon/completed/old", typeFlag: "feature",
			wantType: "explore", wantFrom: "workflow/explore", wantParent: "workflow/explore"},
		{name: "workflow root keeps default", cwdRel: "workflow", typeFlag: "feature",
			wantType: "feature", wantParent: "workflow/feature"},
		{name: "camp root keeps default", cwdRel: ".", typeFlag: "feature",
			wantType: "feature", wantParent: "workflow/feature"},
		{name: "festivals keeps default", cwdRel: "festivals/active/demo-DM0001", typeFlag: "feature",
			wantType: "feature", wantParent: "workflow/feature"},
		{name: "projects keeps default", cwdRel: "projects/camp", typeFlag: "feature",
			wantType: "feature", wantParent: "workflow/feature"},
		{name: "cwd outside camp keeps default", cwdRel: "", typeFlag: "feature",
			wantType: "feature", wantParent: "workflow/feature"},
		{name: "explicit type overrides cwd", cwdRel: "workflow/explore/foo", typeFlag: "bug", typeExplicit: true,
			wantType: "bug", wantParent: "workflow/bug"},
		{name: "explicit default value still counts as explicit", cwdRel: "workflow/explore", typeFlag: "feature", typeExplicit: true,
			wantType: "feature", wantParent: "workflow/feature"},
		{name: "explicit type keeps --dir", cwdRel: "workflow/explore", typeFlag: "bug", typeExplicit: true, dir: "workflow/design",
			wantType: "bug", wantParent: "workflow/design"},
		{name: "--dir workflow/design infers design", cwdRel: ".", typeFlag: "feature", dir: "workflow/design",
			wantType: "design", wantFrom: "workflow/design", wantParent: "workflow/design"},
		{name: "--dir wins over cwd", cwdRel: "workflow/explore", typeFlag: "feature", dir: "workflow/design",
			wantType: "design", wantFrom: "workflow/design", wantParent: "workflow/design"},
		{name: "--dir below a type dir keeps the dir", cwdRel: ".", typeFlag: "feature", dir: "./workflow/design/sub/",
			wantType: "design", wantFrom: "workflow/design", wantParent: "workflow/design/sub"},
		{name: "--dir outside workflow keeps default, ignoring cwd", cwdRel: "workflow/explore", typeFlag: "feature", dir: "docs/notes",
			wantType: "feature", wantParent: "docs/notes"},
		{name: "hidden segment is not a type", cwdRel: "workflow/.dungeon/completed/x", typeFlag: "feature",
			wantType: "feature", wantParent: "workflow/feature"},
		{name: "dungeon segment is not a type", cwdRel: "workflow/dungeon/archived/x", typeFlag: "feature",
			wantType: "feature", wantParent: "workflow/feature"},
		{name: "segment starting with dash is not a type", cwdRel: "workflow/-x/foo", typeFlag: "feature",
			wantType: "feature", wantParent: "workflow/feature"},
		{name: "segment with whitespace is not a type", cwdRel: "workflow/has space/foo", typeFlag: "feature",
			wantType: "feature", wantParent: "workflow/feature"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := planCreateDir(c.typeFlag, c.typeExplicit, c.dir, c.cwdRel)
			want := createPlacement{Type: c.wantType, From: c.wantFrom, Parent: c.wantParent}
			if got != want {
				t.Fatalf("planCreateDir(%q, %v, dir=%q, cwd=%q) = %+v, want %+v",
					c.typeFlag, c.typeExplicit, c.dir, c.cwdRel, got, want)
			}
		})
	}
}

func TestPlanCreateFile(t *testing.T) {
	cases := []struct {
		name         string
		file         string
		typeFlag     string
		typeExplicit bool
		want         createPlacement
	}{
		{name: "file under workflow/bug", file: "workflow/bug/p99-notes.md", typeFlag: "feature",
			want: createPlacement{Type: "bug", From: "workflow/bug", Parent: "workflow/bug"}},
		{name: "file deeper under workflow/bug", file: "workflow/bug/triage/p99.md", typeFlag: "feature",
			want: createPlacement{Type: "bug", From: "workflow/bug", Parent: "workflow/bug/triage"}},
		{name: "file directly in workflow is not typed by its name", file: "workflow/notes.md", typeFlag: "feature",
			want: createPlacement{Type: "feature", Parent: "workflow"}},
		{name: "file at camp root", file: "notes.md", typeFlag: "feature",
			want: createPlacement{Type: "feature", Parent: "."}},
		{name: "explicit type overrides file location", file: "workflow/bug/p99.md", typeFlag: "incident", typeExplicit: true,
			want: createPlacement{Type: "incident", Parent: "workflow/bug"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := planCreateFile(c.typeFlag, c.typeExplicit, c.file); got != c.want {
				t.Fatalf("planCreateFile(%q, %v, %q) = %+v, want %+v", c.typeFlag, c.typeExplicit, c.file, got, c.want)
			}
		})
	}
}

func TestCdTargetFromCwd(t *testing.T) {
	cases := []struct {
		name      string
		cwdRel    string
		cwdInCamp bool
		rel       string
		want      string
	}{
		{name: "camp root", cwdRel: ".", cwdInCamp: true, rel: "workflow/explore/topic", want: "workflow/explore/topic"},
		{name: "type dir", cwdRel: "workflow/explore", cwdInCamp: true, rel: "workflow/explore/topic", want: "topic"},
		{name: "inside a sibling workitem", cwdRel: "workflow/explore/first/notes", cwdInCamp: true, rel: "workflow/explore/topic", want: "../../topic"},
		{name: "outside camp", cwdRel: "", cwdInCamp: false, rel: "workflow/explore/topic", want: "workflow/explore/topic"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := cdTargetFromCwd(c.cwdRel, c.cwdInCamp, c.rel); got != c.want {
				t.Fatalf("cdTargetFromCwd(%q, %v, %q) = %q, want %q", c.cwdRel, c.cwdInCamp, c.rel, got, c.want)
			}
		})
	}
}

func TestCreateFileRel(t *testing.T) {
	cases := []struct {
		name      string
		cwdRel    string
		cwdInCamp bool
		file      string
		want      string
	}{
		{name: "camp root keeps the path", cwdRel: ".", cwdInCamp: true, file: "workflow/bug/p99.md", want: "workflow/bug/p99.md"},
		{name: "type directory joins the cwd", cwdRel: "workflow/explore", cwdInCamp: true, file: "notes.md", want: "workflow/explore/notes.md"},
		{name: "parent segments resolve lexically", cwdRel: "workflow/explore", cwdInCamp: true, file: "../bug/p99.md", want: "workflow/bug/p99.md"},
		{name: "escaping the root stays visible to validation", cwdRel: "workflow", cwdInCamp: true, file: "../../out.md", want: "../out.md"},
		{name: "cwd outside the camp reads from the root", cwdRel: "", cwdInCamp: false, file: "workflow/bug/p99.md", want: "workflow/bug/p99.md"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := createFileRel(tc.cwdRel, tc.cwdInCamp, tc.file); got != tc.want {
				t.Errorf("createFileRel(%q, %v, %q) = %q, want %q", tc.cwdRel, tc.cwdInCamp, tc.file, got, tc.want)
			}
		})
	}
}
