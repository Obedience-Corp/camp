package skills

import (
	"io/fs"
	"strings"
	"syscall"
	"testing"
	"testing/fstest"
)

// failingFS answers every question about one path with err, the way an OS
// does when a bundle is a file (ENOTDIR) or unreadable (EACCES).
type failingFS struct {
	fstest.MapFS
	path string
	err  error
}

func (f failingFS) fail(name string) error {
	if name == f.path {
		return &fs.PathError{Op: "lstat", Path: name, Err: f.err}
	}
	return nil
}

func (f failingFS) Open(name string) (fs.File, error) {
	if err := f.fail(name); err != nil {
		return nil, err
	}
	return f.MapFS.Open(name)
}

func (f failingFS) Stat(name string) (fs.FileInfo, error) {
	if err := f.fail(name); err != nil {
		return nil, err
	}
	return f.MapFS.Stat(name)
}

func (f failingFS) Lstat(name string) (fs.FileInfo, error) {
	if err := f.fail(name); err != nil {
		return nil, err
	}
	return f.MapFS.Lstat(name)
}

func (f failingFS) ReadFile(name string) ([]byte, error) {
	if err := f.fail(name); err != nil {
		return nil, err
	}
	return f.MapFS.ReadFile(name)
}

func TestClassify_OddPluginPathsArePreservedNotErrors(t *testing.T) {
	cases := map[string]struct {
		fsys    fs.FS
		problem string
	}{
		"bundle is a file": {
			fsys:    failingFS{MapFS: fstest.MapFS{"festival-app": {Data: []byte("x")}}, path: "festival-app/SKILL.md", err: syscall.ENOTDIR},
			problem: ".campaign/skills/festival-app is a file, not a folder",
		},
		"SKILL.md is a folder": {
			fsys:    fstest.MapFS{"festival-app/SKILL.md/inner.md": {Data: []byte("x")}},
			problem: ".campaign/skills/festival-app/SKILL.md is a folder, not a file",
		},
		"SKILL.md is unreadable": {
			fsys:    failingFS{MapFS: fstest.MapFS{"festival-app/SKILL.md": {Data: []byte("x")}}, path: "festival-app/SKILL.md", err: syscall.EACCES},
			problem: ".campaign/skills/festival-app/SKILL.md cannot be read: ",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			offer := offered("0.4.0", newSkill)
			inv, err := Classify(t.Context(), tc.fsys, append(campShippedForProblem(), offer.Files...), nil)
			if err != nil {
				t.Fatalf("Classify() error = %v, want the odd bundle reported, not a failure", err)
			}
			inv.applyPluginRules([]PluginBundle{offer}, true)
			f := inv.Files[len(inv.Files)-1]
			if f.State != ShippedUnknown || !strings.HasPrefix(f.Problem, tc.problem) {
				t.Fatalf("state = %s, problem = %q; want unknown with %q", f.State, f.Problem, tc.problem)
			}
		})
	}
}

func campShippedForProblem() []ShippedFile {
	return []ShippedFile{{Bundle: "alpha", Path: "SKILL.md", Content: []byte("alpha v2\n")}}
}

// A camp file that cannot be read still fails, as it does today.
func TestClassify_CampFileErrorsStillFail(t *testing.T) {
	fsys := failingFS{MapFS: fstest.MapFS{"alpha/SKILL.md": {Data: []byte("x")}}, path: "alpha/SKILL.md", err: syscall.EACCES}
	if _, err := Classify(t.Context(), fsys, campShippedForProblem(), nil); err == nil {
		t.Fatal("Classify() hid a camp file error")
	}
}

func TestLinkAndDecline_RefuseSlugsThatAreNotOneFolder(t *testing.T) {
	for _, slug := range []string{"../foo", "foo/bar", ".hidden", "/tmp/abs", "", ".", "..", `foo\bar`} {
		if _, err := LinkBundles("/nonexistent-camp", "/nonexistent-camp/.campaign/skills", []string{slug}, false); err == nil {
			t.Errorf("LinkBundles accepted %q", slug)
		}
		if _, err := Decline(t.Context(), DeclineOptions{Root: "/nonexistent-camp", Slug: slug, Plugin: CampAppPlugin}); err == nil {
			t.Errorf("Decline accepted %q", slug)
		}
	}
	if _, err := Decline(t.Context(), DeclineOptions{Root: "/nonexistent-camp", Slug: "Foo", Plugin: CampAppPlugin}); err == nil {
		t.Error("Decline accepted a name a plugin cannot offer")
	}
}
