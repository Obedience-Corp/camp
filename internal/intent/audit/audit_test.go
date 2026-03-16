package audit

import (
	"path/filepath"
	"testing"
)

func TestFilePath(t *testing.T) {
	tests := []struct {
		name       string
		intentsDir string
		want       string
	}{
		{
			name:       "standard path",
			intentsDir: ".campaign/intents",
			want:       filepath.Join(".campaign/intents", AuditFile),
		},
		{
			name:       "absolute path",
			intentsDir: "/home/user/campaign/.campaign/intents",
			want:       filepath.Join("/home/user/campaign/.campaign/intents", AuditFile),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FilePath(tt.intentsDir)
			if got != tt.want {
				t.Errorf("FilePath(%q) = %q, want %q", tt.intentsDir, got, tt.want)
			}
		})
	}
}
