package attach

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/Obedience-Corp/camp/internal/campaign"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

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
