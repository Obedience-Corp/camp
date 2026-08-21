package workitem

import (
	"reflect"
	"testing"
)

func TestParseProjectRenames(t *testing.T) {
	exists := func(want string) func(string) bool {
		return func(p string) bool { return p == want }
	}
	cases := []struct {
		name   string
		in     string
		exists func(string) bool
		want   map[string]string
	}{
		{
			name:   "directory rename",
			in:     "R100\tprojects/old\tprojects/new",
			exists: exists("projects/new"),
			want:   map[string]string{"projects/old": "projects/new"},
		},
		{
			name: "file-level rename collapses to project root",
			in: "R100\tprojects/old/README.md\tprojects/new/README.md\n" +
				"R095\tprojects/old/src/a.go\tprojects/new/src/a.go",
			exists: exists("projects/new"),
			want:   map[string]string{"projects/old": "projects/new"},
		},
		{
			name:   "worktree rename ignored",
			in:     "R100\tprojects/worktrees/old/feat\tprojects/worktrees/new/feat",
			exists: func(string) bool { return true },
			want:   map[string]string{},
		},
		{
			name:   "same-project file move ignored",
			in:     "R100\tprojects/old/a.md\tprojects/old/b.md",
			exists: func(string) bool { return true },
			want:   map[string]string{},
		},
		{
			name: "conflicting destinations skipped",
			in: "R100\tprojects/old/a.md\tprojects/one/a.md\n" +
				"R100\tprojects/old/b.md\tprojects/two/b.md",
			exists: func(string) bool { return true },
			want:   map[string]string{},
		},
		{
			name:   "missing destination dropped",
			in:     "R100\tprojects/old\tprojects/new",
			exists: func(string) bool { return false },
			want:   map[string]string{},
		},
		{
			name:   "blank lines between commits",
			in:     "\nR100\tprojects/a\tprojects/b\n\n",
			exists: exists("projects/b"),
			want:   map[string]string{"projects/a": "projects/b"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseProjectRenames(tc.in, tc.exists)
			if len(got) == 0 && len(tc.want) == 0 {
				return
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %#v, want %#v", got, tc.want)
			}
		})
	}
}

func TestProjectRootPath(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"projects/camp", "projects/camp"},
		{"projects/camp/docs", "projects/camp"},
		{"/projects/camp/", "projects/camp"},
		{"projects/worktrees/camp/feat", ""},
		{"festivals/foo", ""},
		{"camp", ""},
		{"", ""},
	}
	for _, tc := range cases {
		if got := projectRootPath(tc.in); got != tc.want {
			t.Errorf("projectRootPath(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestMappedProjectPath(t *testing.T) {
	if got := mappedProjectPath("projects/old", "projects/new"); got != "projects/new" {
		t.Errorf("exact = %q", got)
	}
	if got := mappedProjectPath("projects/old/docs", "projects/new"); got != "projects/new/docs" {
		t.Errorf("nested = %q", got)
	}
}

func TestReplaceProjectPath(t *testing.T) {
	got, ok := replaceProjectPath([]string{"projects/old", "projects/keep"}, "projects/old", "projects/new")
	if !ok {
		t.Fatal("expected found")
	}
	want := []string{"projects/new", "projects/keep"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v, want %#v", got, want)
	}

	got, ok = replaceProjectPath([]string{"projects/old", "projects/new"}, "projects/old", "projects/new")
	if !ok {
		t.Fatal("expected found")
	}
	if !reflect.DeepEqual(got, []string{"projects/new"}) {
		t.Errorf("duplicate collapse got %#v", got)
	}

	_, ok = replaceProjectPath([]string{"projects/keep"}, "projects/old", "projects/new")
	if ok {
		t.Fatal("missing from must not report found")
	}
}
