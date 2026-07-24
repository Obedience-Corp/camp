package workitem

import (
	"bytes"
	"encoding/json"
	"testing"

	wkitem "github.com/Obedience-Corp/camp/internal/workitem"
	"github.com/Obedience-Corp/camp/pkg/ledgerkit"
)

func TestCommitsWorkerCountCapsFanout(t *testing.T) {
	for _, repoCount := range []int{0, 1, 2, 100} {
		got := commitsWorkerCount(repoCount)
		if got < 1 {
			t.Fatalf("commitsWorkerCount(%d) = %d, want >= 1", repoCount, got)
		}
		if got > commitsMaxWorkers {
			t.Fatalf("commitsWorkerCount(%d) = %d, want <= %d", repoCount, got, commitsMaxWorkers)
		}
		if repoCount > 0 && got > repoCount {
			t.Fatalf("commitsWorkerCount(%d) = %d, want <= repo count", repoCount, got)
		}
	}
}

func TestEmitCommitsQueryWarnings(t *testing.T) {
	var stderr bytes.Buffer
	if err := emitCommitsQueryWarnings(&stderr, []commitsQueryError{{Repo: "demo", Err: "boom"}}); err != nil {
		t.Fatal(err)
	}
	if got := stderr.String(); got != "warning: 1 repo(s) failed; re-run with --json for details\n" {
		t.Fatalf("warning = %q", got)
	}

	stderr.Reset()
	if err := emitCommitsQueryWarnings(&stderr, nil); err != nil {
		t.Fatal(err)
	}
	if stderr.Len() != 0 {
		t.Fatalf("empty errors emitted warning: %q", stderr.String())
	}
}

func TestWorkitemAliasesCollectsRefIdKeyAndSlug(t *testing.T) {
	wi := &wkitem.WorkItem{
		StableID:     "design-camp-list-tui-2026-06-24",
		Key:          "design/camp-list-tui",
		RelativePath: "workflow/design/camp-list-tui",
	}
	got := WorkitemAliases("WI-abc123", wi)
	for _, want := range []string{"WI-abc123", "design-camp-list-tui-2026-06-24", "design/camp-list-tui", "camp-list-tui"} {
		if !got[want] {
			t.Fatalf("aliases missing %q; got %v", want, got)
		}
	}
	if WorkitemAliases("", nil)["."] {
		t.Fatal("empty inputs should not alias the current dir")
	}
}

func TestCommitsFromLedgerEventsMatchesAliasesAndShapesRecords(t *testing.T) {
	aliases := map[string]bool{"WI-abc123": true, "design-camp-list-tui-2026-06-24": true}
	events := []*ledgerkit.Event{
		{
			Kind:     ledgerkit.KindEvidenceAttached,
			TS:       "2026-06-24T10:00:00Z",
			Scope:    ledgerkit.Scope{Campaign: "c1", Workitem: "WI-abc123"},
			Why:      "[obey:c1-WI-abc123] feat: add tui",
			Payload:  map[string]any{"author": "Lance Rogers"},
			Evidence: []ledgerkit.Evidence{{Type: ledgerkit.EvidenceCommit, Repo: "camp", SHA: "aaa111"}},
		},
		{ // a repair attributing a commit under the stable-id form of the same workitem
			Kind:     ledgerkit.KindRepaired,
			TS:       "2026-06-25T10:00:00Z",
			Scope:    ledgerkit.Scope{Campaign: "c1", Workitem: "design-camp-list-tui-2026-06-24"},
			Evidence: []ledgerkit.Evidence{{Type: ledgerkit.EvidenceCommit, Repo: "camp", SHA: "bbb222"}},
		},
		{ // different workitem: must be ignored
			Kind:     ledgerkit.KindEvidenceAttached,
			TS:       "2026-06-26T10:00:00Z",
			Scope:    ledgerkit.Scope{Campaign: "c1", Workitem: "WI-other"},
			Evidence: []ledgerkit.Evidence{{Type: ledgerkit.EvidenceCommit, Repo: "camp", SHA: "ccc333"}},
		},
		{ // duplicate of the first commit: must dedupe by repo+sha
			Kind:     ledgerkit.KindEvidenceAttached,
			TS:       "2026-06-27T10:00:00Z",
			Scope:    ledgerkit.Scope{Campaign: "c1", Workitem: "WI-abc123"},
			Evidence: []ledgerkit.Evidence{{Type: ledgerkit.EvidenceCommit, Repo: "camp", SHA: "aaa111"}},
		},
	}
	got := commitsFromLedgerEvents(events, aliases)
	if len(got) != 2 {
		t.Fatalf("want 2 records (dedupe + alias filter), got %d: %+v", len(got), got)
	}
	byShaSet := map[string]bool{}
	for _, r := range got {
		byShaSet[r.SHA] = true
	}
	if !byShaSet["aaa111"] || !byShaSet["bbb222"] || byShaSet["ccc333"] {
		t.Fatalf("unexpected sha set: %v", byShaSet)
	}
	var first CommitRecord
	for _, r := range got {
		if r.SHA == "aaa111" {
			first = r
		}
	}
	if first.Author != "Lance Rogers" || first.Repo != "camp" || first.Subject != "[obey:c1-WI-abc123] feat: add tui" {
		t.Fatalf("record not shaped from the event: %+v", first)
	}
	if first.TagParts.WorkitemRef != "WI-abc123" {
		t.Fatalf("tag parsed from subject wrong: %+v", first.TagParts)
	}
	if first.Date.IsZero() {
		t.Fatal("date not parsed from ledger ts")
	}
}

func TestCommitsFromLedgerEventsAuthorFallsBackToActor(t *testing.T) {
	events := []*ledgerkit.Event{{
		Kind:     ledgerkit.KindEvidenceAttached,
		TS:       "2026-06-24T10:00:00Z",
		Scope:    ledgerkit.Scope{Workitem: "WI-x"},
		Actor:    ledgerkit.Actor{Type: ledgerkit.ActorAgent, Name: "obey-agent"},
		Evidence: []ledgerkit.Evidence{{Type: ledgerkit.EvidenceCommit, Repo: ".", SHA: "d1"}},
	}}
	got := commitsFromLedgerEvents(events, map[string]bool{"WI-x": true})
	if len(got) != 1 || got[0].Author != "obey-agent" {
		t.Fatalf("author fallback to actor failed: %+v", got)
	}
}

func TestEmitCommitsJSONIncludesSource(t *testing.T) {
	var out bytes.Buffer
	if err := emitCommitsJSON(&out, "ledger", nil, nil); err != nil {
		t.Fatal(err)
	}
	var payload struct {
		SchemaVersion string `json:"schema_version"`
		Source        string `json:"source"`
		Commits       []any  `json:"commits"`
	}
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Source != "ledger" {
		t.Fatalf("source = %q, want ledger", payload.Source)
	}
	if payload.SchemaVersion != WorkitemCommitsJSONVersion {
		t.Fatalf("schema_version = %q", payload.SchemaVersion)
	}
	if payload.Commits == nil {
		t.Fatal("commits must serialize as [] not null")
	}
}


func TestLedgerCommitDatePrefersPayload(t *testing.T) {
	ev := &ledgerkit.Event{
		TS: "2026-01-01T00:00:00Z",
		Payload: map[string]any{
			"commit_date": "2020-06-15T12:00:00Z",
			"author":      "Ada",
		},
	}
	got := ledgerCommitDate(ev)
	if got.Year() != 2020 {
		t.Fatalf("date = %v, want 2020 from payload", got)
	}
	if ledgerCommitAuthor(ev) != "Ada" {
		t.Fatalf("author = %q, want Ada", ledgerCommitAuthor(ev))
	}
}
