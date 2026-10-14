//go:build container_fs

package starter

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
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

func TestPendingReportsWithoutCreating(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "config"))
	t.Setenv("CAMP_REGISTRY_PATH", filepath.Join(dir, "registry.json"))
	cfg := config.DefaultGlobalConfig()
	cfg.CampaignsDir = filepath.Join(dir, "camps")
	if err := config.SaveGlobalConfig(t.Context(), &cfg); err != nil {
		t.Fatal(err)
	}

	path, ok, err := Pending(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dir, "camps", Name)
	if !ok || path != want {
		t.Fatalf("Pending() = %q, %v; want %q, true", path, ok, want)
	}
	if _, err := os.Lstat(filepath.Join(dir, "camps")); !os.IsNotExist(err) {
		t.Fatalf("Pending created the camps directory: %v", err)
	}
	stateDir, err := config.ConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(stateDir, "starter.json")); !os.IsNotExist(err) {
		t.Fatalf("Pending wrote starter state: %v", err)
	}

	result, err := Ensure(t.Context(), func(ctx context.Context, path string, _ bool) error {
		c := config.DefaultCampaignConfig(Name)
		c.Type = config.CampaignTypePersonal
		c.ID = "starter-pending-id"
		return config.SaveCampaignConfig(ctx, path, &c)
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Action != "created" || result.Path != want {
		t.Fatalf("Ensure() = %+v; want created at the path Pending reported", result)
	}

	if _, ok, err := Pending(t.Context()); err != nil || ok {
		t.Fatalf("Pending() after setup = %v, %v; want false, nil", ok, err)
	}
}

func TestConcurrentEnsureSinglePublication(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "config"))
	t.Setenv("CAMP_REGISTRY_PATH", filepath.Join(dir, "registry.json"))
	cfg := config.DefaultGlobalConfig()
	cfg.CampaignsDir = filepath.Join(dir, "camps")
	if err := config.SaveGlobalConfig(context.Background(), &cfg); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	results := make([]Result, 2)
	errs := make([]error, 2)
	for i := range 2 {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			results[idx], errs[idx] = Ensure(context.Background(), func(ctx context.Context, path string, resume bool) error {
				time.Sleep(100 * time.Millisecond)
				c := config.DefaultCampaignConfig(Name)
				c.Type = config.CampaignTypePersonal
				c.ID = fmt.Sprintf("starter-concurrent-%d", idx)
				return config.SaveCampaignConfig(ctx, path, &c)
			})
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("Ensure[%d]: %v", i, err)
		}
	}
	var created, complete int
	for _, r := range results {
		switch r.Action {
		case "created":
			created++
		case "complete":
			complete++
		default:
			t.Fatalf("unexpected action %+v", r)
		}
	}
	if created != 1 || complete != 1 {
		t.Fatalf("actions created=%d complete=%d; want 1 each", created, complete)
	}
	reg, err := config.LoadRegistry(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(reg.Campaigns) != 1 {
		t.Fatalf("registry camps = %d; want 1", len(reg.Campaigns))
	}
	var published string
	for _, r := range results {
		if r.Action == "created" {
			published = r.Path
		}
	}
	if published == "" {
		t.Fatal("no published starter path")
	}
	if _, err := os.Stat(config.CampaignConfigPath(published)); err != nil {
		t.Fatal(err)
	}
}

func TestEnsureSkipsOccupiedTarget(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "config"))
	t.Setenv("CAMP_REGISTRY_PATH", filepath.Join(dir, "registry.json"))
	cfg := config.DefaultGlobalConfig()
	cfg.CampaignsDir = filepath.Join(dir, "camps")
	if err := config.SaveGlobalConfig(context.Background(), &cfg); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "camps", Name)
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "mine.txt"), []byte("precious"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := Ensure(context.Background(), func(context.Context, string, bool) error {
		t.Fatal("initializer must not run when target is occupied")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Action != "skipped" {
		t.Fatalf("action = %q; want skipped", result.Action)
	}
	data, err := os.ReadFile(filepath.Join(target, "mine.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "precious" {
		t.Fatalf("target file changed: %q", data)
	}
	stateDir, err := config.ConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(stateDir, "starter.json")); err == nil {
		t.Fatal("starter.json written for skipped setup")
	}
}
