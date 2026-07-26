package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/machines"
)

func TestSanitizeSnapshotNames(t *testing.T) {
	tests := []struct {
		name    string
		in      []string
		want    []string
		wantErr bool
	}{
		{name: "comma separated", in: []string{"a,b,c"}, want: []string{"a", "b", "c"}},
		{name: "trims and drops empties", in: []string{" a , ,b "}, want: []string{"a", "b"}},
		{name: "empty input is an error", in: nil, wantErr: true},
		{name: "only separators is an error", in: []string{",,"}, wantErr: true},
		// A name is display and completion data on the receiver, never a path.
		{name: "path separator rejected", in: []string{"a/b"}, wantErr: true},
		{name: "colon rejected", in: []string{"a:b"}, wantErr: true},
		{name: "newline rejected", in: []string{"a\nb"}, wantErr: true},
		{name: "null byte rejected", in: []string{"a\x00b"}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := sanitizeSnapshotNames(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("want error, got %v", got)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if strings.Join(got, ",") != strings.Join(tt.want, ",") {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSanitizeSnapshotNamesTruncatesRatherThanRefuses(t *testing.T) {
	// A partial completion list is useful; an error is not.
	many := make([]string, 0, snapshotMaxNames+50)
	for i := 0; i < snapshotMaxNames+50; i++ {
		many = append(many, "campaign")
	}
	got, err := sanitizeSnapshotNames(many)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != snapshotMaxNames {
		t.Errorf("len = %d, want the cap %d", len(got), snapshotMaxNames)
	}
}

func TestCachePutRejectsHostileMachineID(t *testing.T) {
	// The id becomes the cache FILE NAME, so this validation is what keeps the
	// write inside the cache directory.
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	cachePutCampaigns = []string{"a"}
	t.Cleanup(func() { cachePutCampaigns = nil })

	for _, bad := range []string{"../escape", "/abs", "local", "Has-Caps", "1leading"} {
		cmd := &cobra.Command{}
		cmd.SetContext(context.Background())
		cmd.SetOut(new(strings.Builder))
		cmd.SetErr(new(strings.Builder))
		if err := runMachineCachePut(cmd, []string{bad}); err == nil {
			t.Errorf("machine id %q was accepted", bad)
		}
	}
}

func TestCachePutWritesSnapshotEntry(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	cachePutCampaigns = []string{"alpha,beta"}
	t.Cleanup(func() { cachePutCampaigns = nil })

	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	var out, errb strings.Builder
	cmd.SetOut(&out)
	cmd.SetErr(&errb)

	if err := runMachineCachePut(cmd, []string{"mac-studio"}); err != nil {
		t.Fatal(err)
	}
	if out.Len() != 0 {
		t.Errorf("stdout must stay empty so callers need not parse it, got %q", out.String())
	}

	names, ok := readMachineCacheCampaigns("mac-studio")
	if !ok {
		t.Fatal("pushed snapshot did not read back")
	}
	if strings.Join(names, ",") != "alpha,beta" {
		t.Errorf("names = %v", names)
	}
}

func TestPushedSnapshotSurvivesTheShortCompletionTTL(t *testing.T) {
	// The whole point of the source field: a pushed snapshot older than 60s must
	// still be offered, or the feature is a no-op an hour after it works.
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	if err := os.MkdirAll(machineCacheDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-30 * time.Minute).UnixNano()

	write := func(id string, entry machineCacheEntry) {
		data, err := json.Marshal(entry)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(machineCacheDir()+"/"+id+".json", data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("pushed", machineCacheEntry{Campaigns: []string{"a"}, FetchedAt: old, Source: snapshotSource})
	write("pulled", machineCacheEntry{Campaigns: []string{"a"}, FetchedAt: old})

	if _, ok := readMachineCacheCampaigns("pushed"); !ok {
		t.Error("a 30-minute-old pushed snapshot must still be fresh")
	}
	if _, ok := readMachineCacheCampaigns("pulled"); ok {
		t.Error("a 30-minute-old pulled entry must be stale")
	}
}

func TestOldCampIgnoresTheSourceField(t *testing.T) {
	// Decoder tolerance: a pre-change camp unmarshals into a struct without
	// Source and applies the 60s rule, degrading to today's behavior.
	type oldEntry struct {
		Campaigns []string `json:"campaigns"`
		FetchedAt int64    `json:"fetched_at"`
	}
	data, err := json.Marshal(machineCacheEntry{
		Campaigns: []string{"a", "b"},
		FetchedAt: time.Now().UnixNano(),
		Source:    snapshotSource,
	})
	if err != nil {
		t.Fatal(err)
	}
	var old oldEntry
	if err := json.Unmarshal(data, &old); err != nil {
		t.Fatalf("an older camp must still parse the file: %v", err)
	}
	if len(old.Campaigns) != 2 {
		t.Errorf("campaigns = %v", old.Campaigns)
	}
}

func TestPushSilenceMatrix(t *testing.T) {
	m := &machines.Machine{ID: "archdtop", Host: "archdtop.ts.net", SSHUser: "lance"}

	tests := []struct {
		name      string
		selfID    string
		names     []string
		runErr    error
		wantCalls int
	}{
		{name: "happy path pushes once", selfID: "mac-studio", names: []string{"a"}, wantCalls: 1},
		{name: "no self id, no push", selfID: "", names: []string{"a"}},
		{name: "invalid self id, no push", selfID: "Not-Valid", names: []string{"a"}},
		{name: "no campaigns, no push", selfID: "mac-studio"},
		// Old remote: unknown verb exits 127. Swallowed.
		{name: "old remote is silent", selfID: "mac-studio", names: []string{"a"},
			runErr: errors.New("exit status 127: unknown command"), wantCalls: 1},
		{name: "unreachable is silent", selfID: "mac-studio", names: []string{"a"},
			runErr: errors.New("connection refused"), wantCalls: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			restore := pushSnapshotRun
			pushSnapshotRun = func(context.Context, *machines.Machine, string) ([]byte, error) {
				calls++
				return nil, tt.runErr
			}
			t.Cleanup(func() { pushSnapshotRun = restore })

			// The contract is that this never panics and never returns an error;
			// there is no error to assert because there is no error path.
			pushSelfSnapshot(context.Background(), m, tt.selfID, tt.names)

			if calls != tt.wantCalls {
				t.Errorf("ssh calls = %d, want %d", calls, tt.wantCalls)
			}
		})
	}
}

func TestPushCarriesNamesOnly(t *testing.T) {
	// The invariant, asserted as an absence so a future field addition fails
	// this test rather than quietly shipping.
	var sent string
	restore := pushSnapshotRun
	pushSnapshotRun = func(_ context.Context, _ *machines.Machine, args string) ([]byte, error) {
		sent = args
		return nil, nil
	}
	t.Cleanup(func() { pushSnapshotRun = restore })

	m := &machines.Machine{ID: "archdtop", Host: "archdtop.ts.net", SSHUser: "lance", IdentityFile: "/keys/secret"}
	pushSelfSnapshot(context.Background(), m, "mac-studio", []string{"obey-campaign"})

	for _, forbidden := range []string{"/keys/secret", "/Users/", "8deed8b4", "archdtop.ts.net", "org", "path"} {
		if strings.Contains(sent, forbidden) {
			t.Errorf("push argv leaked %q: %s", forbidden, sent)
		}
	}
	if !strings.Contains(sent, "obey-campaign") || !strings.Contains(sent, "mac-studio") {
		t.Errorf("push argv missing the names it should carry: %s", sent)
	}
}
