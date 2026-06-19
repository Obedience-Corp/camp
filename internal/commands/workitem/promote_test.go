package workitem

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Obedience-Corp/camp/internal/fest"
	wkitem "github.com/Obedience-Corp/camp/internal/workitem"
	"github.com/Obedience-Corp/camp/internal/workitem/links"
)

func promoteCampaign(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeFile(t, filepath.Join(root, ".campaign", "campaign.yaml"), "id: test-campaign\nname: Test\ntype: product\n")
	return root
}

func addWorkitem(t *testing.T, root, wtype, slug, title, body string) string {
	t.Helper()
	dir := filepath.Join(root, "workflow", wtype, slug)
	meta := "version: v1alpha6\nkind: workitem\nid: " + wtype + "-" + slug + "-fixed\ntype: " + wtype + "\ntitle: " + title + "\nref: WI-aaaaaa\n"
	writeFile(t, filepath.Join(dir, ".workitem"), meta)
	writeFile(t, filepath.Join(dir, "README.md"), "# "+title+"\n\n"+body+"\n")
	return dir
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%s): %v", path, err)
	}
}

func writeExecutable(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("WriteFile(%s): %v", path, err)
	}
}

func execPromote(t *testing.T, cwd string, args ...string) (string, string, error) {
	t.Helper()
	restore := chdir(t, cwd)
	defer restore()

	cmd := newPromoteCommand()
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs(args)
	cmd.SetContext(context.Background())
	err := cmd.Execute()
	return out.String(), errOut.String(), err
}

func setCurrentPtr(t *testing.T, root, id string) {
	t.Helper()
	if err := links.SaveCurrent(context.Background(), root, &links.Current{
		Version:    links.CurrentSchemaVersion,
		WorkitemID: id,
	}); err != nil {
		t.Fatalf("SaveCurrent: %v", err)
	}
}

func stubFest(t *testing.T) string {
	t.Helper()
	bin := t.TempDir()
	script := "#!/bin/sh\n" +
		"printf '%s\\n' \"$*\" >> fest-args.txt\n" +
		"mkdir -p festivals/planning/sample-feature-stub/001_INGEST/input_specs\n" +
		"printf '%s' '{\"ok\":true,\"festival\":{\"directory\":\"sample-feature-stub\",\"dest\":\"planning\"}}'\n"
	writeExecutable(t, filepath.Join(bin, "fest"), script)
	return bin
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func dungeonWorkitemDir(t *testing.T, root, wtype, status, slug string) string {
	t.Helper()
	matches, _ := filepath.Glob(filepath.Join(root, "workflow", wtype, "dungeon", status, "*", slug))
	if len(matches) > 0 {
		return matches[0]
	}
	flat := filepath.Join(root, "workflow", wtype, "dungeon", status, slug)
	if dirExists(flat) {
		return flat
	}
	return ""
}

func TestPromoteResolution(t *testing.T) {
	cases := []struct {
		name    string
		setup   func(t *testing.T, root string) string
		id      string
		wantErr string
	}{
		{"no id no cwd no current", func(t *testing.T, root string) string { return root }, "", "no workitem in context"},
		{"id given", func(t *testing.T, root string) string { return root }, "design-sample-feature-fixed", ""},
		{"cwd inside workitem", func(t *testing.T, root string) string {
			return filepath.Join(root, "workflow", "design", "sample-feature")
		}, "", ""},
		{"current pointer set", func(t *testing.T, root string) string {
			setCurrentPtr(t, root, "design-sample-feature-fixed")
			return root
		}, "", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := promoteCampaign(t)
			addWorkitem(t, root, "design", "sample-feature", "Sample Feature", "A sample workitem.")
			cwd := tc.setup(t, root)

			args := []string{"--target", "completed", "--no-commit"}
			if tc.id != "" {
				args = append([]string{tc.id}, args...)
			}
			_, _, err := execPromote(t, cwd, args...)

			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want containing %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestPromoteRejectsBadTargets(t *testing.T) {
	cases := []struct {
		target  string
		wantErr string
	}{
		{"active", "cannot promote to active"},
		{"bogus", "invalid target"},
		{"", "required flag --target not set"},
	}
	for _, tc := range cases {
		t.Run("target="+tc.target, func(t *testing.T) {
			root := promoteCampaign(t)
			addWorkitem(t, root, "design", "sample-feature", "Sample", "body")
			args := []string{"--no-commit"}
			if tc.target != "" {
				args = append(args, "--target", tc.target)
			}
			_, _, err := execPromote(t, filepath.Join(root, "workflow", "design", "sample-feature"), args...)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err = %v, want containing %q", err, tc.wantErr)
			}
		})
	}
}

func TestPromoteDungeonTargets(t *testing.T) {
	for _, status := range []string{"completed", "archived", "someday"} {
		t.Run(status, func(t *testing.T) {
			root := promoteCampaign(t)
			src := addWorkitem(t, root, "design", "feat", "Feat", "body")

			_, _, err := execPromote(t, src, "--target", status, "--no-commit")
			if err != nil {
				t.Fatalf("promote %s: %v", status, err)
			}

			if dirExists(src) {
				t.Fatalf("source %s should have moved", src)
			}
			if dungeonWorkitemDir(t, root, "design", status, "feat") == "" {
				t.Fatalf("workitem not found under dungeon/%s", status)
			}
		})
	}
}

func TestPromoteDoublePromoteErrors(t *testing.T) {
	root := promoteCampaign(t)
	src := addWorkitem(t, root, "design", "feat", "Feat", "body")

	if _, _, err := execPromote(t, src, "--target", "completed", "--no-commit"); err != nil {
		t.Fatalf("first promote: %v", err)
	}

	moved := dungeonWorkitemDir(t, root, "design", "completed", "feat")
	if moved == "" {
		t.Fatal("workitem not in dungeon after first promote")
	}
	_, _, err := execPromote(t, moved, "--target", "completed", "--no-commit")
	if err == nil || !strings.Contains(err.Error(), "already at status") {
		t.Fatalf("err = %v, want 'already at status'", err)
	}
}

func TestPromoteDocTarget(t *testing.T) {
	t.Run("empty doc rejected without force", func(t *testing.T) {
		root := promoteCampaign(t)
		dir := filepath.Join(root, "workflow", "design", "empty")
		writeFile(t, filepath.Join(dir, ".workitem"), "version: v1alpha6\nkind: workitem\nid: design-empty-fixed\ntype: design\n")
		writeFile(t, filepath.Join(dir, "README.md"), "")
		_, _, err := execPromote(t, dir, "--target", "doc", "--no-commit")
		if err == nil || !strings.Contains(err.Error(), "workitem doc is empty") {
			t.Fatalf("err = %v, want 'workitem doc is empty'", err)
		}
	})

	t.Run("clobber rejected without force", func(t *testing.T) {
		root := promoteCampaign(t)
		src := addWorkitem(t, root, "design", "feat", "Feat", "body")
		writeFile(t, filepath.Join(root, "docs", "feat", "existing.md"), "keep")
		_, _, err := execPromote(t, src, "--target", "doc", "--no-commit")
		if err == nil || !strings.Contains(err.Error(), "already exists and is not empty") {
			t.Fatalf("err = %v, want clobber error", err)
		}
	})

	t.Run("copies to docs and shelves source", func(t *testing.T) {
		root := promoteCampaign(t)
		src := addWorkitem(t, root, "design", "feat", "Feat", "body")
		if _, _, err := execPromote(t, src, "--target", "doc", "--no-commit"); err != nil {
			t.Fatalf("doc promote: %v", err)
		}
		if !dirExists(filepath.Join(root, "docs", "feat")) {
			t.Fatal("docs/feat should exist")
		}
		if _, err := os.Stat(filepath.Join(root, "docs", "feat", "README.md")); err != nil {
			t.Fatalf("docs/feat/README.md missing: %v", err)
		}
		if dirExists(src) {
			t.Fatal("source should have been shelved")
		}
	})

	t.Run("dest override and keep", func(t *testing.T) {
		root := promoteCampaign(t)
		src := addWorkitem(t, root, "design", "feat", "Feat", "body")
		if _, _, err := execPromote(t, src, "--target", "doc", "--dest", "guides/feat", "--keep", "--no-commit"); err != nil {
			t.Fatalf("doc promote: %v", err)
		}
		if _, err := os.Stat(filepath.Join(root, "docs", "guides", "feat", "README.md")); err != nil {
			t.Fatalf("docs/guides/feat/README.md missing: %v", err)
		}
		if !dirExists(src) {
			t.Fatal("source should remain with --keep")
		}
		meta, err := wkitem.LoadMetadata(context.Background(), src)
		if err != nil {
			t.Fatalf("LoadMetadata: %v", err)
		}
		if meta.PromotedTo != "docs/guides/feat" {
			t.Fatalf("PromotedTo = %q, want docs/guides/feat", meta.PromotedTo)
		}
	})
}

func TestPromoteFestivalTarget(t *testing.T) {
	root := promoteCampaign(t)
	src := addWorkitem(t, root, "design", "myfeature", "My Feature", "Build it.")

	fest.ResetCache()
	t.Setenv("PATH", stubFest(t))

	if _, _, err := execPromote(t, src, "--target", "festival", "--goal", "custom goal here", "--no-commit"); err != nil {
		t.Fatalf("festival promote: %v", err)
	}

	festDir := filepath.Join(root, "festivals", "planning", "sample-feature-stub")
	if !dirExists(festDir) {
		t.Fatal("festival dir should exist")
	}
	if _, err := os.Stat(filepath.Join(festDir, "001_INGEST", "input_specs", "myfeature", "README.md")); err != nil {
		t.Fatalf("ingest copy missing: %v", err)
	}
	if dirExists(src) {
		t.Fatal("source should have been shelved")
	}

	dungeoned := dungeonWorkitemDir(t, root, "design", "completed", "myfeature")
	if dungeoned == "" {
		t.Fatal("source not in dungeon/completed")
	}
	meta, err := wkitem.LoadMetadata(context.Background(), dungeoned)
	if err != nil {
		t.Fatalf("LoadMetadata: %v", err)
	}
	if meta.PromotedTo != "festivals/planning/sample-feature-stub" {
		t.Fatalf("PromotedTo = %q", meta.PromotedTo)
	}

	args, err := os.ReadFile(filepath.Join(root, "fest-args.txt"))
	if err != nil {
		t.Fatalf("read fest-args.txt: %v", err)
	}
	if !strings.Contains(string(args), "custom goal here") {
		t.Fatalf("fest not called with --goal override: %s", args)
	}
}

func TestPromoteFestivalKeepLeavesSource(t *testing.T) {
	root := promoteCampaign(t)
	src := addWorkitem(t, root, "design", "myfeature", "My Feature", "Build it.")

	fest.ResetCache()
	t.Setenv("PATH", stubFest(t))

	if _, _, err := execPromote(t, src, "--target", "festival", "--keep", "--no-commit"); err != nil {
		t.Fatalf("festival promote --keep: %v", err)
	}
	if !dirExists(src) {
		t.Fatal("source should remain with --keep")
	}
}

func TestPromoteFestivalMissingFest(t *testing.T) {
	root := promoteCampaign(t)
	src := addWorkitem(t, root, "design", "myfeature", "My Feature", "Build it.")

	fest.ResetCache()
	t.Setenv("PATH", t.TempDir())
	t.Setenv("HOME", t.TempDir())

	_, errOut, err := execPromote(t, src, "--target", "festival", "--no-commit")
	if err != nil {
		t.Fatalf("fest-missing should be a graceful no-op, got: %v", err)
	}
	if !strings.Contains(errOut, "fest CLI not found") {
		t.Fatalf("stderr = %q, want fest-not-found notice", errOut)
	}
	if !dirExists(src) {
		t.Fatal("source must stay active when fest is missing")
	}
	if dirExists(filepath.Join(root, "festivals", "planning")) {
		if entries, _ := os.ReadDir(filepath.Join(root, "festivals", "planning")); len(entries) > 0 {
			t.Fatal("no festival should be created when fest is missing")
		}
	}
}

func TestPromoteDryRunChangesNothing(t *testing.T) {
	root := promoteCampaign(t)
	src := addWorkitem(t, root, "design", "feat", "Feat", "body")

	out, _, err := execPromote(t, src, "--target", "completed", "--dry-run", "--no-commit")
	if err != nil {
		t.Fatalf("dry-run: %v", err)
	}
	if !strings.Contains(out, "dry-run") {
		t.Fatalf("stdout = %q, want dry-run plan", out)
	}
	if !dirExists(src) {
		t.Fatal("dry-run must not move the source")
	}
}

func TestPromoteJSONSingleObject(t *testing.T) {
	root := promoteCampaign(t)
	src := addWorkitem(t, root, "design", "feat", "Feat", "body")

	out, _, err := execPromote(t, src, "--target", "completed", "--no-commit", "--json")
	if err != nil {
		t.Fatalf("json promote: %v", err)
	}

	dec := json.NewDecoder(strings.NewReader(out))
	var res struct {
		ID        string `json:"id"`
		Type      string `json:"type"`
		Target    string `json:"target"`
		From      string `json:"from"`
		To        string `json:"to"`
		Committed bool   `json:"committed"`
	}
	if err := dec.Decode(&res); err != nil {
		t.Fatalf("decode: %v\n%s", err, out)
	}
	if dec.More() {
		t.Fatal("expected exactly one JSON object")
	}
	if res.ID != "feat" || res.Type != "design" || res.Target != "completed" {
		t.Fatalf("unexpected result: %+v", res)
	}
	if res.From != "workflow/design/feat" {
		t.Fatalf("From = %q", res.From)
	}
	if res.Committed {
		t.Fatal("Committed should be false with --no-commit")
	}
}

func TestPromoteDocRejectsDestEscape(t *testing.T) {
	root := promoteCampaign(t)
	src := addWorkitem(t, root, "design", "feat", "Feat", "body")

	_, _, err := execPromote(t, src, "--target", "doc", "--dest", "../escaped", "--keep", "--no-commit")
	if err == nil || !strings.Contains(err.Error(), "docs/") {
		t.Fatalf("err = %v, want a docs boundary error", err)
	}
	if _, serr := os.Stat(filepath.Join(root, "escaped")); serr == nil {
		t.Fatal("escaped directory must not be created outside docs/")
	}
}

func TestPromoteFestivalRejectsDest(t *testing.T) {
	root := promoteCampaign(t)
	src := addWorkitem(t, root, "design", "feat", "Feat", "body")

	_, _, err := execPromote(t, src, "--target", "festival", "--dest", "whatever", "--no-commit")
	if err == nil || !strings.Contains(err.Error(), "--dest is only valid for --target doc") {
		t.Fatalf("err = %v, want festival --dest rejection", err)
	}
}
