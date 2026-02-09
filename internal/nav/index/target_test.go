package index

import (
	"testing"
)

func TestTarget_RelativePath(t *testing.T) {
	tests := []struct {
		name         string
		targetPath   string
		campaignRoot string
		want         string
	}{
		{
			name:         "simple relative path",
			targetPath:   "/home/user/campaign/projects/camp",
			campaignRoot: "/home/user/campaign",
			want:         "projects/camp",
		},
		{
			name:         "nested relative path",
			targetPath:   "/home/user/campaign/projects/camp/cmd/camp",
			campaignRoot: "/home/user/campaign",
			want:         "projects/camp/cmd/camp",
		},
		{
			name:         "same path",
			targetPath:   "/home/user/campaign",
			campaignRoot: "/home/user/campaign",
			want:         ".",
		},
		{
			name:         "sibling path",
			targetPath:   "/home/user/other",
			campaignRoot: "/home/user/campaign",
			want:         "../other",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			target := &Target{Path: tt.targetPath}
			got := target.RelativePath(tt.campaignRoot)
			if got != tt.want {
				t.Errorf("RelativePath() = %v, want %v", got, tt.want)
			}
		})
	}
}
