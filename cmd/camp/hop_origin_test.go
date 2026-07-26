package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/Obedience-Corp/camp/internal/machines"
)

func TestEncodeHopOrigin(t *testing.T) {
	tests := []struct {
		name    string
		in      HopOrigin
		want    string
		wantErr bool
	}{
		{
			name: "all fields",
			in:   HopOrigin{Host: "mac-studio.tail37114b.ts.net", User: "lancerogers", Campaign: "obey-campaign", ID: "mac-studio"},
			want: "v1;host=mac-studio.tail37114b.ts.net;user=lancerogers;campaign=obey-campaign;id=mac-studio",
		},
		{
			name: "campaign omitted when empty",
			in:   HopOrigin{Host: "box.local", User: "lance", ID: "box"},
			want: "v1;host=box.local;user=lance;id=box",
		},
		{
			name: "id omitted when empty",
			in:   HopOrigin{Host: "box.local", User: "lance", Campaign: "c"},
			want: "v1;host=box.local;user=lance;campaign=c",
		},
		{
			// The separator characters are the whole reason encoding exists: a
			// campaign named "x;host=evil" must not inject a second host pair.
			name: "separators in a value are encoded",
			in:   HopOrigin{Host: "box.local", User: "lance", Campaign: "x;host=evil.example.com"},
			want: "v1;host=box.local;user=lance;campaign=x%3Bhost%3Devil.example.com",
		},
		{
			name: "quote and space encoded",
			in:   HopOrigin{Host: "box.local", User: "lance", Campaign: "x'; rm -rf ~;'"},
			want: "v1;host=box.local;user=lance;campaign=x%27%3B%20rm%20-rf%20~%3B%27",
		},
		{
			name: "percent itself is encoded so decoding is unambiguous",
			in:   HopOrigin{Host: "box.local", User: "lance", Campaign: "100%25"},
			want: "v1;host=box.local;user=lance;campaign=100%2525",
		},
		{
			name: "newline encoded, never raw",
			in:   HopOrigin{Host: "box.local", User: "lance", Campaign: "a\nb"},
			want: "v1;host=box.local;user=lance;campaign=a%0Ab",
		},
		{
			name:    "empty host is refused",
			in:      HopOrigin{User: "lance"},
			wantErr: true,
		},
		{
			name:    "empty user is refused",
			in:      HopOrigin{Host: "box.local"},
			wantErr: true,
		},
		{
			name:    "over-long host is refused, never truncated",
			in:      HopOrigin{Host: strings.Repeat("a", hopOriginMaxHost+1), User: "lance"},
			wantErr: true,
		},
		{
			// An over-cap optional field is dropped, not truncated: a truncated
			// campaign name is a different campaign.
			name: "over-long campaign is dropped, payload survives",
			in:   HopOrigin{Host: "box.local", User: "lance", Campaign: strings.Repeat("c", hopOriginMaxCampaign+1), ID: "box"},
			want: "v1;host=box.local;user=lance;id=box",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := encodeHopOrigin(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("encodeHopOrigin(%+v) = %q, want error", tt.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("encodeHopOrigin(%+v) error: %v", tt.in, err)
			}
			if got != tt.want {
				t.Errorf("encodeHopOrigin(%+v)\n got %q\nwant %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestHopOriginRoundTrip(t *testing.T) {
	// Values chosen to exercise every encoded class at once.
	in := HopOrigin{
		Host:     "mac-studio.tail37114b.ts.net",
		User:     "lance rogers",
		Campaign: "x'; rm -rf ~;' ;host=evil=1",
		ID:       "mac-studio",
	}
	payload, err := encodeHopOrigin(in)
	if err != nil {
		t.Fatal(err)
	}
	got, err := ParseHopOrigin(payload)
	if err != nil {
		t.Fatalf("ParseHopOrigin(%q): %v", payload, err)
	}
	if got != in {
		t.Errorf("round trip\n got %+v\nwant %+v", got, in)
	}
}

func TestParseHopOriginErrors(t *testing.T) {
	tests := []struct {
		name    string
		payload string
		wantErr string
	}{
		{"empty", "", "empty payload"},
		{"unknown version", "v2;host=a;user=b", "unknown payload version"},
		{"no version", "host=a;user=b", "unknown payload version"},
		{"token without =", "v1;host=a;user=b;bare", "token without '='"},
		{"duplicate key", "v1;host=a;host=b;user=c", `duplicate key "host"`},
		{"bad percent escape", "v1;host=a%zz;user=b", "invalid percent-encoding"},
		{"truncated percent escape", "v1;host=a%4;user=b", "invalid percent-encoding"},
		{"missing host", "v1;user=b", "missing host"},
		{"missing user", "v1;host=a", "missing user"},
		{"over size bound", "v1;host=a;user=b;campaign=" + strings.Repeat("x", hopOriginMaxTotal), "exceeds 1024 bytes"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseHopOrigin(tt.payload)
			if err == nil {
				t.Fatalf("ParseHopOrigin(%q) = nil error, want %q", tt.payload, tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("ParseHopOrigin(%q) error = %v, want it to contain %q", tt.payload, err, tt.wantErr)
			}
		})
	}
}

func TestParseHopOriginIgnoresUnknownKeys(t *testing.T) {
	// Additive optional fields must not break an older consumer; only a change
	// in an existing field's meaning bumps the version.
	got, err := ParseHopOrigin("v1;host=a;user=b;future=whatever;id=c")
	if err != nil {
		t.Fatal(err)
	}
	if got.Host != "a" || got.User != "b" || got.ID != "c" {
		t.Errorf("unknown key changed the parse: %+v", got)
	}
}

// tailscaleSelfFixture is a minimal `tailscale status --json` document. The
// trailing dot on DNSName is real MagicDNS output and is why normalization is
// mandatory rather than cosmetic.
const tailscaleSelfFixture = `{
  "BackendState": "Running",
  "Self": {"HostName": "Mac Studio", "DNSName": "mac-studio.tail37114b.ts.net.", "OS": "macOS"}
}`

func TestDetectReachableNamePrefersTailscale(t *testing.T) {
	got, err := detectReachableName(context.Background(), func(context.Context) ([]byte, error) {
		return []byte(tailscaleSelfFixture), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != "mac-studio.tail37114b.ts.net" {
		t.Errorf("detectReachableName = %q, want the trailing dot trimmed", got)
	}
}

func TestDetectReachableNameFallsBackToHostname(t *testing.T) {
	cases := map[string]reachableNameFunc{
		"no tailscale binary": nil,
		"tailscale errors":    func(context.Context) ([]byte, error) { return nil, context.DeadlineExceeded },
		"backend not running": func(context.Context) ([]byte, error) {
			return []byte(`{"BackendState":"Stopped","Self":{"DNSName":"x.ts.net."}}`), nil
		},
		"self has no DNSName": func(context.Context) ([]byte, error) {
			return []byte(`{"BackendState":"Running","Self":{"HostName":"box"}}`), nil
		},
		"unparseable output": func(context.Context) ([]byte, error) { return []byte("not json"), nil },
	}
	for name, run := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := detectReachableName(context.Background(), run)
			if err != nil {
				t.Fatalf("fallback must not error: %v", err)
			}
			if got == "" {
				t.Error("fallback produced no name")
			}
			if got == "x.ts.net" {
				t.Error("used a non-Running backend's DNSName")
			}
		})
	}
}

func TestDetectReachableNameHonorsCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := detectReachableName(ctx, nil); err == nil {
		t.Error("cancelled context must not be ignored")
	}
}

func TestSuggestedMachineID(t *testing.T) {
	tests := map[string]string{
		"mac-studio.tail37114b.ts.net": "mac-studio",
		"Mac-Studio.local":             "mac-studio",
		"archdtop.tail37114b.ts.net":   "archdtop",
		// A leading digit fails validateMachineID, so no id is suggested rather
		// than an invalid one being written into machines.yaml by adopt.
		"100.72.165.77": "",
		"":              "",
	}
	for host, want := range tests {
		if got := suggestedMachineID(host); got != want {
			t.Errorf("suggestedMachineID(%q) = %q, want %q", host, got, want)
		}
	}
}

func TestBuildHopOriginUsesDetectedHost(t *testing.T) {
	got, err := buildHopOrigin(context.Background(), "obey-campaign", func(context.Context) ([]byte, error) {
		return []byte(tailscaleSelfFixture), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseHopOrigin(got)
	if err != nil {
		t.Fatalf("built payload does not parse: %v (%q)", err, got)
	}
	if parsed.Host != "mac-studio.tail37114b.ts.net" {
		t.Errorf("host = %q", parsed.Host)
	}
	if parsed.Campaign != "obey-campaign" {
		t.Errorf("campaign = %q", parsed.Campaign)
	}
	if parsed.ID != "mac-studio" {
		t.Errorf("id = %q", parsed.ID)
	}
	if parsed.User == "" {
		t.Error("user must always be present")
	}
}

func TestBuildHopOriginOutsideCampaign(t *testing.T) {
	// Hopping from outside a campaign is legal; the payload simply carries no
	// campaign, and `camp switch -` reports that rather than guessing.
	got, err := buildHopOrigin(context.Background(), "", func(context.Context) ([]byte, error) {
		return []byte(tailscaleSelfFixture), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "campaign=") {
		t.Errorf("payload must omit an empty campaign: %q", got)
	}
	if _, err := ParseHopOrigin(got); err != nil {
		t.Errorf("payload without campaign must still parse: %v", err)
	}
}

func TestEmitShellConnectEmbedsOrigin(t *testing.T) {
	var buf bytes.Buffer
	m := &machines.Machine{ID: "archdtop", Host: "archdtop.tail37114b.ts.net", SSHUser: "lance", AuthMethod: machines.AuthTailscaleSSH}
	origin := "v1;host=mac-studio.tail37114b.ts.net;user=lancerogers;campaign=obey-campaign;id=mac-studio"
	if err := emitShellConnect(&buf, true, "/home/lance/campaigns/obey-campaign", m, origin); err != nil {
		t.Fatal(err)
	}
	out := buf.String()

	want := `'export CAMP_HOP_ORIGIN='\''` + origin + `'\'' && cd '\''/home/lance/campaigns/obey-campaign'\'' && exec $SHELL -l'`
	if !strings.Contains(out, want) {
		t.Errorf("emitted remote command\n got %q\nwant it to contain %q", out, want)
	}
	// The export must precede the cd: it has to be set before exec replaces
	// this process image with the login shell.
	if strings.Index(out, "export CAMP_HOP_ORIGIN") > strings.Index(out, "cd ") {
		t.Errorf("export must come before cd: %q", out)
	}
	if !strings.HasPrefix(out, "exec ssh -t ") || !strings.HasSuffix(out, "\n") {
		t.Errorf("hop line shape changed: %q", out)
	}
	if strings.Count(out, "\n") != 1 {
		t.Errorf("shell-connect must emit exactly one line: %q", out)
	}
}

func TestEmitShellConnectHostileOriginIsQuoted(t *testing.T) {
	var buf bytes.Buffer
	m := &machines.Machine{ID: "box", Host: "box.local", AuthMethod: machines.AuthSSHAgent}
	// A payload that somehow carried a raw quote (it cannot, encoding forbids
	// it) must still be inert after ShellQuote. Belt and suspenders: this pins
	// the shell layer independently of the encoding layer.
	hostile := `v1;host=box.local;user=lance;campaign=x'; rm -rf ~;'`
	if err := emitShellConnect(&buf, true, "/x", m, hostile); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if strings.Contains(out, `campaign=x'; rm -rf ~;'`) {
		t.Errorf("hostile payload leaked unquoted: %q", out)
	}
	if !strings.Contains(out, `'\''`) {
		t.Errorf("expected escaped quotes in the emitted line: %q", out)
	}
}

func TestEmitShellConnectWithoutOriginIsUnchanged(t *testing.T) {
	// Regression pin: an empty origin must produce byte-identical output to the
	// pre-feature hop line, so a machine with no reachable name hops exactly as
	// it does today.
	var buf bytes.Buffer
	m := &machines.Machine{ID: "devbox", Host: "devbox.ts.net", SSHUser: "lance", AuthMethod: machines.AuthSSHAgent}
	if err := emitShellConnect(&buf, true, "/srv/campaigns/obey", m, ""); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if strings.Contains(out, "CAMP_HOP_ORIGIN") {
		t.Errorf("empty origin must emit no export: %q", out)
	}
	if !strings.Contains(out, `'cd '\''/srv/campaigns/obey'\'' && exec $SHELL -l'`) {
		t.Errorf("remote command shape changed without an origin: %q", out)
	}
}
