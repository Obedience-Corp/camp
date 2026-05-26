package links

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

// seedScopeTargets creates every directory referenced by the example fixtures
// so quest.ValidateLinkPath does not error on path-existence checks. This
// keeps each test self-contained: no shared state between fixtures and the
// campaign root.
func seedScopeTargets(t *testing.T, root string, links *Links) {
	t.Helper()
	for _, link := range links.Links {
		full := filepath.Join(root, filepath.FromSlash(link.Scope.Path))
		if err := os.MkdirAll(full, 0o755); err != nil {
			t.Fatalf("seed %s: %v", full, err)
		}
		if link.Scope.Kind == ScopeRepo {
			if err := os.MkdirAll(filepath.Join(full, ".git"), 0o755); err != nil {
				t.Fatalf("seed git dir: %v", err)
			}
		}
	}
}

func loadFixture(t *testing.T, name string) *Links {
	t.Helper()
	path := filepath.Join("testdata", name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var out Links
	if err := yaml.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal %s: %v", path, err)
	}
	return &out
}

func TestLoad_MissingFileReturnsEmpty(t *testing.T) {
	root := t.TempDir()
	got, err := Load(context.Background(), root)
	if err != nil {
		t.Fatalf("Load missing: %v", err)
	}
	if got == nil || got.Version != LinksSchemaVersion {
		t.Fatalf("missing file should return Empty(), got %#v", got)
	}
	if len(got.Links) != 0 {
		t.Fatalf("expected empty Links slice, got %d", len(got.Links))
	}
}

func TestLoadCurrent_MissingFileReturnsNil(t *testing.T) {
	root := t.TempDir()
	got, err := LoadCurrent(context.Background(), root)
	if err != nil {
		t.Fatalf("LoadCurrent missing: %v", err)
	}
	if got != nil {
		t.Fatalf("missing current.yaml should return nil, got %#v", got)
	}
}

func TestLoad_RejectsUnknownVersion(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".campaign", "workitems"), 0o755); err != nil {
		t.Fatal(err)
	}
	yaml := "version: workitem-links/v9beta\nlinks: []\n"
	if err := os.WriteFile(LinksPath(root), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Load(context.Background(), root)
	if err == nil {
		t.Fatal("expected error for unknown version")
	}
	if !strings.Contains(err.Error(), "schema version") {
		t.Fatalf("error should mention schema version, got %v", err)
	}
}

func TestSaveLoadRoundTrip_ExampleLinksFixture(t *testing.T) {
	root := t.TempDir()
	in := loadFixture(t, "example_links.yaml")
	seedScopeTargets(t, root, in)

	fixtureNow, err := time.Parse(time.RFC3339, "2026-05-24T22:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if errs := Validate(context.Background(), in, ValidateOptions{
		CampaignRoot: root,
		Now:          fixtureNow,
	}); len(errs) > 0 {
		t.Fatalf("example_links.yaml fixture fails its own schema: %+v", errs)
	}

	if err := Save(context.Background(), root, in); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := Load(context.Background(), root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got.Links) != len(in.Links) {
		t.Fatalf("link count mismatch: got %d, want %d", len(got.Links), len(in.Links))
	}
	for _, expected := range in.Links {
		found, ok := got.FindByID(expected.ID)
		if !ok {
			t.Fatalf("link %s missing after round-trip", expected.ID)
		}
		if found.WorkitemID != expected.WorkitemID {
			t.Fatalf("workitem_id mismatch for %s: got %q, want %q",
				expected.ID, found.WorkitemID, expected.WorkitemID)
		}
		if found.Scope != expected.Scope {
			t.Fatalf("scope mismatch for %s: got %#v, want %#v",
				expected.ID, found.Scope, expected.Scope)
		}
		if found.Role != expected.Role {
			t.Fatalf("role mismatch for %s: got %s, want %s",
				expected.ID, found.Role, expected.Role)
		}
		if !found.CreatedAt.Equal(expected.CreatedAt) {
			t.Fatalf("created_at mismatch for %s: got %v, want %v",
				expected.ID, found.CreatedAt, expected.CreatedAt)
		}
	}
}

func TestSave_ByteStableTwiceInARow(t *testing.T) {
	root := t.TempDir()
	in := loadFixture(t, "example_links.yaml")
	seedScopeTargets(t, root, in)

	if err := Save(context.Background(), root, in); err != nil {
		t.Fatalf("Save 1: %v", err)
	}
	first, err := os.ReadFile(LinksPath(root))
	if err != nil {
		t.Fatal(err)
	}

	reloaded, err := Load(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if err := Save(context.Background(), root, reloaded); err != nil {
		t.Fatalf("Save 2: %v", err)
	}
	second, err := os.ReadFile(LinksPath(root))
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(first, second) {
		t.Fatalf("save→load→save not byte-stable.\nfirst:\n%s\nsecond:\n%s", first, second)
	}
}

func TestSaveLoadCurrent_RoundTrip(t *testing.T) {
	root := t.TempDir()
	cur := &Current{
		Version:    CurrentSchemaVersion,
		WorkitemID: "design-camp-timeline-2026-05-19",
		SelectedAt: time.Now().UTC().Truncate(time.Second),
	}
	if err := SaveCurrent(context.Background(), root, cur); err != nil {
		t.Fatalf("SaveCurrent: %v", err)
	}
	got, err := LoadCurrent(context.Background(), root)
	if err != nil {
		t.Fatalf("LoadCurrent: %v", err)
	}
	if got.WorkitemID != cur.WorkitemID {
		t.Fatalf("workitem_id mismatch: %q vs %q", got.WorkitemID, cur.WorkitemID)
	}
	if !got.SelectedAt.Equal(cur.SelectedAt) {
		t.Fatalf("selected_at mismatch")
	}

	// SaveCurrent(nil) clears the file.
	if err := SaveCurrent(context.Background(), root, nil); err != nil {
		t.Fatalf("SaveCurrent nil: %v", err)
	}
	if got, err := LoadCurrent(context.Background(), root); err != nil || got != nil {
		t.Fatalf("after clear, LoadCurrent = %v, %v; want nil, nil", got, err)
	}
}

// makeValidLink builds a minimal valid Link for unit tests.
func makeValidLink(t *testing.T, root string) Link {
	t.Helper()
	full := filepath.Join(root, "projects", "demo")
	if err := os.MkdirAll(full, 0o755); err != nil {
		t.Fatal(err)
	}
	return Link{
		ID:         genLinkID(t),
		WorkitemID: "design-demo-2026-05-24",
		Scope:      LinkScope{Kind: ScopeProject, Path: "projects/demo"},
		Role:       RolePrimary,
		CreatedAt:  time.Now().UTC().Truncate(time.Second),
		CreatedBy:  "test",
	}
}

func genLinkID(t *testing.T) string {
	t.Helper()
	var b [3]byte
	if _, err := rand.Read(b[:]); err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("lnk_%s_%s", time.Now().UTC().Format("20060102"), hex.EncodeToString(b[:]))
}

func TestValidate_EveryRule(t *testing.T) {
	root := t.TempDir()
	good := makeValidLink(t, root)

	cases := []struct {
		name      string
		mutate    func(*Link)
		wantField string
	}{
		{"missing id", func(l *Link) { l.ID = "" }, "id"},
		{"bad id format", func(l *Link) { l.ID = "not-a-link-id" }, "id"},
		{"missing workitem_id", func(l *Link) { l.WorkitemID = "" }, "workitem_id"},
		{"unknown scope kind", func(l *Link) { l.Scope.Kind = "nope" }, "scope.kind"},
		{"missing scope path", func(l *Link) { l.Scope.Path = "" }, "scope.path"},
		{"leading slash path", func(l *Link) { l.Scope.Path = "/abs/path" }, "scope.path"},
		{"parent escape path", func(l *Link) { l.Scope.Path = "projects/../etc/passwd" }, "scope.path"},
		{"unknown role", func(l *Link) { l.Role = "boss" }, "role"},
		{"missing created_at", func(l *Link) { l.CreatedAt = time.Time{} }, "created_at"},
		{"missing created_by", func(l *Link) { l.CreatedBy = "" }, "created_by"},
		{"bad created_by chars", func(l *Link) { l.CreatedBy = "spaces are bad" }, "created_by"},
		{"project kind under worktrees", func(l *Link) {
			l.Scope.Kind = ScopeProject
			l.Scope.Path = "projects/worktrees/demo"
		}, "scope.path"},
		{"worktree kind outside worktrees", func(l *Link) {
			l.Scope.Kind = ScopeWorktree
			l.Scope.Path = "projects/demo"
		}, "scope.path"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			link := good
			tc.mutate(&link)
			l := &Links{Version: LinksSchemaVersion, Links: []Link{link}}
			errs := Validate(context.Background(), l, ValidateOptions{CampaignRoot: root})
			found := false
			for _, e := range errs {
				if e.Field == tc.wantField {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("expected error on field %q, got %v", tc.wantField, errs)
			}
		})
	}
}

func TestValidate_DuplicatePrimary(t *testing.T) {
	root := t.TempDir()
	a := makeValidLink(t, root)
	b := makeValidLink(t, root) // same scope.path, same role
	b.WorkitemID = "design-other-2026-05-24"
	l := &Links{Version: LinksSchemaVersion, Links: []Link{a, b}}
	errs := Validate(context.Background(), l, ValidateOptions{CampaignRoot: root})
	found := false
	for _, e := range errs {
		if e.Field == "role" && strings.Contains(e.Message, "duplicate primary") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected duplicate-primary finding, got %v", errs)
	}
}

func TestValidate_HappyPath(t *testing.T) {
	root := t.TempDir()
	link := makeValidLink(t, root)
	l := &Links{Version: LinksSchemaVersion, Links: []Link{link}}
	errs := Validate(context.Background(), l, ValidateOptions{CampaignRoot: root})
	if len(errs) != 0 {
		t.Fatalf("expected no errors, got %v", errs)
	}
}

func TestValidate_WorkitemIDMustExistWhenSetProvided(t *testing.T) {
	root := t.TempDir()
	link := makeValidLink(t, root)
	known := map[string]struct{}{"something-else": {}}
	l := &Links{Version: LinksSchemaVersion, Links: []Link{link}}
	errs := Validate(context.Background(), l, ValidateOptions{
		CampaignRoot: root,
		WorkitemIDs:  known,
	})
	found := false
	for _, e := range errs {
		if e.Field == "workitem_id" && strings.Contains(e.Message, "no workitem with id") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected workitem_id missing finding, got %v", errs)
	}

	// AllowMissing suppresses the check.
	errs = Validate(context.Background(), l, ValidateOptions{
		CampaignRoot: root,
		WorkitemIDs:  known,
		AllowMissing: true,
	})
	for _, e := range errs {
		if e.Field == "workitem_id" && strings.Contains(e.Message, "no workitem with id") {
			t.Fatalf("AllowMissing should suppress the workitem-existence check; got %v", errs)
		}
	}
}

func TestAddLink_DuplicatePrimaryRequiresReplace(t *testing.T) {
	l := &Links{Version: LinksSchemaVersion, Links: []Link{}}
	scope := LinkScope{Kind: ScopeProject, Path: "projects/demo"}
	a := Link{
		ID:         "lnk_20260524_aaaaaa",
		WorkitemID: "design-a-2026-05-24",
		Scope:      scope,
		Role:       RolePrimary,
		CreatedAt:  time.Now().UTC(),
		CreatedBy:  "test",
	}
	b := a
	b.ID = "lnk_20260524_bbbbbb"
	b.WorkitemID = "design-b-2026-05-24"

	if err := l.AddLink(a, false); err != nil {
		t.Fatalf("AddLink a: %v", err)
	}
	if err := l.AddLink(b, false); err == nil {
		t.Fatal("expected primary collision without replace")
	}
	if err := l.AddLink(b, true); err != nil {
		t.Fatalf("AddLink b with replace: %v", err)
	}
	if len(l.Links) != 1 {
		t.Fatalf("replace should leave exactly one link, got %d", len(l.Links))
	}
	if l.Links[0].ID != b.ID {
		t.Fatalf("replace should keep new link, got %s", l.Links[0].ID)
	}
}

func TestSave_ConcurrentInvocationsDoNotCorrupt(t *testing.T) {
	root := t.TempDir()
	if err := Save(context.Background(), root, Empty()); err != nil {
		t.Fatalf("seed Save: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "projects", "demo"), 0o755); err != nil {
		t.Fatal(err)
	}

	const workers = 2
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			link := makeValidLink(t, root)
			link.ID = fmt.Sprintf("lnk_20260524_%06x", i+0xabcd00)
			link.Role = RoleRelated // avoid primary collisions between workers
			existing, err := Load(context.Background(), root)
			if err != nil {
				errs <- err
				return
			}
			_ = existing.AddLink(link, false)
			errs <- Save(context.Background(), root, existing)
		}(i)
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatalf("concurrent Save: %v", e)
		}
	}

	got, err := Load(context.Background(), root)
	if err != nil {
		t.Fatalf("Load after concurrent: %v", err)
	}
	// At least one link present (one of the writers won the last-writer-wins
	// race); the file is valid YAML and parses cleanly.
	if len(got.Links) == 0 {
		t.Fatal("expected at least one link after concurrent saves")
	}
}

func TestSave_RandomRoundTripStability(t *testing.T) {
	const iterations = 100
	for i := 0; i < iterations; i++ {
		root := t.TempDir()
		l := generateRandomLinks(t, root, 1+i%5)

		if err := Save(context.Background(), root, l); err != nil {
			t.Fatalf("iter %d Save: %v", i, err)
		}
		first, err := os.ReadFile(LinksPath(root))
		if err != nil {
			t.Fatal(err)
		}
		reloaded, err := Load(context.Background(), root)
		if err != nil {
			t.Fatalf("iter %d Load: %v", i, err)
		}
		if err := Save(context.Background(), root, reloaded); err != nil {
			t.Fatalf("iter %d resave: %v", i, err)
		}
		second, err := os.ReadFile(LinksPath(root))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(first, second) {
			t.Fatalf("iter %d round-trip not byte-stable", i)
		}
	}
}

func generateRandomLinks(t *testing.T, root string, n int) *Links {
	t.Helper()
	l := Empty()
	for i := 0; i < n; i++ {
		path := fmt.Sprintf("projects/demo-%03d", i)
		if err := os.MkdirAll(filepath.Join(root, path), 0o755); err != nil {
			t.Fatal(err)
		}
		l.Links = append(l.Links, Link{
			ID:         fmt.Sprintf("lnk_20260524_%06x", i),
			WorkitemID: fmt.Sprintf("design-demo-%d-2026-05-24", i),
			Scope:      LinkScope{Kind: ScopeProject, Path: path},
			Role:       RolePrimary,
			CreatedAt:  time.Date(2026, 5, 24, 12, i%60, i%60, 0, time.UTC),
			CreatedBy:  "test",
		})
	}
	return l
}
