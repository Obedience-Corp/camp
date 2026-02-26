package dungeon

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadCrawlConfig(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		want     []string
		wantErr  bool
	}{
		{
			name:    "valid config with excludes",
			content: "excludes:\n  - templates\n  - pending\n",
			want:    []string{"templates", "pending"},
		},
		{
			name:    "empty excludes",
			content: "excludes: []\n",
			want:    nil,
		},
		{
			name:    "no excludes field",
			content: "# empty config\n",
			want:    nil,
		},
		{
			name:    "single exclude",
			content: "excludes:\n  - templates\n",
			want:    []string{"templates"},
		},
		{
			name:    "empty file",
			content: "",
			want:    nil,
		},
		{
			name:    "invalid yaml",
			content: ":::invalid",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			cfgPath := filepath.Join(tmpDir, CrawlConfigFile)

			if err := os.WriteFile(cfgPath, []byte(tt.content), 0644); err != nil {
				t.Fatalf("failed to write test config: %v", err)
			}

			cfg, err := loadCrawlConfig(cfgPath)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if len(cfg.Excludes) != len(tt.want) {
				t.Errorf("got %d excludes, want %d", len(cfg.Excludes), len(tt.want))
				return
			}
			for i, got := range cfg.Excludes {
				if got != tt.want[i] {
					t.Errorf("exclude[%d] = %q, want %q", i, got, tt.want[i])
				}
			}
		})
	}
}

func TestLoadCrawlConfig_MissingFile(t *testing.T) {
	_, err := loadCrawlConfig("/nonexistent/.crawl.yaml")
	if err == nil {
		t.Error("expected error for missing file, got nil")
	}
}
