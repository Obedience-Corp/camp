package remote

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Obedience-Corp/camp/internal/machines"
)

func testMachine(host string) *machines.Machine {
	return &machines.Machine{ID: "mac-studio", Host: host}
}

// The dial decision matrix. Each row records which probes may be consulted, so
// a regression that starts guessing addresses (or paying for lookups it does
// not need) fails here, not in the field.
func TestResolveEndpointCauseMatrix(t *testing.T) {
	resolves := func(context.Context, string) ([]string, error) { return []string{"100.64.0.9"}, nil }
	fails := func(context.Context, string) ([]string, error) { return nil, errors.New("no such host") }
	emptyAnswer := func(context.Context, string) ([]string, error) { return nil, nil }
	peerHit := func(context.Context, string) (string, bool) { return "100.72.165.77", true }
	peerMiss := func(context.Context, string) (string, bool) { return "", false }

	tests := []struct {
		name        string
		host        string
		lookup      func(context.Context, string) ([]string, error)
		peer        func(context.Context, string) (string, bool)
		wantDial    string
		wantViaPeer bool
		allowLookup bool
		allowPeer   bool
	}{
		{
			name: "name resolves: direct, peer table never consulted",
			host: "mac-studio.example-net.ts.net", lookup: resolves, peer: peerHit,
			wantDial: "mac-studio.example-net.ts.net", allowLookup: true,
		},
		{
			name: "IP literal: direct, no lookup at all",
			host: "100.72.165.77", lookup: resolves, peer: peerHit,
			wantDial: "100.72.165.77",
		},
		{
			name: "empty host: direct, nothing consulted",
			host: "", lookup: resolves, peer: peerHit,
			wantDial: "",
		},
		{
			name: "ts.net fails, peer found: fallback address",
			host: "mac-studio.example-net.ts.net", lookup: fails, peer: peerHit,
			wantDial: "100.72.165.77", wantViaPeer: true, allowLookup: true, allowPeer: true,
		},
		{
			name: "empty resolver answer counts as failure",
			host: "mac-studio.example-net.ts.net", lookup: emptyAnswer, peer: peerHit,
			wantDial: "100.72.165.77", wantViaPeer: true, allowLookup: true, allowPeer: true,
		},
		{
			name: "ts.net fails, peer absent: direct, ssh left to fail",
			host: "mac-studio.example-net.ts.net", lookup: fails, peer: peerMiss,
			wantDial: "mac-studio.example-net.ts.net", allowLookup: true, allowPeer: true,
		},
		{
			name: "non-ts.net fails: direct, peer table never consulted",
			host: "devbox.example.com", lookup: fails, peer: peerHit,
			wantDial: "devbox.example.com", allowLookup: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lookupCalled, peerCalled := false, false
			e := ResolveEndpointWith(context.Background(), testMachine(tt.host), EndpointProbes{
				LookupHost: func(ctx context.Context, h string) ([]string, error) {
					lookupCalled = true
					return tt.lookup(ctx, h)
				},
				PeerAddress: func(ctx context.Context, n string) (string, bool) {
					peerCalled = true
					return tt.peer(ctx, n)
				},
			})
			if e.DialHost != tt.wantDial || e.ViaPeer != tt.wantViaPeer {
				t.Fatalf("got DialHost=%q ViaPeer=%v, want %q/%v", e.DialHost, e.ViaPeer, tt.wantDial, tt.wantViaPeer)
			}
			if lookupCalled && !tt.allowLookup {
				t.Error("resolver was consulted when it must not be")
			}
			if peerCalled && !tt.allowPeer {
				t.Error("peer table was consulted when it must not be")
			}
			if e.Machine.Host != tt.host {
				t.Errorf("Machine.Host mutated to %q", e.Machine.Host)
			}
		})
	}
}

// The escape hatch: with CAMP_NO_PEER_FALLBACK set, camp fails exactly the way
// bare ssh would — no lookup, no peer table, direct dial.
func TestResolveEndpointEscapeHatch(t *testing.T) {
	t.Setenv(NoPeerFallbackEnv, "1")
	consulted := false
	e := ResolveEndpointWith(context.Background(), testMachine("mac-studio.example-net.ts.net"), EndpointProbes{
		LookupHost: func(context.Context, string) ([]string, error) {
			consulted = true
			return nil, errors.New("no such host")
		},
		PeerAddress: func(context.Context, string) (string, bool) {
			consulted = true
			return "100.72.165.77", true
		},
	})
	if consulted {
		t.Error("probes consulted despite " + NoPeerFallbackEnv)
	}
	if e.ViaPeer || e.DialHost != "mac-studio.example-net.ts.net" {
		t.Fatalf("got %+v, want direct", e)
	}
}

// A fallback endpoint must pin known_hosts identity to the configured name in
// BOTH opts builders and in the pasteable probe line, or accept-new silently
// files a second key under the IP.
func TestEndpointFallbackPinsHostKeyAlias(t *testing.T) {
	m := &machines.Machine{ID: "mac-studio", Host: "mac-studio.example-net.ts.net", SSHUser: "lance", IdentityFile: "/keys/id"}
	e := Endpoint{Machine: m, DialHost: "100.72.165.77", ViaPeer: true}

	alias := "HostKeyAlias=mac-studio.example-net.ts.net"
	for name, opts := range map[string][]string{
		"Opts":          e.Opts(),
		"OptsReuseOnly": e.OptsReuseOnly(),
	} {
		joined := strings.Join(opts, " ")
		if !strings.Contains(joined, alias) {
			t.Errorf("%s missing %s: %s", name, alias, joined)
		}
	}

	probe := e.ProbeCommand()
	if !strings.Contains(probe, alias) || !strings.Contains(probe, "lance@100.72.165.77") {
		t.Errorf("probe does not reproduce the real dial: %s", probe)
	}
	if e.Target() != "lance@100.72.165.77" {
		t.Errorf("Target = %q", e.Target())
	}
}

// A direct endpoint must not grow an alias, and the package-level wrappers
// must stay byte-identical to the endpoint form so unmigrated call sites keep
// today's behavior.
func TestDirectEndpointMatchesLegacyWrappers(t *testing.T) {
	m := &machines.Machine{ID: "devbox", Host: "devbox.example-net.ts.net", SSHUser: "u", IdentityFile: "/keys/id"}
	e := Direct(m)
	if got, want := e.Target(), Target(m); got != want {
		t.Errorf("Target: %q != %q", got, want)
	}
	if got, want := strings.Join(e.Opts(), " "), strings.Join(Opts(m), " "); got != want {
		t.Errorf("Opts: %q != %q", got, want)
	}
	if got, want := strings.Join(e.OptsReuseOnly(), " "), strings.Join(OptsReuseOnly(m), " "); got != want {
		t.Errorf("OptsReuseOnly: %q != %q", got, want)
	}
	if got, want := e.ProbeCommand(), ProbeCommand(m); got != want {
		t.Errorf("ProbeCommand: %q != %q", got, want)
	}
	if strings.Contains(strings.Join(e.Opts(), " "), "HostKeyAlias") {
		t.Error("direct endpoint grew a HostKeyAlias")
	}
}

// The alias pins to the normalized configured name even when machines.yaml
// carries the trailing FQDN dot.
func TestEndpointAliasNormalizesTrailingDot(t *testing.T) {
	e := Endpoint{Machine: testMachine("mac-studio.example-net.ts.net."), DialHost: "100.72.165.77", ViaPeer: true}
	if got := strings.Join(e.Opts(), " "); !strings.Contains(got, "HostKeyAlias=mac-studio.example-net.ts.net ") &&
		!strings.HasSuffix(got, "HostKeyAlias=mac-studio.example-net.ts.net") {
		t.Errorf("alias not normalized: %s", got)
	}
}

func TestTransferTargetBracketsIPv6(t *testing.T) {
	tests := []struct {
		name string
		e    Endpoint
		path string
		want string
	}{
		{
			name: "v4 fallback, plain",
			e:    Endpoint{Machine: testMachine("h.example-net.ts.net"), DialHost: "100.72.165.77", ViaPeer: true},
			path: "/root/file", want: "100.72.165.77:/root/file",
		},
		{
			name: "v6 fallback is bracketed",
			e:    Endpoint{Machine: testMachine("h.example-net.ts.net"), DialHost: "fd7a:115c:a1e0::1", ViaPeer: true},
			path: "/root/file", want: "[fd7a:115c:a1e0::1]:/root/file",
		},
		{
			name: "hostname with user, no brackets",
			e:    Endpoint{Machine: &machines.Machine{Host: "h.example-net.ts.net", SSHUser: "lance"}, DialHost: "h.example-net.ts.net"},
			path: "/root/file", want: "lance@h.example-net.ts.net:/root/file",
		},
		{
			name: "v6 with user: user outside the brackets",
			e:    Endpoint{Machine: &machines.Machine{Host: "h.example-net.ts.net", SSHUser: "lance"}, DialHost: "fd7a::1", ViaPeer: true},
			path: "/f", want: "lance@[fd7a::1]:/f",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.e.TransferTarget(tt.path); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFallbackNotice(t *testing.T) {
	direct := Direct(testMachine("h.example-net.ts.net"))
	if direct.FallbackNotice() != "" {
		t.Error("direct endpoint produced a notice")
	}
	e := Endpoint{Machine: testMachine("mac-studio.example-net.ts.net"), DialHost: "100.72.165.77", ViaPeer: true}
	n := e.FallbackNotice()
	for _, want := range []string{"mac-studio.example-net.ts.net", "100.72.165.77", "camp machine diagnose mac-studio"} {
		if !strings.Contains(n, want) {
			t.Errorf("notice missing %q: %s", want, n)
		}
	}
}

// Q7: check-mode classification parses stderr content, not the target, so a
// fallback dial that hits Tailscale SSH check mode still surfaces the URL.
func TestCheckModeClassificationSurvivesFallbackTarget(t *testing.T) {
	stderr := "# Tailscale SSH requires an additional check.\n# To authenticate, visit: https://login.tailscale.com/a/abc123"
	err := sshExitError("lance@100.72.165.77", 255, stderr, errors.New("exit status 255"))
	if url := TailscaleCheckURL(err); url != "https://login.tailscale.com/a/abc123" {
		t.Fatalf("check URL lost on fallback target: %q", url)
	}
}
