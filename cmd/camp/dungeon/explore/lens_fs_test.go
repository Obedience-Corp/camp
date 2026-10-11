//go:build container_fs

package explore

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"

	intexplore "github.com/Obedience-Corp/camp/internal/dungeon/explore"
)

// stageLensCamp is a camp with a festival, a status directory named like a
// lens, and an intent, and makes it the working directory.
func stageLensCamp(t *testing.T) string {
	t.Helper()
	t.Setenv("CAMP_ROOT", "")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	root := t.TempDir()
	for rel, body := range map[string]string{
		".campaign/campaign.yaml": "id: fixture\nname: fixture\n",
		"festivals/.dungeon/completed/2026-10-05/alpha-AL0001/fest.yaml": "metadata:\n  id: AL0001\n  name: alpha\n",
		"festivals/.dungeon/all/kept.md":                                 "Kept in a directory named all.\n",
		".campaign/intents/.dungeon/done/note.md":                        "---\ntitle: Note\n---\n\nDone.\n",
	} {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(root)
	return root
}

type exploreStart struct {
	calls int
	query intexplore.Query
	index *intexplore.Index
}

// captureExploreStart stands in for the feed and records what it was given.
func captureExploreStart(t *testing.T) *exploreStart {
	t.Helper()
	seen := &exploreStart{}
	orig := startExploreTUI
	startExploreTUI = func(_ *cobra.Command, _, _ string, query intexplore.Query, _, _ string, _ bool, start *intexplore.Index) error {
		seen.calls++
		seen.query, seen.index = query, start
		return nil
	}
	t.Cleanup(func() { startExploreTUI = orig })
	return seen
}

// A status directory or a dungeon exists only in the camp, so these are
// refused after the camp is read and before the feed takes the terminal.
func TestExploreTerminalRefusesALensTheCampDoesNotHave(t *testing.T) {
	stageLensCamp(t)
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"status", []string{"--status", "bogus"}, `unknown status "bogus"; use finished, completed, done, archived, someday, killed, holding, all, or a status directory name`},
		{"status directory", []string{"--status", "killed/"}, `unknown status "killed/"; use finished, completed, done, archived, someday, killed, holding, all, or a status directory name`},
		{"dungeon", []string{"--dungeon", "nope"}, `unknown dungeon "nope"; use all, a dungeon label, or a camp-relative dungeon path`},
		{"dungeon below a dungeon", []string{"--dungeon", "festivals/.dungeon/completed/"}, `unknown dungeon "festivals/.dungeon/completed/"; use all, a dungeon label, or a camp-relative dungeon path`},
		{"json off", []string{"--json=false", "--status", "bogus"}, `unknown status "bogus"; use finished, completed, done, archived, someday, killed, holding, all, or a status directory name`},
		{"valid status, unknown dungeon", []string{"--status", "all/", "--dungeon", "nope"}, `unknown dungeon "nope"; use all, a dungeon label, or a camp-relative dungeon path`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			started := captureExploreStart(t)
			stdout, stderr, err := runExploreCommand(t, true, tc.args...)
			if got := exitCode(err); got != 2 {
				t.Fatalf("exit code = %d (%v), want 2", got, err)
			}
			if stdout != "" {
				t.Fatalf("stdout = %q, want nothing", stdout)
			}
			if want := "Error: " + tc.want + "\n"; stderr != want {
				t.Fatalf("stderr = %q, want %q", stderr, want)
			}
			if started.calls != 0 {
				t.Fatal("the feed opened on a lens the camp does not have")
			}
		})
	}
}

// The index built to check a lens opens the feed, so the camp is read once.
// Without a lens flag there is nothing to check and the feed starts from its
// cache as before.
func TestExploreTerminalOpensOnTheIndexItChecked(t *testing.T) {
	root := stageLensCamp(t)
	for _, tc := range []struct {
		name    string
		args    []string
		checked bool
		rows    int
	}{
		{"status directory", []string{"--status", "all/"}, true, 1},
		{"built-in status", []string{"--status", "finished"}, true, 2},
		{"dungeon path as a shell completes it", []string{"--dungeon", "./festivals/.dungeon/"}, true, 1},
		{"dungeon label", []string{"--dungeon", "Intents", "--status", "done"}, true, 1},
		{"no lens", nil, false, 0},
		{"text and dates only", []string{"--query", "alpha", "--since", "2026-01-01"}, false, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			started := captureExploreStart(t)
			stdout, stderr, err := runExploreCommand(t, true, tc.args...)
			if err != nil || stdout != "" || stderr != "" {
				t.Fatalf("run = %v, stdout %q, stderr %q", err, stdout, stderr)
			}
			if started.calls != 1 {
				t.Fatalf("the feed opened %d times", started.calls)
			}
			if !tc.checked {
				if started.index != nil {
					t.Fatalf("no lens was named and the camp was read before the feed: %+v", started.index.Items)
				}
				return
			}
			if started.index == nil {
				t.Fatal("the feed was not given the index the lens was checked against")
			}
			if len(started.index.Items) != 3 || len(started.index.Dungeons) != 2 {
				t.Fatalf("index = %d rows in %d dungeons, want the whole camp: 3 rows in 2", len(started.index.Items), len(started.index.Dungeons))
			}
			shown, err := intexplore.Apply(*started.index, started.query)
			if err != nil || len(shown.Items) != tc.rows {
				t.Fatalf("the lens shows %+v (%v), want %d rows", shown.Items, err, tc.rows)
			}
		})
	}

	// The feed's side of the handoff: a model started on such an index saves
	// it and does not read the camp again; one started from the cache reads.
	ctx := context.Background()
	built, err := intexplore.Build(ctx, root)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	cacheDir := filepath.Join(t.TempDir(), "dungeon-feed")
	fresh := exploreModel{ctx: ctx, root: root, cacheDir: cacheDir, index: built, fresh: true}
	cmd := fresh.Init()
	if cmd == nil {
		t.Fatal("a feed started on a built index scheduled no cache save")
	}
	if msg := cmd(); msg != nil {
		t.Fatalf("a feed started on a built index sent %T, want only the save", msg)
	}
	if cached, ok, err := intexplore.LoadCache(ctx, cacheDir); err != nil || !ok || len(cached.Items) != len(built.Items) {
		t.Fatalf("saved cache: ok %v, err %v, %d rows", ok, err, len(cached.Items))
	}
	fromCache := exploreModel{ctx: ctx, root: root, index: built}
	if msg, ok := fromCache.Init()().(exploreLoaded); !ok || msg.err != nil {
		t.Fatalf("a feed started from the cache sent %T, want the camp read", msg)
	}
}
