package commitkit_test

import (
	"testing"

	"github.com/Obedience-Corp/camp/pkg/commitkit"
)

func TestJoinMessages(t *testing.T) {
	cases := []struct {
		name     string
		messages []string
		want     string
	}{
		{
			name:     "nil is empty",
			messages: nil,
			want:     "",
		},
		{
			name:     "single value unchanged",
			messages: []string{"subject only"},
			want:     "subject only",
		},
		{
			name:     "single value with embedded newlines preserved",
			messages: []string{"subject\n\nauthored body\nsecond line"},
			want:     "subject\n\nauthored body\nsecond line",
		},
		{
			name:     "two values join with blank line",
			messages: []string{"subject", "body paragraph"},
			want:     "subject\n\nbody paragraph",
		},
		{
			name:     "three values each their own paragraph",
			messages: []string{"subject", "para one", "para two"},
			want:     "subject\n\npara one\n\npara two",
		},
		{
			name:     "empty string value alone yields empty",
			messages: []string{""},
			want:     "",
		},
		{
			name:     "leading empty dropped git-style",
			messages: []string{"", "body after empty"},
			want:     "body after empty",
		},
		{
			name:     "trailing empty dropped git-style",
			messages: []string{"real subject", ""},
			want:     "real subject",
		},
		{
			name:     "interior empty collapses without double blank",
			messages: []string{"subject", "", "body"},
			want:     "subject\n\nbody",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := commitkit.JoinMessages(tc.messages); got != tc.want {
				t.Fatalf("JoinMessages(%q) = %q, want %q", tc.messages, got, tc.want)
			}
		})
	}
}

// TestJoinMessages_TagLandsOnSubject proves the join runs before tag prepend so
// the campaign tag lands on the subject line, not a body paragraph.
func TestJoinMessages_TagLandsOnSubject(t *testing.T) {
	joined := commitkit.JoinMessages([]string{"test: subject", "body paragraph"})
	tagged := commitkit.PrependContextTagsFullNamed("obey-campaign", "8deed8b4", "", "", "", joined)
	want := "[obey-campaign:8deed8b4] test: subject\n\nbody paragraph"
	if tagged != want {
		t.Fatalf("tagged = %q, want %q", tagged, want)
	}
}
