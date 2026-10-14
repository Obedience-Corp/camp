//go:build windows

package artifacts

import (
	"context"
	"strings"
	"testing"

	"github.com/Obedience-Corp/camp/internal/peer"
	"github.com/Obedience-Corp/camp/internal/rsyncprobe"
)

type failIfProbed struct{ called *bool }

func (p failIfProbed) Both(context.Context, string, rsyncprobe.ShellRunner) (rsyncprobe.Pair, error) {
	*p.called = true
	return rsyncprobe.Pair{}, nil
}

func TestWindowsArtifactPullCannotReportIncompleteCopyAsSynced(t *testing.T) {
	probed := false
	r := PullWithProber(context.Background(), `C:\no-such-camp`, peer.FromPath("source", `C:\no-such-peer`),
		Root{Path: "media"}, failIfProbed{called: &probed})
	if r.Synced || r.Partial || probed || !strings.Contains(r.Warning, "unavailable on Windows") {
		t.Fatalf("Windows pull result = %+v, probed=%v; want hard failure before transfer", r, probed)
	}
}
