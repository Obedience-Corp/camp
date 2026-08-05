package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/machines"
)

// adoptPayload names a machine that is deliberately NOT the machine running the
// tests: a payload whose host is the local machine trips the self-guard first,
// which would make every other assertion here measure the wrong branch.
const adoptPayload = "v1;host=origin-box.tail37114b.ts.net;user=lancerogers;campaign=obey-campaign;id=origin-box"

func newAdoptCmd(t *testing.T) (*cobra.Command, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	var out, errb bytes.Buffer
	cmd := &cobra.Command{}
	// Production always runs under ExecuteContext; a bare command has a nil
	// context, and every I/O path here takes ctx as its first parameter.
	cmd.SetContext(context.Background())
	cmd.SetOut(&out)
	cmd.SetErr(&errb)
	cmd.Flags().Bool("json", false, "")
	return cmd, &out, &errb
}

// isolateFleet points both fleet files at a scratch directory so a test can
// never read or write the developer's real ~/.obey.
func isolateFleet(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("CAMP_MACHINES_PATH", filepath.Join(dir, "machines.yaml"))
	return dir
}

// T4 from registration-spec.md section 8.
func TestAdoptPayloadAbsent(t *testing.T) {
	isolateFleet(t)
	t.Setenv(HopOriginEnvVar, "")
	cmd, out, _ := newAdoptCmd(t)

	err := runMachineAdopt(cmd, nil)
	if err == nil {
		t.Fatal("want an error when no origin is present")
	}
	want := "camp machine adopt: no origin in this session (CAMP_HOP_ORIGIN is not set); " +
		"this command adopts the machine you hopped here from"
	if err.Error() != want {
		t.Errorf("error\n got %q\nwant %q", err.Error(), want)
	}
	if out.Len() != 0 {
		t.Errorf("stdout must stay empty, got %q", out.String())
	}
	if _, statErr := os.Stat(machines.MachinesPath()); !os.IsNotExist(statErr) {
		t.Error("a failed adopt must not create the fleet file")
	}
}

func TestAdoptPayloadMalformed(t *testing.T) {
	isolateFleet(t)
	t.Setenv(HopOriginEnvVar, "v1;host=a;host=b;user=c")
	cmd, out, _ := newAdoptCmd(t)

	err := runMachineAdopt(cmd, nil)
	if err == nil {
		t.Fatal("want an error for a malformed payload")
	}
	if !strings.Contains(err.Error(), `is malformed (duplicate key "host"); nothing to adopt`) {
		t.Errorf("error = %q, want it to name the parse reason", err.Error())
	}
	if out.Len() != 0 {
		t.Errorf("stdout must stay empty, got %q", out.String())
	}
}

// T5: non-TTY is a hard error that writes nothing. The test binary has no
// terminal, so this is the default path here and is what makes the other
// write-nothing assertions meaningful.
func TestAdoptNonTTYWritesNothing(t *testing.T) {
	isolateFleet(t)
	t.Setenv(HopOriginEnvVar, adoptPayload)
	cmd, _, _ := newAdoptCmd(t)

	err := runMachineAdopt(cmd, nil)
	if err == nil {
		t.Fatal("want an error without a terminal")
	}
	want := "camp machine adopt requires an interactive terminal: registering a machine is " +
		"an explicit consent step and cannot be automated"
	if err.Error() != want {
		t.Errorf("error\n got %q\nwant %q", err.Error(), want)
	}
	if _, statErr := os.Stat(machines.MachinesPath()); !os.IsNotExist(statErr) {
		t.Error("non-TTY adopt must not write the fleet file")
	}
}

func TestAdoptJSONIsRefused(t *testing.T) {
	isolateFleet(t)
	t.Setenv(HopOriginEnvVar, adoptPayload)
	cmd, _, _ := newAdoptCmd(t)
	if err := cmd.Flags().Set("json", "true"); err != nil {
		t.Fatal(err)
	}
	err := runMachineAdopt(cmd, nil)
	if err == nil || !strings.Contains(err.Error(), "does not support --json") {
		t.Errorf("error = %v, want the --json refusal", err)
	}
}

func TestAdoptAlreadyRegisteredUnderAnotherID(t *testing.T) {
	// Two rows for one host would mean two ControlMaster sockets to the same
	// machine and an ambiguous `camp machine list`.
	dir := isolateFleet(t)
	if err := os.WriteFile(filepath.Join(dir, "machines.yaml"), []byte(`version: 1
machines:
    - id: studio
      host: ORIGIN-BOX.tail37114b.ts.net.
      auth_method: ssh-agent
`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(HopOriginEnvVar, adoptPayload)
	cmd, out, _ := newAdoptCmd(t)

	if err := runMachineAdopt(cmd, nil); err != nil {
		t.Fatalf("an already-registered host is not an error: %v", err)
	}
	if !strings.Contains(out.String(), `is already in your fleet as "studio"; nothing to adopt`) {
		t.Errorf("stdout = %q", out.String())
	}
}

func TestAdoptRefusesSelfHost(t *testing.T) {
	// A row pointing at this machine would shadow self-reference resolution,
	// which is why the guard lives at write time.
	isolateFleet(t)
	host, err := detectReachableName(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := encodeHopOrigin(HopOrigin{Host: host, User: "someone", Campaign: "c"})
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(HopOriginEnvVar, payload)
	cmd, _, _ := newAdoptCmd(t)

	err = runMachineAdopt(cmd, nil)
	if err == nil || !strings.Contains(err.Error(), "this origin is this machine") {
		t.Errorf("error = %v, want the self-host refusal", err)
	}
}

func TestAdoptSelfOriginDetectionErrorFailsClosed(t *testing.T) {
	// Detection errors must refuse adopt rather than skip the self-guard and
	// risk writing a row that points at this machine.
	isolateFleet(t)
	t.Setenv(HopOriginEnvVar, adoptPayload)
	cmd, out, _ := newAdoptCmd(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cmd.SetContext(ctx)

	err := runMachineAdopt(cmd, nil)
	if err == nil {
		t.Fatal("want fail-closed error when self-origin detection fails")
	}
	if !strings.Contains(err.Error(), "cannot determine whether this origin is this machine") {
		t.Errorf("error = %v, want the fail-closed wrap", err)
	}
	if out.Len() != 0 {
		t.Errorf("stdout must stay empty, got %q", out.String())
	}
	if _, statErr := os.Stat(machines.MachinesPath()); !os.IsNotExist(statErr) {
		t.Error("fail-closed adopt must not write the fleet file")
	}
}

func TestIsSelfOriginDualNameMatrix(t *testing.T) {
	// The dual-name guard exists because detectReachableName prefers tailnet
	// when available: a payload built while tailscale was down carries the bare
	// hostname, and a single comparison against today's tailnet name would miss
	// it. Both names are this machine, so both must count — and this is
	// injectable so the matrix does not depend on a live tailnet or hostname.
	const (
		tailnet = "mac-studio.tail37114b.ts.net"
		bare    = "Mac-Studio.local"
		other   = "archdtop.tail37114b.ts.net"
	)
	prevTail, prevBare := adoptDetectTailnet, adoptDetectBare
	t.Cleanup(func() {
		adoptDetectTailnet, adoptDetectBare = prevTail, prevBare
	})
	adoptDetectTailnet = func(context.Context) (string, error) { return tailnet, nil }
	adoptDetectBare = func(context.Context) (string, error) { return bare, nil }

	tests := []struct {
		name    string
		host    string
		want    bool
		wantErr string
		// override detectors for error rows
		tailnetErr, bareErr error
	}{
		{name: "matches tailnet only", host: tailnet, want: true},
		{name: "matches bare only", host: bare, want: true},
		{name: "matches neither", host: other, want: false},
		{name: "empty host", host: "", want: false},
		{
			name:       "tailnet probe error fails closed",
			host:       other,
			wantErr:    "tailnet down",
			tailnetErr: errors.New("tailnet down"),
		},
		{
			name:    "bare probe error after tailnet miss fails closed",
			host:    other,
			wantErr: "hostname failed",
			bareErr: errors.New("hostname failed"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			adoptDetectTailnet = func(context.Context) (string, error) {
				if tt.tailnetErr != nil {
					return "", tt.tailnetErr
				}
				return tailnet, nil
			}
			adoptDetectBare = func(context.Context) (string, error) {
				if tt.bareErr != nil {
					return "", tt.bareErr
				}
				return bare, nil
			}
			got, err := isSelfOrigin(context.Background(), HopOrigin{Host: tt.host, User: "u"})
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("isSelfOrigin: %v", err)
			}
			if got != tt.want {
				t.Errorf("isSelfOrigin(%q) = %v, want %v", tt.host, got, tt.want)
			}
		})
	}
}

func TestAdoptCancelDoesNotWriteDecline(t *testing.T) {
	// Esc/abort is not a deliberate No — decline memory must stay empty.
	isolateFleet(t)
	t.Setenv(HopOriginEnvVar, adoptPayload)
	cmd, out, _ := newAdoptCmd(t)

	prevTerm, prevConfirm := adoptIsTerminal, adoptConfirm
	t.Cleanup(func() {
		adoptIsTerminal, adoptConfirm = prevTerm, prevConfirm
	})
	adoptIsTerminal = func() bool { return true }
	adoptConfirm = func(context.Context, string) (bool, bool, error) {
		return false, true, nil // canceled
	}

	if err := runMachineAdopt(cmd, nil); err != nil {
		t.Fatalf("cancel is not an error: %v", err)
	}
	if !strings.Contains(out.String(), "Not adopted.") {
		t.Errorf("stdout = %q, want Not adopted", out.String())
	}
	if _, err := os.Stat(machines.DeclinedPath()); !os.IsNotExist(err) {
		t.Error("cancel must not create declined_origins.yaml")
	}
	if _, err := os.Stat(machines.MachinesPath()); !os.IsNotExist(err) {
		t.Error("cancel must not write machines.yaml")
	}
}

func TestAdoptExplicitNoWritesDecline(t *testing.T) {
	isolateFleet(t)
	t.Setenv(HopOriginEnvVar, adoptPayload)
	cmd, out, _ := newAdoptCmd(t)

	prevTerm, prevConfirm := adoptIsTerminal, adoptConfirm
	t.Cleanup(func() {
		adoptIsTerminal, adoptConfirm = prevTerm, prevConfirm
	})
	adoptIsTerminal = func() bool { return true }
	adoptConfirm = func(context.Context, string) (bool, bool, error) {
		return false, false, nil // explicit No
	}

	if err := runMachineAdopt(cmd, nil); err != nil {
		t.Fatalf("explicit No is not an error: %v", err)
	}
	if !strings.Contains(out.String(), "if you change your mind") {
		t.Errorf("stdout = %q, want the durable-decline message", out.String())
	}
	f, err := machines.LoadDeclined()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := f.IsDeclined("origin-box.tail37114b.ts.net"); !ok {
		t.Error("explicit No must record decline memory")
	}
}

func TestAdoptEntryMapping(t *testing.T) {
	origin, err := ParseHopOrigin(adoptPayload)
	if err != nil {
		t.Fatal(err)
	}
	entry, err := adoptEntry(&machines.File{Version: 1}, origin)
	if err != nil {
		t.Fatal(err)
	}
	if entry.ID != "origin-box" || entry.Host != "origin-box.tail37114b.ts.net" || entry.SSHUser != "lancerogers" {
		t.Errorf("entry = %+v", entry)
	}
	if entry.AuthMethod != machines.AuthSSHAgent {
		t.Errorf("auth_method = %q, want the ssh-agent default (the payload carries none)", entry.AuthMethod)
	}
	if !strings.HasPrefix(entry.Label, "adopted from hop ") {
		t.Errorf("label = %q, want provenance", entry.Label)
	}
	if entry.IdentityFile != "" {
		t.Errorf("identity_file must never be guessed, got %q", entry.IdentityFile)
	}
}

func TestAdoptEntryIDCollisionIsRefused(t *testing.T) {
	// U8: never auto-suffix. Two ids differing by a digit are invisible in
	// `camp machine list` and the next hop becomes a coin flip.
	mf := &machines.File{Version: 1, Machines: []machines.Machine{
		{ID: "origin-box", Host: "something.else", AuthMethod: machines.AuthSSHAgent},
	}}
	origin, err := ParseHopOrigin(adoptPayload)
	if err != nil {
		t.Fatal(err)
	}
	_, err = adoptEntry(mf, origin)
	if err == nil || !strings.Contains(err.Error(), "is already used by a different host") {
		t.Errorf("error = %v, want the collision refusal", err)
	}
	if err != nil && !strings.Contains(err.Error(), "camp machine add") {
		t.Errorf("collision error must name the explicit escape hatch: %v", err)
	}
}

func TestAdoptEntryUndeliverableID(t *testing.T) {
	origin := HopOrigin{Host: "100.72.165.77", User: "lance"}
	_, err := adoptEntry(&machines.File{Version: 1}, origin)
	if err == nil || !strings.Contains(err.Error(), "cannot derive a valid machine id") {
		t.Errorf("error = %v, want the derivation failure to name 'camp machine add'", err)
	}
}

func TestAdoptPreviewShowsExactYAML(t *testing.T) {
	origin, err := ParseHopOrigin(adoptPayload)
	if err != nil {
		t.Fatal(err)
	}
	entry, err := adoptEntry(&machines.File{Version: 1}, origin)
	if err != nil {
		t.Fatal(err)
	}
	preview := adoptPreview(origin, entry)
	for _, want := range []string{
		"    - id: origin-box",
		"      host: origin-box.tail37114b.ts.net",
		"      auth_method: ssh-agent",
		"      ssh_user: lancerogers",
		"auth_method is a default",
		"It does not make origin-box reachable",
		"camp machine\ndiagnose origin-box",
	} {
		if !strings.Contains(preview, want) {
			t.Errorf("preview missing %q:\n%s", want, preview)
		}
	}
}

func TestDeclineMemoryRoundTrip(t *testing.T) {
	isolateFleet(t)

	f, err := machines.LoadDeclined()
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Declined) != 0 {
		t.Errorf("absent file must load empty, got %+v", f.Declined)
	}

	f.Decline("origin-box", "ORIGIN-Box.tail37114b.ts.net.", timeFixture())
	if err := f.Save(); err != nil {
		t.Fatal(err)
	}

	reloaded, err := machines.LoadDeclined()
	if err != nil {
		t.Fatal(err)
	}
	// Matching normalizes case and the trailing dot: the same machine arriving
	// under a different spelling is still declined.
	if _, ok := reloaded.IsDeclined("origin-box.tail37114b.ts.net"); !ok {
		t.Errorf("decline not found after reload: %+v", reloaded.Declined)
	}
	if _, ok := reloaded.IsDeclined("other.host"); ok {
		t.Error("an unrelated host must not read as declined")
	}

	reloaded.Remove("origin-box.tail37114b.ts.net")
	if _, ok := reloaded.IsDeclined("origin-box.tail37114b.ts.net"); ok {
		t.Error("adopting must clear the decline")
	}
}

func TestDeclineFileLivesBesideMachinesFile(t *testing.T) {
	dir := isolateFleet(t)
	if got, want := filepath.Dir(machines.DeclinedPath()), dir; got != want {
		t.Errorf("declined path dir = %q, want %q", got, want)
	}
	if filepath.Base(machines.DeclinedPath()) == filepath.Base(machines.MachinesPath()) {
		t.Error("the decline file must not collide with machines.yaml")
	}
}

func timeFixture() time.Time {
	return time.Date(2026, 7, 26, 9, 14, 22, 0, time.UTC)
}

func TestOriginHintSuppression(t *testing.T) {
	tests := []struct {
		name     string
		payload  string
		fleet    string
		declined bool
		want     bool
	}{
		{name: "no payload", want: false},
		{name: "malformed payload", payload: "v1;host=a;host=b;user=c", want: false},
		{name: "unregistered origin", payload: adoptPayload, want: true},
		{
			name:    "already registered under any id",
			payload: adoptPayload,
			fleet: `version: 1
machines:
    - id: whatever
      host: ORIGIN-BOX.tail37114b.ts.net.
      auth_method: ssh-agent
`,
			want: false,
		},
		{name: "declined origin stays quiet", payload: adoptPayload, declined: true, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := isolateFleet(t)
			resetOriginHintForTest()
			t.Setenv(HopOriginEnvVar, tt.payload)
			if tt.fleet != "" {
				if err := os.WriteFile(filepath.Join(dir, "machines.yaml"), []byte(tt.fleet), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if tt.declined {
				f, err := machines.LoadDeclined()
				if err != nil {
					t.Fatal(err)
				}
				f.Decline("origin-box", "origin-box.tail37114b.ts.net", timeFixture())
				if err := f.Save(); err != nil {
					t.Fatal(err)
				}
			}

			hint := OriginHint()
			if got := hint != ""; got != tt.want {
				t.Errorf("OriginHint() = %q, want present=%v", hint, tt.want)
			}
			if tt.want && !strings.Contains(hint, "camp machine adopt") {
				t.Errorf("hint must name the command: %q", hint)
			}
		})
	}
}

func TestOriginHintFiresAtMostOncePerProcess(t *testing.T) {
	isolateFleet(t)
	resetOriginHintForTest()
	t.Setenv(HopOriginEnvVar, adoptPayload)

	if first := OriginHint(); first == "" {
		t.Fatal("first call should produce the hint")
	}
	if second := OriginHint(); second != "" {
		t.Errorf("second call must stay quiet, got %q", second)
	}
}

func TestMachineListJSONNeverCarriesTheHint(t *testing.T) {
	// JSON consumers get data, not advice.
	isolateFleet(t)
	resetOriginHintForTest()
	t.Setenv(HopOriginEnvVar, adoptPayload)

	var buf bytes.Buffer
	mf, err := machines.Load()
	if err != nil {
		t.Fatal(err)
	}
	if err := writeMachineListJSON(&buf, mf); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), "camp machine adopt") {
		t.Errorf("json output carried the hint: %s", buf.String())
	}
}

func TestMachineTUIShowsOriginHintAtOpen(t *testing.T) {
	isolateFleet(t)
	resetOriginHintForTest()
	t.Setenv(HopOriginEnvVar, adoptPayload)

	mf, err := machines.Load()
	if err != nil {
		t.Fatal(err)
	}
	model := newMachineTUIModel(context.Background(), mf)
	if !strings.Contains(model.status, "camp machine adopt") {
		t.Errorf("status line = %q, want the adopt hint", model.status)
	}
	if model.statusKind == statusError {
		t.Error("a suggestion must not render as an error")
	}
	// Nor as a success. This is the first screen after a hop, and a check mark
	// beside "not in your fleet" reads as something that worked.
	if model.statusKind != statusAdvice {
		t.Errorf("status kind = %v, want %v", model.statusKind, statusAdvice)
	}
	if line := model.statusLine(); strings.Contains(line, "\u2713") {
		t.Errorf("advisory rendered with a success glyph: %q", line)
	}
}

func TestMachineTUIStaysQuietWithoutAnOrigin(t *testing.T) {
	isolateFleet(t)
	resetOriginHintForTest()
	t.Setenv(HopOriginEnvVar, "")

	mf, err := machines.Load()
	if err != nil {
		t.Fatal(err)
	}
	if status := newMachineTUIModel(context.Background(), mf).status; status != "" {
		t.Errorf("status = %q, want empty", status)
	}
}
