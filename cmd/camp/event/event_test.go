package event

import (
	"testing"

	"github.com/Obedience-Corp/camp/pkg/ledgerkit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseKind(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    ledgerkit.Kind
		wantErr bool
	}{
		{"valid decided", "decided", ledgerkit.KindDecided, false},
		{"case-insensitive", "Created", ledgerkit.KindCreated, false},
		{"trimmed", "  completed  ", ledgerkit.KindCompleted, false},
		{"empty is error", "", "", true},
		{"unknown is error", "frobnicate", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseKind(tt.in)
			if tt.wantErr {
				require.Error(t, err)
				// The error enumerates every valid kind (enumerate-and-teach bar).
				for _, k := range validKinds {
					assert.Contains(t, err.Error(), string(k))
				}
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestParseEvidence(t *testing.T) {
	refs, warnings, err := parseEvidence("/camp", []string{
		"https://example.com/render.mp4",
		"camp@deadbeef1234",
		"renders/missing_v3.mp4",
		"  ", // blank is skipped
	})
	require.NoError(t, err)
	require.Len(t, refs, 3)

	assert.Equal(t, ledgerkit.EvidenceURL, refs[0].Type)
	assert.Equal(t, "https://example.com/render.mp4", refs[0].URL)

	assert.Equal(t, ledgerkit.EvidenceCommit, refs[1].Type)
	assert.Equal(t, "camp", refs[1].Repo)
	assert.Equal(t, "deadbeef1234", refs[1].SHA)

	assert.Equal(t, ledgerkit.EvidencePath, refs[2].Type)
	assert.Equal(t, "renders/missing_v3.mp4", refs[2].Path)

	// A path that does not exist is a warning (media files move), not a failure.
	require.Len(t, warnings, 1)
	assert.Contains(t, warnings[0], "does not exist")
	assert.Contains(t, warnings[0], "renders/missing_v3.mp4")
}

func TestKindListEnumeratesAll(t *testing.T) {
	list := kindList()
	for _, k := range validKinds {
		assert.Contains(t, list, string(k))
	}
}
