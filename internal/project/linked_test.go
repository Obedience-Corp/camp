package project

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Obedience-Corp/camp/internal/campaign"
	"github.com/Obedience-Corp/camp/internal/config"
)

func setupLinkedProjectFixture(t *testing.T, name string, gitRepo bool) (string, string) {
	t.Helper()

	tmpDir := t.TempDir()
	tmpDir, _ = filepath.EvalSymlinks(tmpDir)

	campaignRoot := filepath.Join(tmpDir, "campaign")
	projectRoot := filepath.Join(tmpDir, "external", name)

	if err := os.MkdirAll(filepath.Join(campaignRoot, campaign.CampaignDir), 0o755); err != nil {
		t.Fatalf("create campaign marker dir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(campaignRoot, "projects"), 0o755); err != nil {
		t.Fatalf("create projects dir: %v", err)
	}
	if err := os.MkdirAll(projectRoot, 0o755); err != nil {
		t.Fatalf("create project root: %v", err)
	}

	if gitRepo {
		initGitRepo(t, projectRoot)
		if err := os.WriteFile(filepath.Join(projectRoot, "go.mod"), []byte("module linked"), 0o644); err != nil {
			t.Fatalf("write go.mod: %v", err)
		}
	} else {
		if err := os.WriteFile(filepath.Join(projectRoot, "package.json"), []byte("{}"), 0o644); err != nil {
			t.Fatalf("write package.json: %v", err)
		}
	}

	if err := os.Symlink(projectRoot, filepath.Join(campaignRoot, "projects", name)); err != nil {
		t.Fatalf("create project symlink: %v", err)
	}
	if err := campaign.WriteMarker(projectRoot, campaign.LinkMarker{
		Version:          campaign.LinkMarkerVersion,
		ActiveCampaignID: filepath.Base(campaignRoot) + "-id",
	}); err != nil {
		t.Fatalf("write marker: %v", err)
	}

	return campaignRoot, projectRoot
}

func addLinkedProjectFixture(t *testing.T, campaignRoot, name string, gitRepo bool) string {
	t.Helper()

	projectRoot := filepath.Join(filepath.Dir(campaignRoot), "external", name)
	if err := os.MkdirAll(projectRoot, 0o755); err != nil {
		t.Fatalf("create project root: %v", err)
	}

	if gitRepo {
		initGitRepo(t, projectRoot)
		if err := os.WriteFile(filepath.Join(projectRoot, "go.mod"), []byte("module linked"), 0o644); err != nil {
			t.Fatalf("write go.mod: %v", err)
		}
	} else {
		if err := os.WriteFile(filepath.Join(projectRoot, "package.json"), []byte("{}"), 0o644); err != nil {
			t.Fatalf("write package.json: %v", err)
		}
	}

	if err := os.Symlink(projectRoot, filepath.Join(campaignRoot, "projects", name)); err != nil {
		t.Fatalf("create project symlink: %v", err)
	}
	if err := campaign.WriteMarker(projectRoot, campaign.LinkMarker{
		Version:          campaign.LinkMarkerVersion,
		ActiveCampaignID: filepath.Base(campaignRoot) + "-id",
	}); err != nil {
		t.Fatalf("write marker: %v", err)
	}

	return projectRoot
}

func TestList_IncludesLinkedProjectSources(t *testing.T) {
	campaignRoot, linkedGitPath := setupLinkedProjectFixture(t, "linked-go", true)
	linkedDirPath := addLinkedProjectFixture(t, campaignRoot, "linked-js", false)

	projects, err := List(context.Background(), campaignRoot)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}

	projectMap := make(map[string]Project)
	for _, proj := range projects {
		projectMap[proj.Name] = proj
	}

	linkedGit, ok := projectMap["linked-go"]
	if !ok {
		t.Fatal("missing linked-go project")
	}
	if linkedGit.Source != SourceLinked {
		t.Fatalf("linked-go Source = %q, want %q", linkedGit.Source, SourceLinked)
	}
	if linkedGit.LinkedPath != linkedGitPath {
		t.Fatalf("linked-go LinkedPath = %q, want %q", linkedGit.LinkedPath, linkedGitPath)
	}
	if linkedGit.Type != TypeGo {
		t.Fatalf("linked-go Type = %q, want %q", linkedGit.Type, TypeGo)
	}

	linkedDir, ok := projectMap["linked-js"]
	if !ok {
		t.Fatal("missing linked-js project")
	}
	if linkedDir.Source != SourceLinkedNonGit {
		t.Fatalf("linked-js Source = %q, want %q", linkedDir.Source, SourceLinkedNonGit)
	}
	if linkedDir.LinkedPath != linkedDirPath {
		t.Fatalf("linked-js LinkedPath = %q, want %q", linkedDir.LinkedPath, linkedDirPath)
	}
	if linkedDir.Type != TypeTypeScript {
		t.Fatalf("linked-js Type = %q, want %q", linkedDir.Type, TypeTypeScript)
	}
}

func TestResolve_WithFlag_PreservesLinkedMetadata(t *testing.T) {
	campaignRoot, linkedPath := setupLinkedProjectFixture(t, "linked-go", true)

	result, err := Resolve(context.Background(), campaignRoot, "linked-go")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	if result.Name != "linked-go" {
		t.Fatalf("Resolve().Name = %q, want %q", result.Name, "linked-go")
	}
	if result.Path != linkedPath {
		t.Fatalf("Resolve().Path = %q, want %q", result.Path, linkedPath)
	}
	if result.LogicalPath != filepath.Join("projects", "linked-go") {
		t.Fatalf("Resolve().LogicalPath = %q, want %q", result.LogicalPath, filepath.Join("projects", "linked-go"))
	}
	if result.Source != SourceLinked {
		t.Fatalf("Resolve().Source = %q, want %q", result.Source, SourceLinked)
	}
	if result.LinkedPath != linkedPath {
		t.Fatalf("Resolve().LinkedPath = %q, want %q", result.LinkedPath, linkedPath)
	}
}

func TestResolveFromCwd_LinkedProject(t *testing.T) {
	campaignRoot, linkedPath := setupLinkedProjectFixture(t, "linked-go", true)
	nestedDir := filepath.Join(linkedPath, "src", "pkg")

	if err := os.MkdirAll(nestedDir, 0o755); err != nil {
		t.Fatalf("create nested dir: %v", err)
	}

	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("get cwd: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	if err := os.Chdir(nestedDir); err != nil {
		t.Fatalf("chdir nested dir: %v", err)
	}

	result, err := ResolveFromCwd(context.Background(), campaignRoot)
	if err != nil {
		t.Fatalf("ResolveFromCwd() error = %v", err)
	}

	if result.Name != "linked-go" {
		t.Fatalf("ResolveFromCwd().Name = %q, want %q", result.Name, "linked-go")
	}
	if result.Path != linkedPath {
		t.Fatalf("ResolveFromCwd().Path = %q, want %q", result.Path, linkedPath)
	}
	if result.LogicalPath != filepath.Join("projects", "linked-go") {
		t.Fatalf("ResolveFromCwd().LogicalPath = %q, want %q", result.LogicalPath, filepath.Join("projects", "linked-go"))
	}
	if result.Source != SourceLinked {
		t.Fatalf("ResolveFromCwd().Source = %q, want %q", result.Source, SourceLinked)
	}
}

func TestRemove_LinkedProject_UnlinksAndRemovesMarker(t *testing.T) {
	tmpDir := t.TempDir()
	tmpDir, _ = filepath.EvalSymlinks(tmpDir)

	campaignRoot := filepath.Join(tmpDir, "campaign")
	linkedPath := filepath.Join(tmpDir, "external", "linked-go")

	if err := os.MkdirAll(filepath.Join(campaignRoot, campaign.CampaignDir), 0o755); err != nil {
		t.Fatalf("create campaign marker dir: %v", err)
	}
	if err := os.MkdirAll(linkedPath, 0o755); err != nil {
		t.Fatalf("create linked path: %v", err)
	}
	writeCampaignConfig(t, campaignRoot, "campaign-id")

	mustRunCmd(t, campaignRoot, "git", "init", "-b", "main")
	mustRunCmd(t, campaignRoot, "git", "-C", campaignRoot, "config", "user.email", "test@test.com")
	mustRunCmd(t, campaignRoot, "git", "-C", campaignRoot, "config", "user.name", "Test")
	initGitRepo(t, linkedPath)

	if _, err := AddLinked(context.Background(), campaignRoot, linkedPath, LinkOptions{}); err != nil {
		t.Fatalf("AddLinked() error = %v", err)
	}

	result, err := Remove(context.Background(), campaignRoot, "linked-go", RemoveOptions{})
	if err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	if !result.LinkRemoved {
		t.Fatal("expected LinkRemoved=true")
	}

	if _, err := os.Lstat(filepath.Join(campaignRoot, "projects", "linked-go")); !os.IsNotExist(err) {
		t.Fatalf("expected project symlink removed, got err = %v", err)
	}
	if _, err := os.Stat(campaign.MarkerPath(linkedPath)); !os.IsNotExist(err) {
		t.Fatalf("expected marker removed, got err = %v", err)
	}
}

func TestAddLinked_RejectsProjectLinkedToAnotherCampaign(t *testing.T) {
	tmpDir := t.TempDir()
	tmpDir, _ = filepath.EvalSymlinks(tmpDir)

	campaignRootA := filepath.Join(tmpDir, "campaign-a")
	campaignRootB := filepath.Join(tmpDir, "campaign-b")
	linkedPath := filepath.Join(tmpDir, "external", "shared-linked")
	campaignIDA := "campaign-a-id"
	campaignIDB := "campaign-b-id"

	for i, root := range []string{campaignRootA, campaignRootB} {
		if err := os.MkdirAll(filepath.Join(root, campaign.CampaignDir), 0o755); err != nil {
			t.Fatalf("create campaign marker dir: %v", err)
		}
		if err := os.MkdirAll(filepath.Join(root, "projects"), 0o755); err != nil {
			t.Fatalf("create projects dir: %v", err)
		}
		id := campaignIDA
		if i == 1 {
			id = campaignIDB
		}
		writeCampaignConfig(t, root, id)
	}
	if err := os.MkdirAll(linkedPath, 0o755); err != nil {
		t.Fatalf("create linked path: %v", err)
	}
	initGitRepo(t, linkedPath)

	if _, err := AddLinked(context.Background(), campaignRootA, linkedPath, LinkOptions{}); err != nil {
		t.Fatalf("AddLinked() first campaign error = %v", err)
	}

	_, err := AddLinked(context.Background(), campaignRootB, linkedPath, LinkOptions{})
	if err == nil {
		t.Fatal("expected second AddLinked() to fail")
	}
	if !strings.Contains(err.Error(), "already linked to another campaign") {
		t.Fatalf("AddLinked() error = %v, want linked-to-another-campaign", err)
	}

	marker, err := campaign.ReadMarker(linkedPath)
	if err != nil {
		t.Fatalf("ReadMarker() error = %v", err)
	}
	if marker.ActiveCampaignID != campaignIDA {
		t.Fatalf("marker ActiveCampaignID = %q, want %q", marker.ActiveCampaignID, campaignIDA)
	}

	if _, err := os.Lstat(filepath.Join(campaignRootB, "projects", "shared-linked")); !os.IsNotExist(err) {
		t.Fatalf("expected second campaign symlink to be absent, got err = %v", err)
	}
}

func TestAddLinked_RejectsDuplicateTargetWithinCampaign(t *testing.T) {
	tmpDir := t.TempDir()
	tmpDir, _ = filepath.EvalSymlinks(tmpDir)

	campaignRoot := filepath.Join(tmpDir, "campaign")
	linkedPath := filepath.Join(tmpDir, "external", "shared-linked")

	if err := os.MkdirAll(filepath.Join(campaignRoot, campaign.CampaignDir), 0o755); err != nil {
		t.Fatalf("create campaign marker dir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(campaignRoot, "projects"), 0o755); err != nil {
		t.Fatalf("create projects dir: %v", err)
	}
	writeCampaignConfig(t, campaignRoot, "campaign-id")
	if err := os.MkdirAll(linkedPath, 0o755); err != nil {
		t.Fatalf("create linked path: %v", err)
	}
	initGitRepo(t, linkedPath)

	if _, err := AddLinked(context.Background(), campaignRoot, linkedPath, LinkOptions{Name: "alpha"}); err != nil {
		t.Fatalf("AddLinked() first alias error = %v", err)
	}

	_, err := AddLinked(context.Background(), campaignRoot, linkedPath, LinkOptions{Name: "beta"})
	if err == nil {
		t.Fatal("expected duplicate linked target to fail")
	}

	var linkedErr *ErrProjectAlreadyLinked
	if !errors.As(err, &linkedErr) {
		t.Fatalf("AddLinked() error = %T %v, want ErrProjectAlreadyLinked", err, err)
	}
	if linkedErr.ExistingName != "alpha" {
		t.Fatalf("ExistingName = %q, want %q", linkedErr.ExistingName, "alpha")
	}
	if linkedErr.AttemptedName != "beta" {
		t.Fatalf("AttemptedName = %q, want %q", linkedErr.AttemptedName, "beta")
	}

	if _, err := os.Lstat(filepath.Join(campaignRoot, "projects", "beta")); !os.IsNotExist(err) {
		t.Fatalf("expected duplicate alias to be absent, got err = %v", err)
	}
}

func TestAddLinked_RejectsLegacyMarkerFromAnotherCampaign(t *testing.T) {
	tmpDir := t.TempDir()
	tmpDir, _ = filepath.EvalSymlinks(tmpDir)

	campaignRootA := filepath.Join(tmpDir, "campaign-a")
	campaignRootB := filepath.Join(tmpDir, "campaign-b")
	linkedPath := filepath.Join(tmpDir, "external", "shared-linked")

	for _, root := range []string{campaignRootA, campaignRootB} {
		if err := os.MkdirAll(filepath.Join(root, campaign.CampaignDir), 0o755); err != nil {
			t.Fatalf("create campaign marker dir: %v", err)
		}
		if err := os.MkdirAll(filepath.Join(root, "projects"), 0o755); err != nil {
			t.Fatalf("create projects dir: %v", err)
		}
		writeCampaignConfig(t, root, filepath.Base(root)+"-id")
	}
	if err := os.MkdirAll(linkedPath, 0o755); err != nil {
		t.Fatalf("create linked path: %v", err)
	}
	initGitRepo(t, linkedPath)

	normalizedCampaignA, err := normalizeCampaignRoot(campaignRootA)
	if err != nil {
		t.Fatalf("normalizeCampaignRoot() error = %v", err)
	}
	if err := campaign.WriteMarker(linkedPath, campaign.LinkMarker{
		Version:      1,
		CampaignRoot: normalizedCampaignA,
	}); err != nil {
		t.Fatalf("write legacy marker: %v", err)
	}

	_, err = AddLinked(context.Background(), campaignRootB, linkedPath, LinkOptions{})
	if err == nil {
		t.Fatal("expected AddLinked() to fail for mismatched legacy marker")
	}
	if !strings.Contains(err.Error(), "legacy .camp marker") {
		t.Fatalf("AddLinked() error = %v, want legacy marker guidance", err)
	}

	if _, err := os.Lstat(filepath.Join(campaignRootB, "projects", "shared-linked")); !os.IsNotExist(err) {
		t.Fatalf("expected second campaign symlink to be absent, got err = %v", err)
	}
}

func TestUnlink_RejectsNonLinkedProject(t *testing.T) {
	tmpDir := t.TempDir()
	tmpDir, _ = filepath.EvalSymlinks(tmpDir)

	campaignRoot := filepath.Join(tmpDir, "campaign")
	projectPath := filepath.Join(campaignRoot, "projects", "alpha")

	if err := os.MkdirAll(projectPath, 0o755); err != nil {
		t.Fatalf("create project path: %v", err)
	}

	_, err := Unlink(context.Background(), campaignRoot, "alpha", UnlinkOptions{})
	if err == nil {
		t.Fatal("expected Unlink() to reject non-linked project")
	}

	var notLinkedErr *ErrProjectNotLinked
	if !errors.As(err, &notLinkedErr) {
		t.Fatalf("Unlink() error = %T %v, want ErrProjectNotLinked", err, err)
	}
}

func TestEnsurePatternInFile_Deduplicates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "info", "exclude")
	const pattern = "projects/linked"

	if err := ensurePatternInFile(path, pattern); err != nil {
		t.Fatalf("ensurePatternInFile() first call error = %v", err)
	}
	if err := ensurePatternInFile(path, pattern); err != nil {
		t.Fatalf("ensurePatternInFile() second call error = %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(data) != pattern+"\n" {
		t.Fatalf("exclude contents = %q, want %q", string(data), pattern+"\n")
	}
}

func TestRemovePatternFromFile_RemovesOnlyMatchingLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "info", "exclude")
	initial := "projects/keep\nprojects/remove\nprojects/keep-too\n"
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(path, []byte(initial), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	if err := removePatternFromFile(path, "projects/remove"); err != nil {
		t.Fatalf("removePatternFromFile() error = %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	want := "projects/keep\nprojects/keep-too\n"
	if string(data) != want {
		t.Fatalf("exclude contents = %q, want %q", string(data), want)
	}
}

func writeCampaignConfig(t *testing.T, campaignRoot, campaignID string) {
	t.Helper()

	cfg := &config.CampaignConfig{
		ID:   campaignID,
		Name: filepath.Base(campaignRoot),
		Type: config.CampaignTypeProduct,
	}
	if err := config.SaveCampaignConfig(context.Background(), campaignRoot, cfg); err != nil {
		t.Fatalf("SaveCampaignConfig() error = %v", err)
	}
}
