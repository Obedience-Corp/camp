package main

import (
	"context"
	"strings"
	"testing"
)

func TestCheckReverseReachabilityCauseMatrix(t *testing.T) {
	yes := func(context.Context) bool { return true }
	no := func(context.Context) bool { return false }

	tests := []struct {
		name        string
		probes      reverseProbes
		wantCapable bool
		wantHint    string
		forbidden   string
	}{
		{
			name: "darwin, Remote Login off",
			probes: reverseProbes{
				GOOS:          "darwin",
				SSHDListening: no,
				RemoteLoginOn: func(context.Context) (bool, bool) { return false, true },
			},
			wantHint:  "macOS Remote Login is off",
			forbidden: "nothing can ssh in",
		},
		{
			name: "darwin, Remote Login on but nothing listening",
			probes: reverseProbes{
				GOOS:          "darwin",
				SSHDListening: no,
				RemoteLoginOn: func(context.Context) (bool, bool) { return true, true },
			},
			// Already on: must not tell the operator to enable Remote Login again.
			wantHint:  "Remote Login reports on but nothing is accepting on port 22",
			forbidden: "enable",
		},
		{
			name: "darwin, state unreadable, no sshd",
			probes: reverseProbes{
				GOOS:          "darwin",
				SSHDListening: no,
				RemoteLoginOn: func(context.Context) (bool, bool) { return false, false },
			},
			// Unknowable state must not produce a confident "it is off" claim.
			wantHint:  "no sshd is listening",
			forbidden: "Remote Login is off",
		},
		{
			name:     "linux, no sshd",
			probes:   reverseProbes{GOOS: "linux", SSHDListening: no},
			wantHint: "systemctl enable --now sshd",
		},
		{
			name:        "sshd up, tailscale down",
			probes:      reverseProbes{GOOS: "linux", SSHDListening: yes, TailscaleUp: no},
			wantCapable: true,
			wantHint:    "tailscale is not up here",
		},
		{
			name:        "sshd up, tailscale up",
			probes:      reverseProbes{GOOS: "linux", SSHDListening: yes, TailscaleUp: yes},
			wantCapable: true,
			wantHint:    "camp does not manage",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := checkReverseReachability(context.Background(), tt.probes)
			if got.Capable != tt.wantCapable {
				t.Errorf("Capable = %v, want %v (hint %q)", got.Capable, tt.wantCapable, got.Hint)
			}
			if !strings.Contains(got.Hint, tt.wantHint) {
				t.Errorf("hint %q must contain %q", got.Hint, tt.wantHint)
			}
			if tt.forbidden != "" && strings.Contains(strings.ToLower(got.Hint), strings.ToLower(tt.forbidden)) {
				t.Errorf("hint %q must not contain %q", got.Hint, tt.forbidden)
			}
		})
	}
}

func TestParseRemoteLoginOutput(t *testing.T) {
	tests := []struct {
		name         string
		out          string
		wantOn       bool
		wantKnowable bool
	}{
		{name: "classic On", out: "Remote Login: On\n", wantOn: true, wantKnowable: true},
		{name: "classic Off", out: "Remote Login: Off\n", wantOn: false, wantKnowable: true},
		{name: "lowercase", out: "remote login: on", wantOn: true, wantKnowable: true},
		{name: "extra padding", out: "  Remote Login:   Off  \n", wantOn: false, wantKnowable: true},
		// "Off" contains "on" as a substring; a naive Contains("on") would misread it.
		// The line parser must stay exact.
		{name: "garbage success", out: "something on somewhere\n", wantKnowable: false},
		{name: "empty", out: "", wantKnowable: false},
		{name: "multiline with On", out: "header\nRemote Login: On\nfooter\n", wantOn: true, wantKnowable: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			on, knowable := parseRemoteLoginOutput(tt.out)
			if knowable != tt.wantKnowable || on != tt.wantOn {
				t.Errorf("parseRemoteLoginOutput(%q) = (%v, %v), want (%v, %v)",
					tt.out, on, knowable, tt.wantOn, tt.wantKnowable)
			}
		})
	}
}

func TestReverseHintsNeverOfferToFixAnything(t *testing.T) {
	// D003: diagnose, never manage. Each hint names one action for the OPERATOR;
	// none may imply camp will do it.
	probeSets := []reverseProbes{
		{GOOS: "darwin", SSHDListening: func(context.Context) bool { return false },
			RemoteLoginOn: func(context.Context) (bool, bool) { return false, true }},
		{GOOS: "linux", SSHDListening: func(context.Context) bool { return false }},
		{GOOS: "linux", SSHDListening: func(context.Context) bool { return true },
			TailscaleUp: func(context.Context) bool { return false }},
		{GOOS: "linux", SSHDListening: func(context.Context) bool { return true },
			TailscaleUp: func(context.Context) bool { return true }},
	}
	for _, p := range probeSets {
		hint := checkReverseReachability(context.Background(), p).Hint
		for _, forbidden := range []string{"camp will", "camp can enable", "run 'camp machine fix", "automatically"} {
			if strings.Contains(strings.ToLower(hint), strings.ToLower(forbidden)) {
				t.Errorf("hint offers to manage: %q", hint)
			}
		}
		if hint == "" {
			t.Error("every cause must produce a hint")
		}
	}
}

func TestReverseReachabilityHonorsContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	// A cancelled context must not hang or panic; the probes see a dead context
	// and report the not-listening path.
	got := checkReverseReachability(ctx, defaultReverseProbes())
	if got.Hint == "" {
		t.Error("want a hint even under cancellation")
	}
}

// The reverse probe must reach the same conclusion as a hop's dial fallback:
// when MagicDNS is broken locally, the machine's own name does not connect,
// and the probe retries the tailnet self-address before declaring "no sshd"
// (design WI-44f57e, Q7).
func TestSSHDListeningFallsBackToTailnetSelfAddress(t *testing.T) {
	ctx := context.Background()
	dialed := []string{}
	probes := listenProbes{
		ReachableName: func(context.Context) string { return "archdtop.tail37114b.ts.net" },
		SelfAddress:   func(context.Context) (string, bool) { return "100.94.55.106", true },
		Dial: func(_ context.Context, addr string) bool {
			dialed = append(dialed, addr)
			return addr == "100.94.55.106:22"
		},
	}
	if !sshdListeningWith(ctx, probes) {
		t.Fatalf("want listening via self-address fallback; dialed %v", dialed)
	}
	if len(dialed) != 2 || dialed[0] != "archdtop.tail37114b.ts.net:22" || dialed[1] != "100.94.55.106:22" {
		t.Errorf("dial order = %v, want name then self address", dialed)
	}

	// A non-MagicDNS name gets no tailnet retry: guessing an address for an
	// ordinary hostname would be worse than failing.
	probes.ReachableName = func(context.Context) string { return "workstation.internal" }
	dialed = nil
	if sshdListeningWith(ctx, probes) {
		t.Error("plain hostname must not be rescued by the tailnet address")
	}
	if len(dialed) != 1 {
		t.Errorf("dialed %v, want only the configured name", dialed)
	}

	// Refused on both addresses is the answer; loopback must not turn it into
	// a yes.
	probes.ReachableName = func(context.Context) string { return "archdtop.tail37114b.ts.net" }
	probes.Dial = func(_ context.Context, addr string) bool { return addr == "127.0.0.1:22" }
	if sshdListeningWith(ctx, probes) {
		t.Error("refused name+self address must not fall back to loopback")
	}

	// No name at all: loopback is the last honest resort.
	probes.ReachableName = func(context.Context) string { return "" }
	if !sshdListeningWith(ctx, probes) {
		t.Error("with no reachable name, loopback should decide")
	}
}
