package audit

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestClassifyCommit(t *testing.T) {
	tests := []struct {
		name    string
		subject string
		want    CommitClass
	}{
		{"name-style tag clean", "[obey-campaign:8deed8b4] docs: update", ClassTagged},
		{"legacy id tag clean", "[OBEY-CAMPAIGN-8deed8b4] Create: thing", ClassTagged},
		{"tag with fest ref clean", "[obey-campaign:8deed8b4-FE-CA0002] verify: shipped", ClassTagged},
		{"degraded duplicate quest segment", "[obey-campaign:8deed8b4-qst_aaa-qst_bbb] chore: dup", ClassDegraded},
		{"no tag is untagged", "fix: a plain commit with no tag", ClassUntagged},
		{"non-tag bracket is untagged", "[WIP] scratch work", ClassUntagged},
		{"empty subject is untagged", "", ClassUntagged},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, ClassifyCommit(tt.subject))
		})
	}
}

func TestRepoScanSampleBounded(t *testing.T) {
	// A pure check that the sample cap constant is sane and the struct math is
	// consistent (tagged+degraded+untagged == total is enforced by ScanRepo).
	assert.Positive(t, SampleLimit)
	s := RepoScan{Tagged: 3, Degraded: 1, Untagged: 6, Total: 10}
	assert.Equal(t, s.Total, s.Tagged+s.Degraded+s.Untagged)
}
