package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

// health builds a TailscaleHealth seam returning a fixed answer.
func health(msgs []string, knowable bool) func(context.Context) ([]string, bool) {
	return func(context.Context) ([]string, bool) { return msgs, knowable }
}

func lookupOK(addrs ...string) func(context.Context, string) ([]string, error) {
	return func(context.Context, string) ([]string, error) { return addrs, nil }
}

func lookupFail() func(context.Context, string) ([]string, error) {
	return func(context.Context, string) ([]string, error) {
		return nil, errors.New("lookup: no such host")
	}
}

func TestCheckHostResolvesCauseMatrix(t *testing.T) {
	const tailscaleDNSHealth = "Tailscale failed to fetch the DNS configuration of your device: exit status 1"

	tests := []struct {
		name         string
		host         string
		probes       resolveProbes
		wantChecked  bool
		wantResolved bool
		wantHint     string
		forbidden    string
	}{
		{
			name:   "no host is not a failure",
			host:   "  ",
			probes: resolveProbes{LookupHost: lookupFail()},
			// Checked stays false so the caller can omit the line entirely
			// rather than render a machine as unresolvable on no evidence.
			wantChecked: false,
		},
		{
			name:         "name resolves",
			host:         "devbox.example-net.ts.net",
			probes:       resolveProbes{LookupHost: lookupOK("100.64.0.2")},
			wantChecked:  true,
			wantResolved: true,
		},
		{
			name:         "empty answer with no error is still a failure",
			host:         "devbox.example-net.ts.net",
			probes:       resolveProbes{LookupHost: lookupOK(), TailscaleHealth: health(nil, false)},
			wantChecked:  true,
			wantResolved: false,
			wantHint:     "MagicDNS name",
		},
		{
			name:         "ordinary name gets an ordinary DNS hint",
			host:         "buildbox.internal",
			probes:       resolveProbes{LookupHost: lookupFail()},
			wantChecked:  true,
			wantResolved: false,
			wantHint:     "getent hosts buildbox.internal",
			// An ordinary host has nothing to do with MagicDNS; saying so
			// would send the operator to the wrong subsystem.
			forbidden: "MagicDNS",
		},
		{
			name: "MagicDNS name quotes tailscale's own diagnosis",
			host: "mac-studio.tailnet.ts.net",
			probes: resolveProbes{
				LookupHost:      lookupFail(),
				TailscaleHealth: health([]string{tailscaleDNSHealth}, true),
			},
			wantChecked:  true,
			wantResolved: false,
			wantHint:     tailscaleDNSHealth,
		},
		{
			name: "MagicDNS name with unreadable health names the command that answers",
			host: "mac-studio.tailnet.ts.net",
			probes: resolveProbes{
				LookupHost:      lookupFail(),
				TailscaleHealth: health(nil, false),
			},
			wantChecked:  true,
			wantResolved: false,
			wantHint:     "run 'tailscale status'",
		},
		{
			name: "MagicDNS name ignores unrelated health messages",
			host: "mac-studio.tailnet.ts.net",
			probes: resolveProbes{
				LookupHost:      lookupFail(),
				TailscaleHealth: health([]string{"some peers are unreachable"}, true),
			},
			wantChecked:  true,
			wantResolved: false,
			// Quoting a non-DNS complaint under a resolution failure would
			// misdirect; fall back to the command that reports the real one.
			wantHint:  "run 'tailscale status'",
			forbidden: "some peers are unreachable",
		},
		{
			name: "trailing dot and casing still read as MagicDNS",
			host: "MAC-STUDIO.Tailnet.TS.NET.",
			probes: resolveProbes{
				LookupHost:      lookupFail(),
				TailscaleHealth: health([]string{tailscaleDNSHealth}, true),
			},
			wantChecked:  true,
			wantResolved: false,
			wantHint:     tailscaleDNSHealth,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := checkHostResolves(context.Background(), tt.host, tt.probes)
			if got.Checked != tt.wantChecked {
				t.Fatalf("Checked = %v, want %v", got.Checked, tt.wantChecked)
			}
			if got.Resolved != tt.wantResolved {
				t.Fatalf("Resolved = %v, want %v (hint %q)", got.Resolved, tt.wantResolved, got.Hint)
			}
			if tt.wantHint != "" && !strings.Contains(got.Hint, tt.wantHint) {
				t.Errorf("hint = %q, want it to contain %q", got.Hint, tt.wantHint)
			}
			if tt.forbidden != "" && strings.Contains(got.Hint, tt.forbidden) {
				t.Errorf("hint = %q, must not contain %q", got.Hint, tt.forbidden)
			}
			if got.Resolved && got.Hint != "" {
				t.Errorf("a resolved host carried a failure hint: %q", got.Hint)
			}
		})
	}
}

// An address literal is already an address. Sending it to the resolver risks a
// search-domain suffix answering for the wrong thing, so the lookup must not
// happen at all.
func TestCheckHostResolvesSkipsLookupForAddressLiterals(t *testing.T) {
	for _, host := range []string{"100.72.165.77", "fd7a:115c:a1e0::1"} {
		t.Run(host, func(t *testing.T) {
			called := false
			got := checkHostResolves(context.Background(), host, resolveProbes{
				LookupHost: func(context.Context, string) ([]string, error) {
					called = true
					return nil, errors.New("resolver must not be consulted")
				},
			})
			if called {
				t.Error("resolver was consulted for an address literal")
			}
			if !got.Checked || !got.Resolved {
				t.Fatalf("literal %s: Checked=%v Resolved=%v, want both true", host, got.Checked, got.Resolved)
			}
			if len(got.Addrs) != 1 || got.Addrs[0] != host {
				t.Errorf("Addrs = %v, want [%s]", got.Addrs, host)
			}
		})
	}
}

// A nil LookupHost seam must degrade to "not checked", never to a false
// accusation that the host is unresolvable.
func TestCheckHostResolvesWithoutSeam(t *testing.T) {
	got := checkHostResolves(context.Background(), "devbox.tailnet.ts.net", resolveProbes{})
	if got.Checked || got.Resolved || got.Hint != "" {
		t.Fatalf("got %+v, want the zero report", got)
	}
}

func TestFirstDNSHealthMessage(t *testing.T) {
	tests := []struct {
		name string
		msgs []string
		want string
	}{
		{name: "none", msgs: []string{"peers unreachable"}, want: ""},
		{name: "empty", msgs: nil, want: ""},
		{
			name: "picks the DNS one past unrelated entries",
			msgs: []string{"peers unreachable", "  ", "Tailscale failed to fetch the DNS configuration"},
			want: "Tailscale failed to fetch the DNS configuration",
		},
		{
			name: "matches case-insensitively",
			msgs: []string{"dns resolver is not responding"},
			want: "dns resolver is not responding",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := firstDNSHealthMessage(tt.msgs); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

// The regression this whole change exists for: an unresolvable host must not be
// reported as a possibly-missing or outdated remote camp.
func TestRenderMachineDiagnoseUnresolvableHostBlamesDNS(t *testing.T) {
	var buf bytes.Buffer
	err := renderMachineDiagnoseTable(&buf, []machineDiagnoseRow{{
		ID:             "mac-studio",
		Host:           "mac-studio.tailnet.ts.net",
		AuthMethod:     "tailscale-ssh",
		AuthLabel:      "Tailscale SSH (identity)",
		Socket:         "/tmp/mac-studio.sock",
		State:          "none",
		ResolveChecked: true,
		Resolves:       false,
		ResolveHint:    "MagicDNS is not resolving here; tailscale reports: DNS config unreadable",
	}})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	out := buf.String()
	for _, want := range []string{
		"RESOLVE",
		"✗",
		"MagicDNS is not resolving here",
		"not probed (the host does not resolve)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "camp missing / too old") {
		t.Errorf("unresolvable host still blamed on the remote camp binary:\n%s", out)
	}
}

func TestRenderMachineDiagnoseResolvedHostShowsAddresses(t *testing.T) {
	var buf bytes.Buffer
	err := renderMachineDiagnoseTable(&buf, []machineDiagnoseRow{{
		ID:             "devbox",
		Host:           "devbox.tailnet.ts.net",
		AuthMethod:     "ssh-agent",
		AuthLabel:      "OpenSSH (keys/agent)",
		Socket:         "/tmp/devbox.sock",
		State:          "none",
		ResolveChecked: true,
		Resolves:       true,
		ResolveAddrs:   []string{"100.64.0.2"},
		CampVersion:    "dev",
	}})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "RESOLVE") || !strings.Contains(out, "100.64.0.2") {
		t.Errorf("resolved host did not report its address:\n%s", out)
	}
	if strings.Contains(out, "not probed") {
		t.Errorf("resolved host was reported as unprobed:\n%s", out)
	}
}

// A machine with no host at all must not gain a RESOLVE line: there was nothing
// to look up, and an unconditional line would read as a verdict.
func TestRenderMachineDiagnoseOmitsResolveWhenUnchecked(t *testing.T) {
	var buf bytes.Buffer
	err := renderMachineDiagnoseTable(&buf, []machineDiagnoseRow{{
		ID:     "legacy",
		Socket: "/tmp/legacy.sock",
		State:  "none",
	}})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if strings.Contains(buf.String(), "RESOLVE") {
		t.Errorf("unchecked host still rendered a RESOLVE line:\n%s", buf.String())
	}
}

// A host that did not resolve but has a peer-table fallback reports the
// address a hop will actually dial — and probes the remote camp over it
// instead of declaring the machine unaddressable.
func TestRenderMachineDiagnoseFallbackDialShowsPeerAddress(t *testing.T) {
	var buf bytes.Buffer
	err := renderMachineDiagnoseTable(&buf, []machineDiagnoseRow{{
		ID:             "mac-studio",
		Host:           "mac-studio.tailnet.ts.net",
		AuthMethod:     "tailscale-ssh",
		AuthLabel:      "Tailscale SSH (identity)",
		Socket:         "/tmp/mac-studio.sock",
		State:          "none",
		ResolveChecked: true,
		Resolves:       false,
		ResolveHint:    "MagicDNS is not resolving here; tailscale reports: DNS config unreadable",
		DialHost:       "100.72.165.77",
		DialViaPeer:    true,
		CampVersion:    "v0.2.0",
	}})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	out := buf.String()
	for _, want := range []string{
		"RESOLVE",
		"100.72.165.77 (via tailnet peer table; MagicDNS is not resolving here",
		"v0.2.0",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
	for _, reject := range []string{"not probed (the host does not resolve)", "camp missing / too old"} {
		if strings.Contains(out, reject) {
			t.Errorf("fallback row still reads as undialable (%q):\n%s", reject, out)
		}
	}
}

// A machine ssh reached whose far side found no camp anywhere must read as
// exactly that — not "unreachable", which sends the operator at the network.
func TestRenderMachineDiagnoseCampMissingIsNotUnreachable(t *testing.T) {
	var buf bytes.Buffer
	err := renderMachineDiagnoseTable(&buf, []machineDiagnoseRow{{
		ID:             "archdtop",
		Host:           "archdtop.tailnet.ts.net",
		AuthMethod:     "tailscale-ssh",
		AuthLabel:      "Tailscale SSH (identity)",
		Socket:         "/tmp/archdtop.sock",
		State:          "none",
		ResolveChecked: true,
		Resolves:       true,
		ResolveAddrs:   []string{"100.74.252.71"},
		CampMissing:    true,
	}})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	out := buf.String()
	for _, want := range []string{
		"BINARY", "not found on the login-shell PATH", "~/go/bin", "CAMP_REMOTE_CAMP_PATH",
		"no camp binary to run",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
	for _, reject := range []string{"unreachable", "too old"} {
		if strings.Contains(out, reject) {
			t.Errorf("a missing binary was reported as %q:\n%s", reject, out)
		}
	}
}

// The archdtop shape after the fix: the login shell cannot see camp, the far
// side found it in ~/go/bin, and the version probe ran against it. Diagnose
// must show the path, say the login shell did not find it, and still report
// the version.
func TestRenderMachineDiagnoseReportsFallbackBinary(t *testing.T) {
	var buf bytes.Buffer
	err := renderMachineDiagnoseTable(&buf, []machineDiagnoseRow{{
		ID:             "archdtop",
		Host:           "archdtop.tailnet.ts.net",
		AuthMethod:     "tailscale-ssh",
		AuthLabel:      "Tailscale SSH (identity)",
		Socket:         "/tmp/archdtop.sock",
		State:          "none",
		ResolveChecked: true,
		Resolves:       true,
		ResolveAddrs:   []string{"100.74.252.71"},
		CampVersion:    "dev",
		CampCommit:     "a3a1cee2",
		CampPath:       "/home/lance/go/bin/camp",
		CampOnPath:     false,
	}})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"BINARY", "/home/lance/go/bin/camp", "not on the login-shell PATH", "a3a1cee2"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "unavailable") || strings.Contains(out, "not probed") {
		t.Errorf("a found-and-versioned camp was reported as unavailable:\n%s", out)
	}
}

func TestMachineDiagnoseBinaryLineShapes(t *testing.T) {
	cases := []struct {
		name string
		row  machineDiagnoseRow
		want []string
		none bool
	}{
		{name: "never probed", row: machineDiagnoseRow{}, none: true},
		{name: "on path", row: machineDiagnoseRow{CampPath: "/usr/local/bin/camp", CampOnPath: true},
			want: []string{"/usr/local/bin/camp", "on the login-shell PATH"}},
		{name: "fallback", row: machineDiagnoseRow{CampPath: "/home/u/.local/bin/camp"},
			want: []string{"/home/u/.local/bin/camp", "not on the login-shell PATH"}},
		{name: "override", row: machineDiagnoseRow{CampPath: "/opt/x/camp", CampOverride: true},
			want: []string{"/opt/x/camp", "CAMP_REMOTE_CAMP_PATH"}},
		{name: "missing", row: machineDiagnoseRow{CampMissing: true},
			want: []string{"✗", "~/.local/bin", "$GOBIN", "CAMP_REMOTE_CAMP_PATH"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := machineDiagnoseBinaryLine(tc.row)
			if tc.none {
				if got != "" {
					t.Errorf("expected no BINARY line, got %q", got)
				}
				return
			}
			for _, w := range tc.want {
				if !strings.Contains(got, w) {
					t.Errorf("BINARY line %q missing %q", got, w)
				}
			}
		})
	}
}
