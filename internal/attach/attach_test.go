package attach

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Obedience-Corp/camp/internal/campaign"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

func mustRunGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s in %s: %v\n%s", strings.Join(args, " "), dir, err, out)
	}
}

func setupCampaign(t *testing.T) (campaignRoot, registryPath string) {
	t.Helper()
	tmp := t.TempDir()
	tmp, _ = filepath.EvalSymlinks(tmp)

	campaignRoot = filepath.Join(tmp, "campaign")
	if err := os.MkdirAll(filepath.Join(campaignRoot, campaign.CampaignDir), 0755); err != nil {
		t.Fatalf("mkdir campaign: %v", err)
	}

	registryPath = filepath.Join(tmp, "registry.json")
	registryData := []byte(`{
  "version": 1,
  "campaigns": {
    "campaign-xyz": {
      "name": "campaign",
      "path": "` + campaignRoot + `"
    }
  }
}`)
	if err := os.WriteFile(registryPath, registryData, 0644); err != nil {
		t.Fatalf("write registry: %v", err)
	}
	t.Setenv("CAMP_REGISTRY_PATH", registryPath)
	return campaignRoot, registryPath
}

func TestAttach_WritesAttachmentMarker(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink test not supported on Windows")
	}

	campaignRoot, _ := setupCampaign(t)

	external := filepath.Join(t.TempDir(), "external")
	external, _ = filepath.EvalSymlinks(filepath.Dir(external))
	external = filepath.Join(external, "external")
	if err := os.MkdirAll(external, 0755); err != nil {
		t.Fatalf("mkdir external: %v", err)
	}

	res, err := Attach(context.Background(), campaignRoot, "campaign-xyz", external, Options{})
	if err != nil {
		t.Fatalf("Attach: %v", err)
	}
	if res.CampaignID != "campaign-xyz" {
		t.Errorf("CampaignID = %q, want %q", res.CampaignID, "campaign-xyz")
	}

	marker, err := campaign.ReadMarker(external)
	if err != nil {
		t.Fatalf("ReadMarker: %v", err)
	}
	if marker.Kind != campaign.KindAttachment {
		t.Errorf("Kind = %q, want %q", marker.Kind, campaign.KindAttachment)
	}
	if marker.Version != campaign.LinkMarkerVersion {
		t.Errorf("Version = %d, want %d", marker.Version, campaign.LinkMarkerVersion)
	}
	if marker.ActiveCampaignID != "campaign-xyz" {
		t.Errorf("ActiveCampaignID = %q, want %q", marker.ActiveCampaignID, "campaign-xyz")
	}
}

func TestAttach_FollowsSymlinkInput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink test not supported on Windows")
	}

	campaignRoot, _ := setupCampaign(t)

	tmp := t.TempDir()
	tmp, _ = filepath.EvalSymlinks(tmp)
	target := filepath.Join(tmp, "real-dir")
	if err := os.MkdirAll(target, 0755); err != nil {
		t.Fatalf("mkdir target: %v", err)
	}
	symlinkParent := filepath.Join(campaignRoot, "ai_docs", "examples")
	if err := os.MkdirAll(symlinkParent, 0755); err != nil {
		t.Fatalf("mkdir symlink parent: %v", err)
	}
	symlink := filepath.Join(symlinkParent, "external-repo")
	if err := os.Symlink(target, symlink); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	res, err := Attach(context.Background(), campaignRoot, "campaign-xyz", symlink, Options{})
	if err != nil {
		t.Fatalf("Attach: %v", err)
	}

	if res.Target != target {
		t.Errorf("Target = %q, want %q", res.Target, target)
	}
	if !res.FollowedSymlink {
		t.Errorf("FollowedSymlink = false, want true")
	}

	if _, err := os.Stat(filepath.Join(target, campaign.LinkMarkerFile)); err != nil {
		t.Errorf("marker not at resolved target: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(symlinkParent, campaign.LinkMarkerFile)); !os.IsNotExist(err) {
		t.Errorf("marker should not be next to symlink")
	}
}

func TestAttach_RefusesPathInsideCampaign(t *testing.T) {
	campaignRoot, _ := setupCampaign(t)

	inside := filepath.Join(campaignRoot, "festivals", "active", "foo")
	if err := os.MkdirAll(inside, 0755); err != nil {
		t.Fatalf("mkdir inside: %v", err)
	}

	_, err := Attach(context.Background(), campaignRoot, "campaign-xyz", inside, Options{})
	if err == nil {
		t.Fatal("expected error for path inside campaign tree")
	}
	if !errors.Is(err, camperrors.ErrInvalidInput) {
		t.Errorf("error = %v, want ErrInvalidInput", err)
	}
}

func TestAttach_RefusesExistingMarkerWithoutForce(t *testing.T) {
	campaignRoot, _ := setupCampaign(t)

	external := filepath.Join(t.TempDir(), "external")
	external, _ = filepath.EvalSymlinks(filepath.Dir(external))
	external = filepath.Join(external, "external")
	if err := os.MkdirAll(external, 0755); err != nil {
		t.Fatalf("mkdir external: %v", err)
	}
	if _, err := Attach(context.Background(), campaignRoot, "campaign-xyz", external, Options{}); err != nil {
		t.Fatalf("first Attach: %v", err)
	}

	_, err := Attach(context.Background(), campaignRoot, "campaign-xyz", external, Options{})
	if err == nil {
		t.Fatal("expected error on duplicate attach")
	}
	if !errors.Is(err, camperrors.ErrAlreadyExists) {
		t.Errorf("error = %v, want ErrAlreadyExists", err)
	}
}

func TestAttach_ForceOverwritesAttachment(t *testing.T) {
	campaignRoot, _ := setupCampaign(t)

	external := filepath.Join(t.TempDir(), "external")
	external, _ = filepath.EvalSymlinks(filepath.Dir(external))
	external = filepath.Join(external, "external")
	if err := os.MkdirAll(external, 0755); err != nil {
		t.Fatalf("mkdir external: %v", err)
	}
	if _, err := Attach(context.Background(), campaignRoot, "campaign-xyz", external, Options{}); err != nil {
		t.Fatalf("first Attach: %v", err)
	}

	if _, err := Attach(context.Background(), campaignRoot, "campaign-xyz", external, Options{Force: true}); err != nil {
		t.Errorf("Force attach: %v", err)
	}
}

func TestAttach_ForceRefusesProjectMarker(t *testing.T) {
	campaignRoot, _ := setupCampaign(t)

	external := filepath.Join(t.TempDir(), "external")
	external, _ = filepath.EvalSymlinks(filepath.Dir(external))
	external = filepath.Join(external, "external")
	if err := os.MkdirAll(external, 0755); err != nil {
		t.Fatalf("mkdir external: %v", err)
	}

	// Hand-write a project marker.
	projectMarker := campaign.LinkMarker{
		Version:          campaign.LinkMarkerVersion,
		Kind:             campaign.KindProject,
		ActiveCampaignID: "campaign-xyz",
		ProjectName:      "example",
	}
	if err := campaign.WriteMarker(external, projectMarker); err != nil {
		t.Fatalf("WriteMarker: %v", err)
	}

	_, err := Attach(context.Background(), campaignRoot, "campaign-xyz", external, Options{Force: true})
	if err == nil {
		t.Fatal("expected error refusing to overwrite project marker")
	}
	if !errors.Is(err, camperrors.ErrInvalidInput) {
		t.Errorf("error = %v, want ErrInvalidInput", err)
	}
}

func TestDetach_RemovesAttachmentMarker(t *testing.T) {
	campaignRoot, _ := setupCampaign(t)

	external := filepath.Join(t.TempDir(), "external")
	external, _ = filepath.EvalSymlinks(filepath.Dir(external))
	external = filepath.Join(external, "external")
	if err := os.MkdirAll(external, 0755); err != nil {
		t.Fatalf("mkdir external: %v", err)
	}
	if _, err := Attach(context.Background(), campaignRoot, "campaign-xyz", external, Options{}); err != nil {
		t.Fatalf("Attach: %v", err)
	}

	res, err := Detach(context.Background(), external)
	if err != nil {
		t.Fatalf("Detach: %v", err)
	}
	if res.Target != external {
		t.Errorf("Target = %q, want %q", res.Target, external)
	}

	if _, err := os.Stat(filepath.Join(external, campaign.LinkMarkerFile)); !os.IsNotExist(err) {
		t.Errorf("marker still present: %v", err)
	}
}

func TestDetach_RefusesProjectMarker(t *testing.T) {
	external := filepath.Join(t.TempDir(), "external")
	external, _ = filepath.EvalSymlinks(filepath.Dir(external))
	external = filepath.Join(external, "external")
	if err := os.MkdirAll(external, 0755); err != nil {
		t.Fatalf("mkdir external: %v", err)
	}

	projectMarker := campaign.LinkMarker{
		Version:          campaign.LinkMarkerVersion,
		Kind:             campaign.KindProject,
		ActiveCampaignID: "campaign-xyz",
	}
	if err := campaign.WriteMarker(external, projectMarker); err != nil {
		t.Fatalf("WriteMarker: %v", err)
	}

	_, err := Detach(context.Background(), external)
	if err == nil {
		t.Fatal("expected error refusing to detach project marker")
	}
	if !errors.Is(err, camperrors.ErrInvalidInput) {
		t.Errorf("error = %v, want ErrInvalidInput", err)
	}
}

// TestAttach_RefusesPathInsideOtherCampaign covers the security gap obey-agent
// flagged in PR #270 review: previously, the in-tree check only rejected the
// selected campaign root, so attaching inside ANOTHER campaign would write a
// shadowing .camp marker. Detection must now reject any campaign context.
func TestAttach_RefusesPathInsideOtherCampaign(t *testing.T) {
	tmp := t.TempDir()
	tmp, _ = filepath.EvalSymlinks(tmp)

	// Selected campaign (target of the attach call).
	selected := filepath.Join(tmp, "selected")
	if err := os.MkdirAll(filepath.Join(selected, campaign.CampaignDir), 0755); err != nil {
		t.Fatalf("mkdir selected: %v", err)
	}
	// Other campaign — totally unrelated, target lives inside it.
	other := filepath.Join(tmp, "other")
	if err := os.MkdirAll(filepath.Join(other, campaign.CampaignDir), 0755); err != nil {
		t.Fatalf("mkdir other: %v", err)
	}
	insideOther := filepath.Join(other, "subdir")
	if err := os.MkdirAll(insideOther, 0755); err != nil {
		t.Fatalf("mkdir insideOther: %v", err)
	}

	registryPath := filepath.Join(tmp, "registry.json")
	registryData := []byte(`{
  "version": 1,
  "campaigns": {
    "selected-id": {"name": "selected", "path": "` + selected + `"},
    "other-id":    {"name": "other",    "path": "` + other + `"}
  }
}`)
	if err := os.WriteFile(registryPath, registryData, 0644); err != nil {
		t.Fatalf("write registry: %v", err)
	}
	t.Setenv("CAMP_REGISTRY_PATH", registryPath)

	_, err := Attach(context.Background(), selected, "selected-id", insideOther, Options{})
	if err == nil {
		t.Fatal("expected error for path inside another campaign")
	}
	if !errors.Is(err, camperrors.ErrInvalidInput) {
		t.Errorf("error = %v, want ErrInvalidInput", err)
	}
	// No .camp marker should have been written into the other campaign.
	if _, statErr := os.Stat(filepath.Join(insideOther, campaign.LinkMarkerFile)); !os.IsNotExist(statErr) {
		t.Errorf("marker leaked into other campaign: %v", statErr)
	}
}

// TestAttach_AddsGitInfoExclude covers the second gap obey-agent flagged: when
// the target is a Git repo, .camp must be added to .git/info/exclude so
// campaign-local state is not accidentally committed.
func TestAttach_AddsGitInfoExclude(t *testing.T) {
	campaignRoot, _ := setupCampaign(t)

	external := filepath.Join(t.TempDir(), "external")
	external, _ = filepath.EvalSymlinks(filepath.Dir(external))
	external = filepath.Join(external, "external")
	if err := os.MkdirAll(external, 0755); err != nil {
		t.Fatalf("mkdir external: %v", err)
	}
	// Initialize the directory as a real git repo via plumbing.
	mustRunGit(t, external, "init", "-q")

	res, err := Attach(context.Background(), campaignRoot, "campaign-xyz", external, Options{})
	if err != nil {
		t.Fatalf("Attach: %v", err)
	}
	if res.GitExcludeWarning != "" {
		t.Fatalf("unexpected GitExcludeWarning: %s", res.GitExcludeWarning)
	}
	if !res.GitExcludeUpdated {
		t.Fatal("GitExcludeUpdated = false, want true for git target")
	}

	excludePath := filepath.Join(external, ".git", "info", "exclude")
	data, err := os.ReadFile(excludePath)
	if err != nil {
		t.Fatalf("read exclude: %v", err)
	}
	if !strings.Contains(string(data), campaign.LinkMarkerFile) {
		t.Errorf(".git/info/exclude does not contain %q; got:\n%s", campaign.LinkMarkerFile, string(data))
	}
}

// TestDetach_RemovesGitInfoExclude verifies the symmetric cleanup on detach.
func TestDetach_RemovesGitInfoExclude(t *testing.T) {
	campaignRoot, _ := setupCampaign(t)

	external := filepath.Join(t.TempDir(), "external")
	external, _ = filepath.EvalSymlinks(filepath.Dir(external))
	external = filepath.Join(external, "external")
	if err := os.MkdirAll(external, 0755); err != nil {
		t.Fatalf("mkdir external: %v", err)
	}
	mustRunGit(t, external, "init", "-q")

	if _, err := Attach(context.Background(), campaignRoot, "campaign-xyz", external, Options{}); err != nil {
		t.Fatalf("Attach: %v", err)
	}
	if _, err := Detach(context.Background(), external); err != nil {
		t.Fatalf("Detach: %v", err)
	}

	excludePath := filepath.Join(external, ".git", "info", "exclude")
	data, _ := os.ReadFile(excludePath)
	if strings.Contains(string(data), campaign.LinkMarkerFile) {
		t.Errorf(".git/info/exclude still contains %q after detach; got:\n%s", campaign.LinkMarkerFile, string(data))
	}
}

func TestDetach_NoMarkerError(t *testing.T) {
	external := filepath.Join(t.TempDir(), "external")
	external, _ = filepath.EvalSymlinks(filepath.Dir(external))
	external = filepath.Join(external, "external")
	if err := os.MkdirAll(external, 0755); err != nil {
		t.Fatalf("mkdir external: %v", err)
	}

	_, err := Detach(context.Background(), external)
	if err == nil {
		t.Fatal("expected error when no marker present")
	}
	if !errors.Is(err, camperrors.ErrNotFound) {
		t.Errorf("error = %v, want ErrNotFound", err)
	}
}
