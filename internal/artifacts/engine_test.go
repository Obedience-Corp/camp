package artifacts

import (
	"strings"
	"testing"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/rsyncprobe"
)

var (
	genuine32 = rsyncprobe.Engine{Kind: rsyncprobe.KindRsync, Version: "3.4.4", Protocol: 32, Binary: "/opt/homebrew/bin/rsync"}
	genuine29 = rsyncprobe.Engine{Kind: rsyncprobe.KindRsync, Version: "2.6.9", Protocol: 29}
	openRsync = rsyncprobe.Engine{Kind: rsyncprobe.KindOpenRsync, Version: "2.6.9", Protocol: 29, Binary: "/usr/bin/rsync"}
	absent    = rsyncprobe.Engine{Kind: rsyncprobe.KindAbsent}
	unknown   = rsyncprobe.Engine{Kind: rsyncprobe.KindUnknown}
)

func TestEngineForSelection(t *testing.T) {
	tests := []struct {
		name       string
		pair       rsyncprobe.Pair
		probeErr   error
		wantEngine string
		wantReason string
		// wantWFlag is whether -W should be passed: only genuine rsync
		// documents --whole-file, and openrsync is whole-file regardless.
		wantWFlag bool
	}{
		{
			name:       "a failed probe takes the honest slow path, never an optimistic delta",
			probeErr:   camperrors.New("ssh unreachable"),
			wantEngine: EngineWholeFile,
			wantReason: "probe failed",
		},
		{
			name:       "openrsync locally",
			pair:       rsyncprobe.Pair{Local: openRsync, Peer: genuine32},
			wantEngine: EngineWholeFile,
			wantReason: "local: openrsync",
			wantWFlag:  false,
		},
		{
			name:       "openrsync on the peer, genuine local still gets -W",
			pair:       rsyncprobe.Pair{Local: genuine32, Peer: openRsync},
			wantEngine: EngineWholeFile,
			wantReason: "peer: openrsync",
			wantWFlag:  true,
		},
		{
			name:       "old genuine rsync on the peer",
			pair:       rsyncprobe.Pair{Local: genuine32, Peer: genuine29},
			wantEngine: EngineWholeFile,
			wantReason: "peer:",
			wantWFlag:  true,
		},
		{
			name:       "no rsync on the peer",
			pair:       rsyncprobe.Pair{Local: genuine32, Peer: absent},
			wantEngine: EngineWholeFile,
			wantReason: "peer: no rsync found",
			wantWFlag:  true,
		},
		{
			name:       "unidentified engine fails closed to whole-file",
			pair:       rsyncprobe.Pair{Local: genuine32, Peer: unknown},
			wantEngine: EngineWholeFile,
			wantReason: "peer: unrecognized",
			wantWFlag:  true,
		},
		{
			name:       "both ends genuine and modern",
			pair:       rsyncprobe.Pair{Local: genuine32, Peer: genuine32},
			wantEngine: EngineDelta,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := engineFor(tt.pair, tt.probeErr)
			if got.name != tt.wantEngine {
				t.Fatalf("engine = %q, want %q (reason %q)", got.name, tt.wantEngine, got.reason)
			}
			if tt.wantReason == "" {
				if got.reason != "" {
					t.Errorf("delta engine carried reason %q, want none", got.reason)
				}
				return
			}
			if !strings.Contains(got.reason, tt.wantReason) {
				t.Errorf("reason = %q, want it to mention %q", got.reason, tt.wantReason)
			}
			if got.wholeFileFlag != tt.wantWFlag {
				t.Errorf("wholeFileFlag = %v, want %v", got.wholeFileFlag, tt.wantWFlag)
			}
		})
	}
}

func TestStagingArgs(t *testing.T) {
	const partial = "/campaigns/demo/.campaign/cache/rsync-staging/media-partial"

	t.Run("delta engine adds nothing", func(t *testing.T) {
		if args := (transferEngine{name: EngineDelta}).stagingArgs(partial); len(args) != 0 {
			t.Errorf("delta stagingArgs = %v, want none", args)
		}
	})

	t.Run("whole-file resumes at file granularity", func(t *testing.T) {
		args := (transferEngine{name: EngineWholeFile}).stagingArgs(partial)
		joined := strings.Join(args, " ")
		if !strings.Contains(joined, "--partial-dir="+partial) {
			t.Errorf("args = %v, want --partial-dir pointing outside staging", args)
		}
		if strings.Contains(joined, "-W") {
			t.Error("-W must not be passed when the local binary is not genuine rsync")
		}
	})

	t.Run("genuine rsync disables the delta algorithm explicitly", func(t *testing.T) {
		args := (transferEngine{name: EngineWholeFile, wholeFileFlag: true}).stagingArgs(partial)
		found := false
		for _, a := range args {
			if a == "-W" {
				found = true
			}
		}
		if !found {
			t.Errorf("args = %v, want -W so genuine rsync sends whole files", args)
		}
	})

	// The partial directory must never sit under the staging tree: the merge
	// step moves every regular file it finds there into the live root, so an
	// in-flight partial would be published as a complete artifact.
	t.Run("partial dir is the caller's path, kept outside staging", func(t *testing.T) {
		args := (transferEngine{name: EngineWholeFile}).stagingArgs(partial)
		for _, a := range args {
			if strings.HasPrefix(a, "--partial-dir=") &&
				strings.TrimPrefix(a, "--partial-dir=") != partial {
				t.Errorf("partial dir = %q, want exactly the caller's path", a)
			}
		}
	})
}
