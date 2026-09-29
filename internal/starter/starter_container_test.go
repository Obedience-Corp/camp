//go:build container_fs && unix

package starter

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Obedience-Corp/camp/internal/config"
)

func TestCancelledSetupDoesNotInitialize(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Ensure(ctx, func(context.Context, string, bool) error { t.Fatal("initializer called"); return nil })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
}

func TestStarterLockCancellation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lock")
	release, err := lock(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err = lock(ctx, path)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("got %v", err)
	}
}

func TestInterruptedScaffoldKeepsJournalAndRetries(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "config"))
	t.Setenv("CAMP_REGISTRY_PATH", filepath.Join(dir, "registry.json"))
	cfg := config.DefaultGlobalConfig()
	cfg.CampaignsDir = filepath.Join(dir, "camps")
	if err := config.SaveGlobalConfig(context.Background(), &cfg); err != nil {
		t.Fatal(err)
	}
	var first string
	_, err := Ensure(context.Background(), func(_ context.Context, path string, resume bool) error {
		if resume {
			t.Fatal("first setup cannot be a resume")
		}
		first = path
		return errors.New("interrupted before campaign config")
	})
	if err == nil {
		t.Fatal("expected initializer failure")
	}
	result, err := Ensure(context.Background(), func(ctx context.Context, path string, resume bool) error {
		if !resume || path != first {
			t.Fatalf("lost journal: %s %v", path, resume)
		}
		c := config.DefaultCampaignConfig("festival")
		c.Type = config.CampaignTypePersonal
		c.ID = "starter-test-id"
		return config.SaveCampaignConfig(ctx, path, &c)
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Action != "created" {
		t.Fatalf("%+v", result)
	}
	if _, err := os.Stat(config.CampaignConfigPath(result.Path)); err != nil {
		t.Fatal(err)
	}
	reg, err := config.LoadRegistry(context.Background())
	if err != nil || len(reg.Campaigns) != 1 {
		t.Fatalf("registry: %+v %v", reg, err)
	}
}
