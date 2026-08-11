package tailnet

import (
	"errors"
	"sync"
	"testing"
)

// Moved from cmd/camp/machine_resolve_test.go when the parser moved here.
func TestParseHealthShapes(t *testing.T) {
	tests := []struct {
		name    string
		data    string
		wantOK  bool
		want    []string
		wantLen int
	}{
		{
			name:    "string array (the shape tailscale ships today)",
			data:    `{"Health":["DNS is broken","exit status 1"]}`,
			wantOK:  true,
			want:    []string{"DNS is broken", "exit status 1"},
			wantLen: 2,
		},
		{
			name:    "structured entries prefer the specific text over the title",
			data:    `{"Health":[{"Title":"DNS","Text":"DNS config could not be read"}]}`,
			wantOK:  true,
			want:    []string{"DNS config could not be read"},
			wantLen: 1,
		},
		{
			name:    "structured entry with only a title still reports it",
			data:    `{"Health":[{"Title":"DNS unavailable"}]}`,
			wantOK:  true,
			want:    []string{"DNS unavailable"},
			wantLen: 1,
		},
		{
			name: "unknown entry shapes are skipped, not fatal",
			data: `{"Health":["DNS is broken",12345,{"Title":"DNS","Text":"also this"}]}`,
			// The number decodes as neither string nor object and drops out;
			// the entries around it survive.
			wantOK:  true,
			want:    []string{"DNS is broken", "also this"},
			wantLen: 2,
		},
		{
			name:    "a warning banner before the JSON is skipped",
			data:    "Warning: client version is older than the server\n{\"Health\":[\"DNS is broken\"]}",
			wantOK:  true,
			want:    []string{"DNS is broken"},
			wantLen: 1,
		},
		{
			name: "healthy tailnet reports readable-but-empty",
			data: `{"Health":[]}`,
			// Readable and empty is a real answer (nothing is wrong) and must
			// be distinguishable from unreadable, or the hint quotes silence.
			wantOK:  true,
			wantLen: 0,
		},
		{name: "no JSON at all is unreadable", data: "command not found", wantOK: false},
		{name: "malformed JSON is unreadable", data: `{"Health":[`, wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := ParseHealth([]byte(tt.data))
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if !ok {
				return
			}
			if len(got) != tt.wantLen {
				t.Fatalf("got %d message(s) %q, want %d", len(got), got, tt.wantLen)
			}
			for i, want := range tt.want {
				if got[i] != want {
					t.Errorf("message %d = %q, want %q", i, got[i], want)
				}
			}
		})
	}
}

func TestParsePeerAddress(t *testing.T) {
	status := `{
		"Peer": {
			"key1": {"DNSName": "mac-studio.example-net.ts.net.", "TailscaleIPs": ["100.72.165.77", "fd7a:115c:a1e0::1"]},
			"key2": {"DNSName": "v6only.example-net.ts.net.", "TailscaleIPs": ["fd7a:115c:a1e0::2"]},
			"key3": {"DNSName": "offline.example-net.ts.net.", "TailscaleIPs": ["100.72.165.99"], "Online": false},
			"key4": {"DNSName": "noaddr.example-net.ts.net.", "TailscaleIPs": []}
		}
	}`
	tests := []struct {
		name      string
		data      string
		dnsName   string
		want      string
		wantFound bool
	}{
		{
			name: "match tolerates trailing dot and casing on both sides",
			data: status, dnsName: "MAC-STUDIO.example-net.ts.net", want: "100.72.165.77", wantFound: true,
		},
		{
			name: "IPv4 preferred over IPv6",
			data: status, dnsName: "mac-studio.example-net.ts.net.", want: "100.72.165.77", wantFound: true,
		},
		{
			name: "v6-only peer yields its v6 address",
			data: status, dnsName: "v6only.example-net.ts.net", want: "fd7a:115c:a1e0::2", wantFound: true,
		},
		{
			// Documented decision, not an accident: the address is still the
			// right one to dial; ssh reports the truth about reachability.
			name: "offline peer is still returned",
			data: status, dnsName: "offline.example-net.ts.net", want: "100.72.165.99", wantFound: true,
		},
		{
			name: "peer with no usable address is not found",
			data: status, dnsName: "noaddr.example-net.ts.net", wantFound: false,
		},
		{
			name: "unknown peer is not found",
			data: status, dnsName: "ghost.example-net.ts.net", wantFound: false,
		},
		{
			name: "empty name is not found",
			data: status, dnsName: "  ", wantFound: false,
		},
		{
			name: "warning banner before the JSON is skipped",
			data: "Warning: update available\n" + status, dnsName: "mac-studio.example-net.ts.net",
			want: "100.72.165.77", wantFound: true,
		},
		{name: "no JSON is not found", data: "command not found", dnsName: "mac-studio.example-net.ts.net", wantFound: false},
		{name: "malformed JSON is not found", data: `{"Peer":{`, dnsName: "mac-studio.example-net.ts.net", wantFound: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, found := ParsePeerAddress([]byte(tt.data), tt.dnsName)
			if found != tt.wantFound {
				t.Fatalf("found = %v, want %v (addr %q)", found, tt.wantFound, got)
			}
			if found && got != tt.want {
				t.Errorf("addr = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPreferredAddress(t *testing.T) {
	tests := []struct {
		name      string
		ips       []string
		want      string
		wantFound bool
	}{
		{name: "v4 first entry", ips: []string{"100.1.2.3", "fd7a::1"}, want: "100.1.2.3", wantFound: true},
		{name: "v4 wins even when listed second", ips: []string{"fd7a::1", "100.1.2.3"}, want: "100.1.2.3", wantFound: true},
		{name: "v6 only", ips: []string{"fd7a::1"}, want: "fd7a::1", wantFound: true},
		{name: "garbage entries are skipped", ips: []string{"not-an-ip", " 100.1.2.3 "}, want: "100.1.2.3", wantFound: true},
		{name: "nothing usable", ips: []string{"", "nope"}, wantFound: false},
		{name: "empty", ips: nil, wantFound: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, found := preferredAddress(tt.ips)
			if found != tt.wantFound || got != tt.want {
				t.Errorf("got (%q, %v), want (%q, %v)", got, found, tt.want, tt.wantFound)
			}
		})
	}
}

func TestIsMagicDNSName(t *testing.T) {
	for name, want := range map[string]bool{
		"devbox.tailnet.ts.net":   true,
		"devbox.tailnet.ts.net.":  true,
		"DEVBOX.TAILNET.TS.NET":   true,
		"devbox.example.com":      false,
		"tsnet-lookalike.ts.nett": false,
		"":                        false,
	} {
		if got := IsMagicDNSName(name); got != want {
			t.Errorf("IsMagicDNSName(%q) = %v, want %v", name, got, want)
		}
	}
}

// The `list --remote` fan-out resolves many machines concurrently; the status
// snapshot must be read exactly once and never race. Run with -race.
func TestStatusSourceReadsOnceUnderConcurrency(t *testing.T) {
	calls := 0
	src := &statusSource{run: func() ([]byte, error) {
		calls++
		return []byte(`{"Peer":{"k":{"DNSName":"a.b.ts.net.","TailscaleIPs":["100.1.1.1"]}}}`), nil
	}}
	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := src.get(); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if calls != 1 {
		t.Fatalf("status read %d times, want 1", calls)
	}
}

// A failed read is memoized too: a machine without tailscale must not pay a
// failed subprocess per machine in a fleet operation.
func TestStatusSourceMemoizesFailure(t *testing.T) {
	calls := 0
	src := &statusSource{run: func() ([]byte, error) {
		calls++
		return nil, errors.New("tailscale: command not found")
	}}
	for range 3 {
		if _, err := src.get(); err == nil {
			t.Fatal("want error")
		}
	}
	if calls != 1 {
		t.Fatalf("status read %d times, want 1", calls)
	}
}
