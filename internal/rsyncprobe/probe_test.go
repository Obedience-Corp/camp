package rsyncprobe

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Personas are verbatim `--version` output captured from real binaries, not
// paraphrases. The openrsync one is the reason this parser exists: its second
// line advertises a genuine-looking rsync version, so a parser that scans for a
// version before checking the implementation reads it as rsync 2.6.9.
const (
	personaHomebrew344 = `rsync  version 3.4.4  protocol version 32
Copyright (C) 1996-2026 by Andrew Tridgell, Wayne Davison, and others.
Web site: https://rsync.samba.org/
Capabilities:
    64-bit files, 64-bit inums, 64-bit timestamps, 64-bit long ints,`

	personaAppleOpenRsync = `openrsync: protocol version 29
rsync version 2.6.9 compatible`

	personaGenuine269 = `rsync  version 2.6.9  protocol version 29
Copyright (C) 1996-2006 by Andrew Tridgell, Wayne Davison, and others.`

	personaRsync300 = `rsync  version 3.0.0  protocol version 30
Copyright (C) 1996-2008 by Andrew Tridgell, Wayne Davison, and others.`
)

func TestParseVersionPersonas(t *testing.T) {
	tests := []struct {
		name         string
		out          string
		wantKind     Kind
		wantVersion  string
		wantProtocol int
		wantDelta    bool
	}{
		{
			name:         "apple openrsync is not genuine rsync despite its compat line",
			out:          personaAppleOpenRsync,
			wantKind:     KindOpenRsync,
			wantVersion:  "2.6.9",
			wantProtocol: 29,
			wantDelta:    false,
		},
		{
			name:         "genuine rsync below the protocol floor",
			out:          personaGenuine269,
			wantKind:     KindRsync,
			wantVersion:  "2.6.9",
			wantProtocol: 29,
			wantDelta:    false,
		},
		{
			name:         "garbage output is unknown, never trusted",
			out:          "command not found: rsync\nsome shell noise",
			wantKind:     KindUnknown,
			wantProtocol: 0,
			wantDelta:    false,
		},
		{
			name:      "empty output is unknown",
			out:       "   \n  \n",
			wantKind:  KindUnknown,
			wantDelta: false,
		},
		{
			name:         "homebrew rsync 3.4.4 is delta-usable",
			out:          personaHomebrew344,
			wantKind:     KindRsync,
			wantVersion:  "3.4.4",
			wantProtocol: 32,
			wantDelta:    true,
		},
		{
			name:         "protocol 30 exactly is the floor and qualifies",
			out:          personaRsync300,
			wantKind:     KindRsync,
			wantVersion:  "3.0.0",
			wantProtocol: 30,
			wantDelta:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseVersion("/usr/bin/rsync", tt.out)
			if got.Kind != tt.wantKind {
				t.Errorf("Kind = %q, want %q", got.Kind, tt.wantKind)
			}
			if tt.wantVersion != "" && got.Version != tt.wantVersion {
				t.Errorf("Version = %q, want %q", got.Version, tt.wantVersion)
			}
			if got.Protocol != tt.wantProtocol {
				t.Errorf("Protocol = %d, want %d", got.Protocol, tt.wantProtocol)
			}
			if got.DeltaUsable() != tt.wantDelta {
				t.Errorf("DeltaUsable() = %v, want %v (reason: %q)",
					got.DeltaUsable(), tt.wantDelta, got.Reason())
			}
			if got.Binary != "/usr/bin/rsync" {
				t.Errorf("Binary = %q, want the probed path preserved", got.Binary)
			}
		})
	}
}

// TestOpenRsyncIsNeverReadAsGenuine is the regression this package exists for,
// stated on its own so it cannot be lost in a table.
func TestOpenRsyncIsNeverReadAsGenuine(t *testing.T) {
	e := ParseVersion("/usr/bin/rsync", personaAppleOpenRsync)
	if e.Kind == KindRsync {
		t.Fatal("openrsync was identified as genuine rsync; its 'rsync version 2.6.9 compatible' line must not win")
	}
	if e.DeltaUsable() {
		t.Error("openrsync must never be delta-usable")
	}
	if !strings.Contains(e.Reason(), "openrsync") {
		t.Errorf("Reason() = %q, want it to name openrsync so the operator knows why", e.Reason())
	}
}

func TestEngineReason(t *testing.T) {
	tests := []struct {
		name   string
		engine Engine
		want   string
	}{
		{name: "absent", engine: Engine{Kind: KindAbsent}, want: "no rsync found"},
		{name: "unknown", engine: Engine{Kind: KindUnknown}, want: "unrecognized"},
		{name: "openrsync", engine: Engine{Kind: KindOpenRsync, Protocol: 29}, want: "openrsync"},
		{name: "old protocol", engine: Engine{Kind: KindRsync, Protocol: 29}, want: "below 30"},
		{name: "usable has no reason", engine: Engine{Kind: KindRsync, Protocol: 32}, want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.engine.Reason()
			if tt.want == "" {
				if got != "" {
					t.Errorf("Reason() = %q, want empty", got)
				}
				return
			}
			if !strings.Contains(got, tt.want) {
				t.Errorf("Reason() = %q, want it to mention %q", got, tt.want)
			}
		})
	}
}

func TestPairDeltaUsableNeedsBothEnds(t *testing.T) {
	good := Engine{Kind: KindRsync, Version: "3.4.4", Protocol: 32}
	old := Engine{Kind: KindRsync, Version: "2.6.9", Protocol: 29}
	apple := Engine{Kind: KindOpenRsync, Protocol: 29}

	tests := []struct {
		name       string
		pair       Pair
		wantDelta  bool
		wantReason string
	}{
		{
			name:       "weak local decides",
			pair:       Pair{Local: apple, Peer: good},
			wantDelta:  false,
			wantReason: "local: openrsync",
		},
		{
			name:       "weak peer decides",
			pair:       Pair{Local: good, Peer: apple},
			wantDelta:  false,
			wantReason: "peer: openrsync",
		},
		{
			name:       "old protocol on the peer",
			pair:       Pair{Local: good, Peer: old},
			wantDelta:  false,
			wantReason: "peer:",
		},
		{
			name:       "both weak reports local first, the end the user controls",
			pair:       Pair{Local: apple, Peer: apple},
			wantDelta:  false,
			wantReason: "local:",
		},
		{
			name:      "both good",
			pair:      Pair{Local: good, Peer: good},
			wantDelta: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.pair.DeltaUsable(); got != tt.wantDelta {
				t.Fatalf("DeltaUsable() = %v, want %v", got, tt.wantDelta)
			}
			got := tt.pair.Reason()
			if tt.wantReason == "" {
				if got != "" {
					t.Errorf("Reason() = %q, want empty", got)
				}
				return
			}
			if !strings.HasPrefix(got, tt.wantReason) {
				t.Errorf("Reason() = %q, want it to start with %q", got, tt.wantReason)
			}
		})
	}
}

func TestParseProbeOutput(t *testing.T) {
	tests := []struct {
		name       string
		out        string
		wantKind   Kind
		wantBinary string
	}{
		{
			name:     "no marker means no rsync on that end",
			out:      "",
			wantKind: KindAbsent,
		},
		{
			name:     "login shell noise without a marker is still absent",
			out:      "Welcome to peerbox\nLast login: today\n",
			wantKind: KindAbsent,
		},
		{
			name:       "marker plus banner",
			out:        binaryMarker + "/opt/homebrew/bin/rsync\n" + personaHomebrew344,
			wantKind:   KindRsync,
			wantBinary: "/opt/homebrew/bin/rsync",
		},
		{
			name:       "banner before the marker is not attributed to the binary",
			out:        "motd noise\n" + binaryMarker + "/usr/bin/rsync\n" + personaAppleOpenRsync,
			wantKind:   KindOpenRsync,
			wantBinary: "/usr/bin/rsync",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseProbeOutput(tt.out)
			if got.Kind != tt.wantKind {
				t.Errorf("Kind = %q, want %q", got.Kind, tt.wantKind)
			}
			if got.Binary != tt.wantBinary {
				t.Errorf("Binary = %q, want %q", got.Binary, tt.wantBinary)
			}
		})
	}
}

// TestProbeScriptShape pins the properties the script must have: it resolves
// through the shell's own PATH (not a hardcoded path), and a missing rsync
// leaves cleanly rather than failing the hop.
func TestProbeScriptShape(t *testing.T) {
	for _, want := range []string{"command -v rsync", "--version", binaryMarker, "exit 0"} {
		if !strings.Contains(probeScript, want) {
			t.Errorf("probeScript missing %q:\n%s", want, probeScript)
		}
	}
	if strings.Contains(probeScript, "/usr/bin/rsync") {
		t.Error("probeScript must not hardcode a path; resolving the invoked binary is the point")
	}
}

// TestProbeLocalAgainstRealBinary exercises the whole local path — PATH
// resolution, exec, parse — against whatever rsync this machine actually has.
// Fixtures prove the parser; this proves the wiring, which is where a probe
// that reads correctly can still resolve the wrong binary.
//
// Skipped rather than failed where there is no rsync: absence is a supported
// state, and it is covered as a result by the parser tests.
func TestProbeLocalAgainstRealBinary(t *testing.T) {
	if _, err := exec.LookPath("rsync"); err != nil {
		t.Skip("no rsync on PATH; absence is covered by the parser tests")
	}

	engine, err := ProbeLocal(context.Background())
	if err != nil {
		t.Fatalf("ProbeLocal() error = %v", err)
	}
	if engine.Binary == "" || !filepath.IsAbs(engine.Binary) {
		t.Errorf("Binary = %q, want the absolute resolved path", engine.Binary)
	}
	if engine.Kind != KindRsync && engine.Kind != KindOpenRsync {
		t.Fatalf("Kind = %q, want a real implementation identified (engine: %+v)", engine.Kind, engine)
	}
	if engine.Protocol <= 0 {
		t.Errorf("Protocol = %d, want a positive protocol from a real binary", engine.Protocol)
	}
	// Consistency: whatever it found, the verdict and its explanation must agree.
	if engine.DeltaUsable() != (engine.Reason() == "") {
		t.Errorf("DeltaUsable()=%v but Reason()=%q; a usable engine has no reason and vice versa",
			engine.DeltaUsable(), engine.Reason())
	}
	t.Logf("probed real rsync: %+v (delta-usable=%v)", engine, engine.DeltaUsable())
}
