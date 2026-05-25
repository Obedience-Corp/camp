package workitem

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/Obedience-Corp/camp/internal/workitem/links"
)

// commitsTestCampaign mirrors stagingTestCampaign but additionally tags one
// commit in the campaign root with the WI-abc123 ref so queries have data to
// find. The workitem id is design-example-2026-05-24, ref WI-abc123.
func commitsTestCampaign(t *testing.T) string {
	t.Helper()
	root := stagingTestCampaign(t)
	if err := os.WriteFile(filepath.Join(root, "workflow", "design", "example", "ROOT.md"),
		[]byte("root commit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", "-A")
	runGit(t, root, "commit", "-q", "-m", "[OBEY-CAMPAIGN-test-WI-WI-abc123] WorkitemScope: root change")
	return root
}

func TestEnumerateQueryRepos_IncludesCampaignAndLinkedProject(t *testing.T) {
	root := commitsTestCampaign(t)
	demoDir := filepath.Join(root, "projects", "demo")
	runGit(t, demoDir, "init", "-q")
	registry := links.Links{
		Version: links.LinksSchemaVersion,
		Links: []links.Link{{
			ID:         "lnk_20260524_aaaaaa",
			WorkitemID: "design-example-2026-05-24",
			Scope:      links.LinkScope{Kind: links.ScopeProject, Path: "projects/demo"},
			Role:       links.RolePrimary,
		}},
	}
	if err := links.Save(context.Background(), root, &registry); err != nil {
		t.Fatalf("save links: %v", err)
	}

	repos, err := enumerateQueryRepos(context.Background(), root)
	if err != nil {
		t.Fatalf("enumerateQueryRepos: %v", err)
	}
	if !contains(repos, root) {
		t.Errorf("campaign root missing from repos: %v", repos)
	}
	if !contains(repos, demoDir) {
		t.Errorf("linked project missing from repos: %v", repos)
	}
}

func TestEnumerateQueryRepos_DedupesDuplicateLinks(t *testing.T) {
	root := commitsTestCampaign(t)
	demoDir := filepath.Join(root, "projects", "demo")
	_ = demoDir
	registry := links.Links{
		Version: links.LinksSchemaVersion,
		Links: []links.Link{
			{
				ID:         "lnk_20260524_aaaaaa",
				WorkitemID: "design-example-2026-05-24",
				Scope:      links.LinkScope{Kind: links.ScopeProject, Path: "projects/demo"},
				Role:       links.RolePrimary,
			},
			{
				ID:         "lnk_20260524_bbbbbb",
				WorkitemID: "design-example-2026-05-24",
				Scope:      links.LinkScope{Kind: links.ScopeRepo, Path: "projects/demo"},
				Role:       links.RoleRelated,
			},
		},
	}
	if err := links.Save(context.Background(), root, &registry); err != nil {
		t.Fatalf("save links: %v", err)
	}

	repos, err := enumerateQueryRepos(context.Background(), root)
	if err != nil {
		t.Fatalf("enumerateQueryRepos: %v", err)
	}
	seen := map[string]int{}
	for _, r := range repos {
		seen[r]++
	}
	if seen[filepath.Join(root, "projects", "demo")] != 1 {
		t.Fatalf("expected demo to appear once, got %d: %v", seen[filepath.Join(root, "projects", "demo")], repos)
	}
}

func TestQueryRepo_FiltersFalsePositives(t *testing.T) {
	root := commitsTestCampaign(t)
	// Add a commit whose subject mentions WI-abc123 in body-like text but
	// whose tag references a different ref. The grep would match; ParseTag
	// must reject.
	if err := os.WriteFile(filepath.Join(root, "workflow", "design", "example", "noise.md"),
		[]byte("noise\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", "-A")
	runGit(t, root, "commit", "-q", "-m",
		"[OBEY-CAMPAIGN-test-WI-WI-zzzzzz] WorkitemScope: notes about WI-abc123 (different ref tag)")

	records, err := queryRepo(context.Background(), root, "WI-abc123")
	if err != nil {
		t.Fatalf("queryRepo: %v", err)
	}
	// Should only return the original commit, not the noise one.
	if len(records) != 1 {
		for _, r := range records {
			t.Logf("record: sha=%s subject=%q tagRef=%q", r.SHA[:8], r.Subject, r.TagParts.WorkitemRef)
		}
		t.Fatalf("expected 1 record after ParseTag filtering, got %d", len(records))
	}
	if records[0].TagParts.WorkitemRef != "WI-abc123" {
		t.Fatalf("tag ref = %q, want WI-abc123", records[0].TagParts.WorkitemRef)
	}
}

func TestQueryRepo_NonGitDirReturnsNil(t *testing.T) {
	dir := t.TempDir()
	records, err := queryRepo(context.Background(), dir, "WI-abc123")
	if err != nil {
		t.Fatalf("queryRepo: %v", err)
	}
	if records != nil {
		t.Fatalf("expected nil for non-git dir, got %v", records)
	}
}

func TestQueryRepo_ContextCanceledSurfacesError(t *testing.T) {
	root := commitsTestCampaign(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := queryRepo(ctx, root, "WI-abc123")
	if err == nil {
		t.Fatal("queryRepo with canceled context returned nil error")
	}
}

func TestCommitsWorkerCountCapsFanout(t *testing.T) {
	for _, repoCount := range []int{0, 1, 2, 100} {
		got := commitsWorkerCount(repoCount)
		if got < 1 {
			t.Fatalf("commitsWorkerCount(%d) = %d, want >= 1", repoCount, got)
		}
		if got > commitsMaxWorkers {
			t.Fatalf("commitsWorkerCount(%d) = %d, want <= %d", repoCount, got, commitsMaxWorkers)
		}
		if repoCount > 0 && got > repoCount {
			t.Fatalf("commitsWorkerCount(%d) = %d, want <= repo count", repoCount, got)
		}
	}
}

func TestEmitCommitsQueryWarnings(t *testing.T) {
	var stderr bytes.Buffer
	emitCommitsQueryWarnings(&stderr, []commitsQueryError{{Repo: "demo", Err: "boom"}})
	if got := stderr.String(); got != "warning: 1 repo(s) failed; re-run with --json for details\n" {
		t.Fatalf("warning = %q", got)
	}

	stderr.Reset()
	emitCommitsQueryWarnings(&stderr, nil)
	if stderr.Len() != 0 {
		t.Fatalf("empty errors emitted warning: %q", stderr.String())
	}
}

func TestSearchRepos_AggregatesAndSorts(t *testing.T) {
	root := commitsTestCampaign(t)
	demoDir := filepath.Join(root, "projects", "demo")
	runGit(t, demoDir, "init", "-q")
	runGit(t, demoDir, "config", "user.email", "test@example.com")
	runGit(t, demoDir, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(demoDir, "x.go"), []byte("package x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, demoDir, "add", "-A")
	runGit(t, demoDir, "commit", "-q", "-m",
		"[OBEY-CAMPAIGN-test-WI-WI-abc123] WorkitemScope: project change")

	records, errs := searchRepos(context.Background(), []string{root, demoDir}, "WI-abc123")
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(records) < 2 {
		t.Fatalf("expected at least 2 records across both repos, got %d", len(records))
	}
	// Verify both repo bases are represented.
	repos := map[string]bool{}
	for _, r := range records {
		repos[r.Repo] = true
	}
	if !repos["demo"] {
		t.Errorf("missing demo repo in records: %v", repos)
	}
	if !repos[filepath.Base(root)] {
		t.Errorf("missing campaign root repo in records: %v", repos)
	}
	// Confirm sort works.
	sorted := append([]CommitRecord{}, records...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Date.After(sorted[j].Date) })
	for i := range sorted {
		if !sorted[i].Date.Equal(sorted[i].Date) {
			t.Fatalf("dates not parsed")
		}
	}
}
