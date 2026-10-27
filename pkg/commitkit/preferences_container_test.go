//go:build container_fs

package commitkit_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Obedience-Corp/camp/pkg/commitkit"
	"github.com/stretchr/testify/require"
)

func TestCommitTagsEnabledSettings(t *testing.T) {
	for _, tt := range []struct {
		name, global, local       string
		standalone, want, wantErr bool
	}{
		{name: "defaults", want: true},
		{name: "global disable", global: `{"commit":{"disable_commit_tags":true}}`},
		{name: "local disable", local: `{"commit":{"disable_commit_tags":true}}`},
		{name: "local enable replaces global", global: `{"commit":{"disable_commit_tags":true}}`, local: `{"commit":{"disable_commit_tags":false}}`, want: true},
		{name: "empty local block replaces global", global: `{"commit":{"disable_commit_tags":true}}`, local: `{"commit":{}}`, want: true},
		{name: "standalone global", global: `{"commit":{"disable_commit_tags":true}}`, standalone: true},
		{name: "invalid global", global: `{`, wantErr: true},
		{name: "invalid local", local: `{`, wantErr: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			configRoot := t.TempDir()
			t.Setenv("XDG_CONFIG_HOME", configRoot)
			write := func(path, value string) {
				if value == "" {
					return
				}
				require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
				require.NoError(t, os.WriteFile(path, []byte(value), 0o644))
			}
			write(filepath.Join(configRoot, "obey", "campaign", "config.json"), tt.global)
			write(filepath.Join(root, ".campaign", "settings", "local.json"), tt.local)
			if tt.standalone {
				root = ""
			}
			enabled, err := commitkit.CommitTagsEnabled(context.Background(), root)
			if tt.wantErr {
				require.Error(t, err)
				require.False(t, enabled)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, enabled)
		})
	}
}
