package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// TestParseTailscaleStatusFixture exercises the committed tailscale status
// --json fixture (testdata/tailscale_status.json: one Self + one Peer) and
// proves Self is excluded from the device list while the Peer survives with
// its fields mapped per the app's DiscoveredDevice
// (festival-app/src-tauri/src/commands/tailscale.rs:15-23). No live tailscale
// is invoked.
func TestParseTailscaleStatusFixture(t *testing.T) {
	data, err := os.ReadFile("testdata/tailscale_status.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	devices, err := parseTailscaleStatus(data)
	if err != nil {
		t.Fatalf("parseTailscaleStatus() error = %v", err)
	}
	if len(devices) != 1 {
		t.Fatalf("len(devices) = %d, want 1 (Self excluded, one Peer)", len(devices))
	}

	got := devices[0]
	want := discoveredDevice{
		HostName: "Devbox",
		Host:     "devbox.tail37114b.ts.net",
		DNSName:  "devbox.tail37114b.ts.net",
		Online:   true,
		OS:       "linux",
	}
	if got != want {
		t.Fatalf("devices[0] = %+v, want %+v", got, want)
	}
}

func TestParseTailscaleStatusSkipsHostlessPeer(t *testing.T) {
	data := []byte(`{
  "BackendState": "Running",
  "Peer": {
    "nodekey:1": { "HostName": "headless", "DNSName": "", "TailscaleIPs": [], "Online": true }
  }
}`)
	devices, err := parseTailscaleStatus(data)
	if err != nil {
		t.Fatalf("parseTailscaleStatus() error = %v", err)
	}
	if len(devices) != 0 {
		t.Fatalf("len(devices) = %d, want 0 (peer has no DNSName or TailscaleIP)", len(devices))
	}
}

func TestParseTailscaleStatusFallsBackToTailscaleIP(t *testing.T) {
	data := []byte(`{
  "BackendState": "Running",
  "Peer": {
    "nodekey:1": { "HostName": "headless", "DNSName": "", "TailscaleIPs": ["100.100.100.100"], "Online": true }
  }
}`)
	devices, err := parseTailscaleStatus(data)
	if err != nil {
		t.Fatalf("parseTailscaleStatus() error = %v", err)
	}
	if len(devices) != 1 || devices[0].Host != "100.100.100.100" || devices[0].DNSName != "" {
		t.Fatalf("devices = %+v, want single device with host=100.100.100.100 and empty DNSName", devices)
	}
}

func TestParseTailscaleStatusSkipsWarningBanner(t *testing.T) {
	data := []byte("Warning: client version differs\n" + `{
  "BackendState": "Running",
  "Peer": {
    "nodekey:1": { "HostName": "devbox", "DNSName": "devbox.ts.net.", "Online": true }
  }
}`)
	devices, err := parseTailscaleStatus(data)
	if err != nil {
		t.Fatalf("parseTailscaleStatus() error = %v", err)
	}
	if len(devices) != 1 || devices[0].Host != "devbox.ts.net" {
		t.Fatalf("devices = %+v, want single device host=devbox.ts.net", devices)
	}
}

func TestParseTailscaleStatusBackendNotRunning(t *testing.T) {
	tests := []struct {
		state string
		want  string
	}{
		{"NeedsLogin", "logged out"},
		{"Stopped", "stopped"},
		{"NoState", "not ready"},
	}
	for _, tt := range tests {
		t.Run(tt.state, func(t *testing.T) {
			data := []byte(`{"BackendState": "` + tt.state + `", "Peer": {}}`)
			_, err := parseTailscaleStatus(data)
			if err == nil {
				t.Fatalf("parseTailscaleStatus() error = nil, want actionable error for state %q", tt.state)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %q, want to contain %q", err.Error(), tt.want)
			}
		})
	}
}

func TestParseTailscaleStatusRejectsUnparsableOutput(t *testing.T) {
	if _, err := parseTailscaleStatus([]byte("")); err == nil {
		t.Fatal("parseTailscaleStatus(\"\") error = nil, want error")
	}
	if _, err := parseTailscaleStatus([]byte("not json")); err == nil {
		t.Fatal("parseTailscaleStatus(\"not json\") error = nil, want error")
	}
	if _, err := parseTailscaleStatus([]byte("{not valid json")); err == nil {
		t.Fatal("parseTailscaleStatus(malformed) error = nil, want error")
	}
}

func TestSanitizeID(t *testing.T) {
	tests := map[string]string{
		"devbox.tail37114b.ts.net": "devbox",
		"devbox.tailnet.ts.net.":   "devbox",
		"Buildbox":                 "buildbox",
		"UPPER.example.com":        "upper",
	}
	for in, want := range tests {
		if got := sanitizeID(in); got != want {
			t.Errorf("sanitizeID(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDeriveMachineID(t *testing.T) {
	id, err := deriveMachineID(discoveredDevice{DNSName: "devbox.tail37114b.ts.net", HostName: "Devbox"})
	if err != nil {
		t.Fatalf("deriveMachineID() error = %v", err)
	}
	if id != "devbox" {
		t.Fatalf("deriveMachineID() = %q, want %q", id, "devbox")
	}

	// A device whose only name is unusable as an id (spaces/punctuation, no
	// DNSName to fall back on) must fail clearly rather than derive garbage.
	if _, err := deriveMachineID(discoveredDevice{HostName: "MacBook Pro (2)"}); err == nil {
		t.Fatal("deriveMachineID() error = nil, want error for an unsanitizable host name")
	}

	// "local" is reserved; a device that happens to be named that must not
	// silently collide with the implicit local machine.
	if _, err := deriveMachineID(discoveredDevice{DNSName: "local.ts.net"}); err == nil {
		t.Fatal("deriveMachineID() error = nil, want error deriving the reserved id \"local\"")
	}
}

// TestRunTailscaleStatusMissingBinary proves a missing tailscale binary yields
// a clear, actionable error (not a raw exec error) — simulated via an empty
// PATH so exec.LookPath fails exactly as it would with tailscale uninstalled,
// with no filesystem mutation and no real tailscale invocation.
func TestRunTailscaleStatusMissingBinary(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	_, err := runTailscaleStatus(context.Background())
	if err == nil {
		t.Fatal("runTailscaleStatus() error = nil, want error when tailscale is not on PATH")
	}
	if !errors.Is(err, exec.ErrNotFound) {
		t.Fatalf("runTailscaleStatus() error = %v, want to wrap exec.ErrNotFound", err)
	}
	if !strings.Contains(err.Error(), "install Tailscale") {
		t.Fatalf("error = %q, want an actionable install hint", err.Error())
	}
}

func TestDiscoverTailnetPropagatesRunError(t *testing.T) {
	wantErr := errors.New("boom")
	_, err := discoverTailnet(context.Background(), func(context.Context) ([]byte, error) {
		return nil, wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("discoverTailnet() error = %v, want to wrap %v", err, wantErr)
	}
}

func TestDiscoverTailnetHonorsCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	called := false
	_, err := discoverTailnet(ctx, func(context.Context) ([]byte, error) {
		called = true
		return nil, nil
	})
	if err == nil {
		t.Fatal("discoverTailnet(cancelled ctx) error = nil, want context.Canceled")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("discoverTailnet(cancelled ctx) error = %v, want context.Canceled", err)
	}
	if called {
		t.Fatal("discoverTailnet(cancelled ctx) invoked the run func; want early return before exec")
	}
}
