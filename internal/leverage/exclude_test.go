package leverage

import "testing"

func TestExcludeSet_Excluded(t *testing.T) {
	tests := []struct {
		name  string
		extra []string
		path  string
		want  bool
	}{
		{name: "authored source kept", path: "internal/leverage/authors.go", want: false},
		{name: "root file kept", path: "main.go", want: false},
		{name: "node_modules at root", path: "node_modules/react/index.js", want: true},
		{name: "node_modules nested", path: "web/app/node_modules/@mui/icons-material/Add.js", want: true},
		{name: "vendor dir", path: "vendor/github.com/pkg/errors/errors.go", want: true},
		{name: "dist output", path: "web/dist/bundle.js", want: true},
		{name: "python venv", path: ".venv/lib/site-packages/x.py", want: true},

		// The final element is the file, never a directory match: a file
		// literally named "vendor" or "build" is authored code.
		{name: "file named vendor", path: "cmd/vendor", want: false},
		{name: "file named build", path: "build", want: false},

		// Substring collisions must not match.
		{name: "prefix collision", path: "node_modules_helper/x.go", want: false},
		{name: "suffix collision", path: "my_node_modules/x.go", want: false},

		// Project exclusions given as bare names match at any depth.
		{name: "extra name", extra: []string{"generated"}, path: "api/generated/pb.go", want: true},

		// Submodule paths are multi-element and match only their own subtree,
		// so a same-named directory elsewhere is still counted.
		{name: "submodule path", extra: []string{"projects/obey"}, path: "projects/obey/main.go", want: true},
		{name: "submodule path sibling kept", extra: []string{"projects/obey"}, path: "projects/obey-chat/main.go", want: false},
		{name: "submodule path elsewhere kept", extra: []string{"projects/obey"}, path: "other/projects/obey/main.go", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := newExcludeSet(tt.extra).excluded(tt.path)
			if got != tt.want {
				t.Errorf("excluded(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

func TestExcludeSet_HandlesMalformedEntries(t *testing.T) {
	// Empty, whitespace, "." and slash-wrapped entries must not exclude
	// everything by accident.
	set := newExcludeSet([]string{"", "   ", ".", "/", "/generated/"})

	if set.excluded("internal/app/main.go") {
		t.Error("malformed exclude entries should not exclude authored code")
	}
	if !set.excluded("api/generated/pb.go") {
		t.Error("slash-wrapped entry should normalize to a name match")
	}
}

func TestFilterExcluded(t *testing.T) {
	files := []string{
		"main.go",
		"node_modules/react/index.js",
		"internal/app/app.go",
		"vendor/x/y.go",
	}

	got := filterExcluded(files, nil)
	want := []string{"main.go", "internal/app/app.go"}

	if len(got) != len(want) {
		t.Fatalf("filterExcluded() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("filterExcluded()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}
