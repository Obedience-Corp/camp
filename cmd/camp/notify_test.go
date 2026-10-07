package main

import (
	"strings"
	"testing"

	"github.com/Obedience-Corp/camp/internal/notice"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

func TestWriteDismissedNoticesNamesWhatEachIDIsAbout(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.Ascii)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })

	declared := notice.SubjectID(notice.KindNeverSynced, "media/renders")
	removed := notice.SubjectID(notice.KindManifestDrift, "old/root")
	var out strings.Builder
	writeDismissedNotices(&out, []notice.Dismissed{
		{ID: declared, Subject: "media/renders", Summary: notice.Summary(declared), At: "2026-10-07"},
		{ID: removed, Summary: notice.Summary(removed), At: "2026-10-06"},
		{ID: "artifact-roots-missing-locally", Summary: notice.Summary("artifact-roots-missing-locally"), At: "2026-10-05"},
		{ID: "from-a-newer-camp", At: "2026-10-04"},
	})
	got := out.String()

	for _, want := range []string{
		"  " + declared + "  media/renders\n",
		"never synced to another machine, dismissed 2026-10-07",
		"  " + removed + "  (root no longer declared)\n",
		"  artifact-roots-missing-locally\n",
		"not on this machine, dismissed 2026-10-05",
		"a notice this version of Camp does not raise, dismissed 2026-10-04",
		"Restore one: camp notify restore <id>",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q:\n%s", want, got)
		}
	}
}
