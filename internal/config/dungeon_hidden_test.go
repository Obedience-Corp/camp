package config

import (
	"context"
	"testing"
)

func TestGlobalConfig_ResolveDungeonHidden(t *testing.T) {
	trueVal := true
	falseVal := false

	tests := []struct {
		name string
		cfg  *GlobalConfig
		want bool
	}{
		{name: "nil config defaults true", cfg: nil, want: true},
		{name: "unset field defaults true", cfg: &GlobalConfig{}, want: true},
		{name: "explicit true", cfg: &GlobalConfig{DungeonHidden: &trueVal}, want: true},
		{name: "explicit false", cfg: &GlobalConfig{DungeonHidden: &falseVal}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cfg.ResolveDungeonHidden(); got != tt.want {
				t.Errorf("ResolveDungeonHidden() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLoadGlobalConfig_DungeonHiddenRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	ctx := context.Background()

	cfg, err := LoadGlobalConfig(ctx)
	if err != nil {
		t.Fatalf("LoadGlobalConfig() error = %v", err)
	}
	if !cfg.ResolveDungeonHidden() {
		t.Fatalf("fresh global config should default dungeon_hidden to true")
	}

	falseVal := false
	cfg.DungeonHidden = &falseVal
	if err := SaveGlobalConfig(ctx, cfg); err != nil {
		t.Fatalf("SaveGlobalConfig() error = %v", err)
	}

	reloaded, err := LoadGlobalConfig(ctx)
	if err != nil {
		t.Fatalf("LoadGlobalConfig() reload error = %v", err)
	}
	if reloaded.ResolveDungeonHidden() {
		t.Fatalf("expected persisted dungeon_hidden=false to round-trip as false")
	}
}
