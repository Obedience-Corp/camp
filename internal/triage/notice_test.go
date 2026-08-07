package triage

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestTriageBannerText covers the wording every high-traffic command shares.
//
// The banner is the only part of triage most people see on most days, so its
// two jobs are pinned here: it must say something when there is something to
// say, and it must say nothing otherwise. A banner that fires on a fresh
// campaign is noise every user learns to ignore, which costs the notice its
// only chance to be read.
func TestTriageBannerText(t *testing.T) {
	tests := []struct {
		name           string
		daysSince      int
		staleAfterDays int
		changedRows    int
		want           string
	}{
		{
			name:           "changed rows outrank age and say how many",
			daysSince:      1,
			staleAfterDays: 14,
			changedRows:    7,
			want:           "7 workitems have changed since the last triage — run: camp triage start",
		},
		{
			name:           "one changed row reads as one row",
			daysSince:      1,
			staleAfterDays: 14,
			changedRows:    1,
			want:           "1 workitem has changed since the last triage — run: camp triage start",
		},
		{
			name:           "an old run with nothing changed reports its age",
			daysSince:      20,
			staleAfterDays: 14,
			want:           "last triage was 20 days ago — run: camp triage start",
		},
		{
			name:           "exactly at the threshold is not yet stale",
			daysSince:      14,
			staleAfterDays: 14,
		},
		{
			name:           "a recent run with nothing changed says nothing",
			daysSince:      2,
			staleAfterDays: 14,
		},
		{
			name:           "a zero threshold disables the age reminder",
			daysSince:      900,
			staleAfterDays: 0,
		},
		{
			name:           "a clock that ran backwards is not a reminder",
			daysSince:      -3,
			staleAfterDays: 14,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want,
				TriageBannerText(tt.daysSince, tt.staleAfterDays, tt.changedRows))
		})
	}
}

// TestTriageBannerTextHonorsARaisedThreshold is the regression for a banner
// that read camp's built-in threshold instead of the campaign's.
//
// `runs.stale_after_days` ships in the scaffolded profile with a comment
// saying it is the `camp status` notice threshold. A campaign that raises it
// to 30 must go quiet for the same run that a campaign leaving it at 14 is
// nagged about; otherwise the key is one the operator can set and camp
// ignores.
func TestTriageBannerTextHonorsARaisedThreshold(t *testing.T) {
	const daysSince = 20

	assert.NotEmpty(t, TriageBannerText(daysSince, 14, 0),
		"the default threshold must still fire at 20 days")
	assert.Empty(t, TriageBannerText(daysSince, 30, 0),
		"a campaign that raised the threshold to 30 must not be nagged at 20 days")
}
