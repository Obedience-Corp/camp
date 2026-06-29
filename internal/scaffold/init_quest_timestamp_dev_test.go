//go:build dev

package scaffold

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/Obedience-Corp/camp/internal/quest"
)

func TestInit_DefaultQuestHasRealTimestamp(t *testing.T) {
	tmpDir := t.TempDir()
	tmpDir, _ = filepath.EvalSymlinks(tmpDir)
	t.Setenv("XDG_CONFIG_HOME", tmpDir)

	campaignDir := filepath.Join(tmpDir, "ts-campaign")
	ctx := context.Background()

	if _, err := Init(ctx, campaignDir, InitOptions{
		Name:        "ts-campaign",
		Description: "d",
		Mission:     "m",
		NoRegister:  true,
		SkipGitInit: true,
	}); err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	q, err := quest.LoadDefault(ctx, campaignDir)
	if err != nil {
		t.Fatalf("LoadDefault() error = %v", err)
	}
	if q.CreatedAt.IsZero() || q.UpdatedAt.IsZero() {
		t.Fatalf("default quest timestamps must be set, got created=%v updated=%v", q.CreatedAt, q.UpdatedAt)
	}
	if q.CreatedAt.Equal(questDateSentinel) || q.UpdatedAt.Equal(questDateSentinel) {
		t.Errorf("default quest must not carry the %v sentinel, got created=%v", questDateSentinel, q.CreatedAt)
	}
}
