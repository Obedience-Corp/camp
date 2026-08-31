package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/machines"
	"github.com/Obedience-Corp/camp/internal/remote"
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
		// Percent-decoded C0 controls must fail closed so encode/parse stay
		// symmetric and a handcrafted env cannot smuggle newlines into Host.
		{"newline in host via %0A", "v1;host=box%0Aevil;user=u", "control character"},
		{"CR in user via %0D", "v1;host=a;user=u%0D", "control character"},
		{"tab in campaign via %09", "v1;host=a;user=b;campaign=c%09d", "control character"},
		// Field caps match encodeHopOrigin: required refuse, optional drop
		// (optional over-cap is covered by TestParseHopOriginDropsOverlongOptional).
		{"over-long host", "v1;host=" + strings.Repeat("a", hopOriginMaxHost+1) + ";user=u", "too long"},
		{"over-long user", "v1;host=a;user=" + strings.Repeat("u", hopOriginMaxUser+1), "too long"},
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

func TestParseHopOriginDropsOverlongOptional(t *testing.T) {
	// Encode drops optional fields over cap rather than truncating; parse must
	// do the same so a handcrafted env cannot force an over-long campaign/id.
	payload := "v1;host=a;user=b;campaign=" + strings.Repeat("c", hopOriginMaxCampaign+1) +
		";id=" + strings.Repeat("i", hopOriginMaxID+1)
	got, err := ParseHopOrigin(payload)
	if err != nil {
		t.Fatalf("ParseHopOrigin: %v", err)
	}
	if got.Host != "a" || got.User != "b" {
		t.Errorf("required fields = %+v", got)
	}
	if got.Campaign != "" {
		t.Errorf("over-long campaign must be dropped, got %q", got.Campaign)
	}
	if got.ID != "" {
		t.Errorf("over-long id must be dropped, got %q", got.ID)
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
	if err := emitShellConnect(&buf, true, "/home/lance/campaigns/obey-campaign", remote.Direct(m), origin); err != nil {
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
	if !strings.HasPrefix(out, "ssh -t ") || !strings.HasSuffix(out, "\n") {
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
	if err := emitShellConnect(&buf, true, "/x", remote.Direct(m), hostile); err != nil {
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
	if err := emitShellConnect(&buf, true, "/srv/campaigns/obey", remote.Direct(m), ""); err != nil {
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

// newHopBackCmd returns a cobra command whose streams are captured, so a row of
// the decision table can assert exact stdout and stderr rather than "an error
// happened".
func newHopBackCmd() (*cobra.Command, *bytes.Buffer, *bytes.Buffer) {
	var out, errb bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&out)
	cmd.SetErr(&errb)
	return cmd, &out, &errb
}

// testOriginPayload names a host that is deliberately NOT this machine.
//
// It used to carry the author's own tailnet name, which meant every hop-back
// test silently exercised the self-origin path on that one machine and the
// intended remote path everywhere else. A fixture for "somewhere else" has to
// be somewhere else.
const testOriginPayload = "v1;host=devbox.example.ts.net;user=alex;campaign=obey-campaign;id=devbox"

func TestRunHopBackDecisionTable(t *testing.T) {
	tests := []struct {
		name         string
		payload      string
		setPayload   bool
		printOnly    bool
		jsonOut      bool
		shellConnect bool
		resolveErr   error
		wantErr      string
		wantStdout   string
	}{
		{
			name:    "no origin in this session",
			wantErr: "camp switch -: this session did not start from a camp hop (CAMP_HOP_ORIGIN is not set)",
		},
		{
			name:       "empty payload reads as absent",
			payload:    "   ",
			setPayload: true,
			wantErr:    "this session did not start from a camp hop",
		},
		{
			name:       "malformed payload names the reason",
			payload:    "v1;host=a;host=b;user=c",
			setPayload: true,
			wantErr:    `camp switch -: CAMP_HOP_ORIGIN is malformed (duplicate key "host"); hop back with 'camp switch <machine>:<camp>'`,
		},
		{
			name:       "unknown version is malformed, never half-parsed",
			payload:    "v2;host=a;user=b;campaign=c",
			setPayload: true,
			wantErr:    "malformed (unknown payload version)",
		},
		{
			name:       "origin campaign unknown",
			payload:    "v1;host=mac.ts.net;user=lance",
			setPayload: true,
			wantErr:    "camp switch -: origin camp unknown (the outbound hop did not start inside a camp); hop back with 'camp switch <machine>:<camp>'",
		},
		{
			name:       "--print is refused like any remote target",
			payload:    testOriginPayload,
			setPayload: true,
			printOnly:  true,
			wantErr:    "--print is local-only; use the csw shell wrapper to hop there",
		},
		{
			name:       "--json is refused like any remote target",
			payload:    testOriginPayload,
			setPayload: true,
			jsonOut:    true,
			wantErr:    "--json is not supported for a remote (machine:) switch; use the csw shell wrapper",
		},
		{
			name:       "bare invocation resolves then refuses to hop",
			payload:    testOriginPayload,
			setPayload: true,
			wantErr:    "; run via the csw shell wrapper to hop there",
		},
		{
			name:         "shell-connect emits the reverse hop line",
			payload:      testOriginPayload,
			setPayload:   true,
			shellConnect: true,
			wantStdout:   "ssh -t ",
		},
		{
			name:       "unreachable transient origin points at a probe, not diagnose",
			payload:    testOriginPayload,
			setPayload: true,
			resolveErr: errors.New("connection refused"),
			wantErr:    "the origin is not registered here, probe it with: ssh -o BatchMode=yes alex@devbox.example.ts.net true",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setPayload {
				t.Setenv(HopOriginEnvVar, tt.payload)
			} else {
				t.Setenv(HopOriginEnvVar, "")
			}
			// This table pins the DIAL-BACK path. Clear the ssh session markers
			// so it stays on that path even when the test process itself runs
			// inside an ssh login; the unwind path has its own tests
			// (hop_session_test.go).
			for _, v := range sshSessionEnvVars {
				t.Setenv(v, "")
			}
			// Point machines.yaml at an absent file so the transient path is
			// exercised deterministically, regardless of the developer's fleet.
			t.Setenv("CAMP_MACHINES_PATH", filepath.Join(t.TempDir(), "machines.yaml"))

			restore := resolveRemoteRoot
			resolveRemoteRoot = func(_ context.Context, m *machines.Machine, campaign string) (string, error) {
				if tt.resolveErr != nil {
					return "", tt.resolveErr
				}
				return "/home/lancerogers/Dev/AI/" + campaign, nil
			}
			t.Cleanup(func() { resolveRemoteRoot = restore })

			cmd, stdout, _ := newHopBackCmd()
			err := runHopBack(context.Background(), cmd, tt.printOnly, tt.shellConnect, tt.jsonOut)

			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("want error containing %q, got nil (stdout %q)", tt.wantErr, stdout.String())
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("error\n got %q\nwant it to contain %q", err.Error(), tt.wantErr)
				}
				if stdout.Len() != 0 {
					t.Errorf("a failing hop-back must write nothing to stdout, got %q", stdout.String())
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !strings.Contains(stdout.String(), tt.wantStdout) {
				t.Errorf("stdout\n got %q\nwant it to contain %q", stdout.String(), tt.wantStdout)
			}
		})
	}
}

func TestRunHopBackNeverWritesMachinesFile(t *testing.T) {
	// Registration-independence is the point of the gesture (D008): hopping back
	// must work from a machine that has never heard of the origin, and must not
	// quietly register it either.
	dir := t.TempDir()
	path := filepath.Join(dir, "machines.yaml")
	t.Setenv("CAMP_MACHINES_PATH", path)
	t.Setenv(HopOriginEnvVar, testOriginPayload)

	restore := resolveRemoteRoot
	resolveRemoteRoot = func(context.Context, *machines.Machine, string) (string, error) {
		return "/srv/obey-campaign", nil
	}
	t.Cleanup(func() { resolveRemoteRoot = restore })

	cmd, _, _ := newHopBackCmd()
	if err := runHopBack(context.Background(), cmd, false, true, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("hop-back created %s; it must never write the fleet file", path)
	}
}

func TestRunHopBackReverseLineCarriesItsOwnOrigin(t *testing.T) {
	// The toggle property: the session the reverse hop lands in must know the
	// machine it just left, or a second `csw -` would claim the session did not
	// start from a hop.
	t.Setenv("CAMP_MACHINES_PATH", filepath.Join(t.TempDir(), "machines.yaml"))
	t.Setenv(HopOriginEnvVar, testOriginPayload)

	restore := resolveRemoteRoot
	resolveRemoteRoot = func(context.Context, *machines.Machine, string) (string, error) {
		return "/srv/obey-campaign", nil
	}
	t.Cleanup(func() { resolveRemoteRoot = restore })

	cmd, stdout, _ := newHopBackCmd()
	if err := runHopBack(context.Background(), cmd, false, true, false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "export "+HopOriginEnvVar+"=") {
		t.Errorf("reverse hop line carries no origin, so the gesture would be one-way: %q", stdout.String())
	}
}

func TestTransientOriginID(t *testing.T) {
	tests := map[string]string{
		"mac-studio.tail37114b.ts.net": "mac-studio-origin",
		"Mac-Studio.local":             "mac-studio-origin",
		// An id that cannot be derived must still produce a non-empty socket
		// component: "" would collapse the ControlPath to ~/.obey/ssh-ctl/.sock
		// and share one socket across every degenerate origin.
		"100.72.165.77": "origin",
		"":              "origin",
	}
	for host, want := range tests {
		if got := transientOriginID(host); got != want {
			t.Errorf("transientOriginID(%q) = %q, want %q", host, got, want)
		}
	}
}

func TestOriginTargetPrefersRegisteredEntry(t *testing.T) {
	// A registered entry carries the operator's own identity_file and
	// auth_method, which is better information than the payload has.
	dir := t.TempDir()
	path := filepath.Join(dir, "machines.yaml")
	if err := os.WriteFile(path, []byte(`version: 1
machines:
    - id: studio
      host: DEVBOX.example.ts.net.
      auth_method: tailscale-ssh
      ssh_user: someoneelse
      identity_file: /keys/id_ed25519
`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CAMP_MACHINES_PATH", path)

	origin, err := ParseHopOrigin(testOriginPayload)
	if err != nil {
		t.Fatal(err)
	}
	m, registered := originTarget(origin)
	if !registered {
		t.Fatal("host match should have been found despite case and trailing-dot differences")
	}
	if m.ID != "studio" {
		t.Errorf("id = %q, want the registered id", m.ID)
	}
	if m.IdentityFile != "/keys/id_ed25519" {
		t.Errorf("registered identity_file was dropped: %+v", m)
	}
}

func TestOriginTargetTransientLeavesAuthMethodEmpty(t *testing.T) {
	// Guessing an auth mode would put a wrong label in front of a hop failure
	// (FormatHopFailure prefixes by AuthMethod), and the argv is identical
	// either way, so empty is the honest value.
	t.Setenv("CAMP_MACHINES_PATH", filepath.Join(t.TempDir(), "machines.yaml"))
	origin, err := ParseHopOrigin(testOriginPayload)
	if err != nil {
		t.Fatal(err)
	}
	m, registered := originTarget(origin)
	if registered {
		t.Fatal("no machines file, so the origin cannot be registered")
	}
	if m.AuthMethod != "" {
		t.Errorf("AuthMethod = %q, want empty", m.AuthMethod)
	}
	if m.Host != "devbox.example.ts.net" || m.SSHUser != "alex" {
		t.Errorf("transient machine = %+v", m)
	}
}

func TestIsSelfMachine(t *testing.T) {
	// This machine's own derived id, computed the same way the payload builder
	// computes it, so the test asserts the shared derivation rather than a
	// hardcoded hostname.
	host, err := detectReachableName(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	self := suggestedMachineID(host)
	if self == "" {
		t.Skip("this machine's hostname does not derive a valid id")
	}

	t.Run("self id matches", func(t *testing.T) {
		t.Setenv("CAMP_MACHINES_PATH", filepath.Join(t.TempDir(), "machines.yaml"))
		if !isSelfMachine(context.Background(), self) {
			t.Errorf("isSelfMachine(%q) = false, want true", self)
		}
	})

	t.Run("another machine does not match", func(t *testing.T) {
		t.Setenv("CAMP_MACHINES_PATH", filepath.Join(t.TempDir(), "machines.yaml"))
		if isSelfMachine(context.Background(), "definitely-not-this-machine") {
			t.Error("a foreign id must not read as self")
		}
	})

	t.Run("empty id is not self", func(t *testing.T) {
		if isSelfMachine(context.Background(), "") {
			t.Error("empty id must not read as self")
		}
	})

	t.Run("a registered entry pointing elsewhere wins over self detection", func(t *testing.T) {
		// Honoring the operator's explicit file beats second-guessing it, so a
		// row that maps this id to another host still hops.
		dir := t.TempDir()
		path := filepath.Join(dir, "machines.yaml")
		if err := os.WriteFile(path, []byte("version: 1\nmachines:\n    - id: "+self+
			"\n      host: somewhere.else\n      auth_method: ssh-agent\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		t.Setenv("CAMP_MACHINES_PATH", path)
		if isSelfMachine(context.Background(), self) {
			t.Error("a registered entry must take precedence over self detection")
		}
	})

	t.Run("a registered entry pointing at this machine still resolves locally", func(t *testing.T) {
		// The case a shared fleet file produces: the same machines.yaml on every
		// node, listing this one under its real id. Treating that as "registered,
		// therefore remote" sends the hop to ourselves, which is the self-ssh
		// this whole check exists to prevent.
		host, err := detectReachableName(context.Background(), runTailscaleStatusForSelf)
		if err != nil || host == "" {
			t.Skip("no reachable name for this machine")
		}
		path := filepath.Join(t.TempDir(), "machines.yaml")
		if err := os.WriteFile(path, []byte("version: 1\nmachines:\n    - id: "+self+
			"\n      host: "+host+"\n      auth_method: ssh-agent\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		t.Setenv("CAMP_MACHINES_PATH", path)
		if !isSelfMachine(context.Background(), self) {
			t.Error("a registered row whose host is this machine must resolve locally, not ssh to self")
		}
	})
}

// A payload naming this machine must not emit an ssh to ourselves.
func TestRunSwitchHopBackRefusesASelfOrigin(t *testing.T) {
	t.Setenv("CAMP_MACHINES_PATH", filepath.Join(t.TempDir(), "machines.yaml"))
	host, err := detectReachableName(context.Background(), runTailscaleStatusForSelf)
	if err != nil || host == "" {
		t.Skip("no reachable name for this machine")
	}
	t.Setenv(HopOriginEnvVar, "v1;host="+host+";user=someone;campaign=demo;id=whatever")

	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})

	err = runHopBack(context.Background(), cmd, false, true, false)
	if err == nil {
		t.Fatal("a self origin must not produce a hop")
	}
	if !strings.Contains(err.Error(), "already here") {
		t.Errorf("error should say the user is already here, got: %v", err)
	}
}

func TestRunSwitchHopBackWorksWithNoLocalRegistry(t *testing.T) {
	// Hop-back resolves the campaign on the ORIGIN machine, so this machine
	// having no registry is not a precondition. Before the check was hoisted,
	// an empty registry produced "no campaigns registered (use 'camp init'…)",
	// pointing the operator at an unrelated command.
	t.Setenv("CAMP_REGISTRY_PATH", filepath.Join(t.TempDir(), "registry.json"))
	t.Setenv("CAMP_MACHINES_PATH", filepath.Join(t.TempDir(), "machines.yaml"))
	t.Setenv(HopOriginEnvVar, "")

	cmd, _, _ := newHopBackCmd()
	cmd.SetArgs([]string{"-"})
	cmd.Flags().Bool("print", false, "")
	cmd.Flags().Bool("json", false, "")
	cmd.Flags().Bool("shell-connect", false, "")
	cmd.Flags().String("org", "", "")
	cmd.Flags().String("status", "", "")
	cmd.Flags().Bool("all", false, "")

	err := runSwitch(cmd, []string{"-"})
	if err == nil {
		t.Fatal("want the hop-back error, got nil")
	}
	if strings.Contains(err.Error(), "no campaigns registered") {
		t.Errorf("hop-back was gated on the local registry: %v", err)
	}
	if !strings.Contains(err.Error(), "did not start from a camp hop") {
		t.Errorf("error = %v, want the hop-back precondition message", err)
	}
}
