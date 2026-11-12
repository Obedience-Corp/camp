package approvalkit

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Obedience-Corp/camp/pkg/ledgerkit"
)

func ev(id, ts string, kind ledgerkit.Kind, payload map[string]any) *ledgerkit.Event {
	return &ledgerkit.Event{V: 1, ID: id, TS: ts, Kind: kind, Payload: payload, Source: ledgerkit.SourceCommand}
}

func reqPayload(source string, step int) map[string]any {
	return map[string]any{
		"key": RequestKey(source, step), "source": source, "source_kind": SourceStandaloneCheckpoint,
		"step": float64(step), "title": "t", "cwd": "workflow/explore/x",
	}
}

func TestFold(t *testing.T) {
	const src = "standalone:workflow/explore/x"
	key := RequestKey(src, 2)
	resolved := map[string]any{"key": key}

	tests := []struct {
		name   string
		events []*ledgerkit.Event
		want   []string // pending event ids, in order
	}{
		{"empty", nil, nil},
		{"requested stays pending", []*ledgerkit.Event{
			ev("a", "2026-01-01T00:00:01Z", KindRequested, reqPayload(src, 2)),
		}, []string{"a"}},
		{"approved clears", []*ledgerkit.Event{
			ev("a", "2026-01-01T00:00:01Z", KindRequested, reqPayload(src, 2)),
			ev("b", "2026-01-01T00:00:02Z", KindApproved, resolved),
		}, nil},
		{"rejected clears", []*ledgerkit.Event{
			ev("a", "2026-01-01T00:00:01Z", KindRequested, reqPayload(src, 2)),
			ev("b", "2026-01-01T00:00:02Z", KindRejected, resolved),
		}, nil},
		{"withdrawn clears", []*ledgerkit.Event{
			ev("a", "2026-01-01T00:00:01Z", KindRequested, reqPayload(src, 2)),
			ev("b", "2026-01-01T00:00:02Z", KindWithdrawn, resolved),
		}, nil},
		{"same step waiting again after a rejection shows once, as the new request", []*ledgerkit.Event{
			ev("a", "2026-01-01T00:00:01Z", KindRequested, reqPayload(src, 2)),
			ev("b", "2026-01-01T00:00:02Z", KindRejected, resolved),
			ev("c", "2026-01-01T00:00:03Z", KindRequested, reqPayload(src, 2)),
		}, []string{"c"}},
		{"duplicate request for one key shows once", []*ledgerkit.Event{
			ev("a", "2026-01-01T00:00:01Z", KindRequested, reqPayload(src, 2)),
			ev("c", "2026-01-01T00:00:03Z", KindRequested, reqPayload(src, 2)),
		}, []string{"c"}},
		{"unknown kinds and keyless events are ignored", []*ledgerkit.Event{
			ev("a", "2026-01-01T00:00:01Z", KindRequested, reqPayload(src, 2)),
			ev("x", "2026-01-01T00:00:02Z", ledgerkit.Kind("approval_from_the_future"), resolved),
			ev("y", "2026-01-01T00:00:03Z", KindApproved, map[string]any{}),
			nil,
		}, []string{"a"}},
		{"different steps and sources are independent, oldest first", []*ledgerkit.Event{
			ev("b", "2026-01-01T00:00:02Z", KindRequested, reqPayload("standalone:other", 1)),
			ev("a", "2026-01-01T00:00:01Z", KindRequested, reqPayload(src, 2)),
		}, []string{"a", "b"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Fold(tt.events)
			var ids []string
			for _, p := range got {
				ids = append(ids, p.EventID)
			}
			if strings.Join(ids, ",") != strings.Join(tt.want, ",") {
				t.Fatalf("pending = %v, want %v", ids, tt.want)
			}
		})
	}
}

func TestFoldCarriesRequestFields(t *testing.T) {
	e := ev("019a7c3e-51d2-7a10-9f44-2b6c0e8d1a35", "2026-01-01T00:00:01Z", KindRequested, map[string]any{
		"key": "s#3", "source": "s", "source_kind": SourceFestivalCheckpoint, "step": float64(3),
		"step_name": "REVIEW", "title": "Title", "body": "Body", "cwd": "festivals/active/f/001",
		"doc": "festivals/active/f/001/WORKFLOW.md", "writer": "wabc",
	})
	e.Actor = ledgerkit.Actor{Type: ledgerkit.ActorAgent, Name: "a"}
	e.Scope = ledgerkit.Scope{Campaign: "c", Festival: "F1"}
	got := Fold([]*ledgerkit.Event{e})
	if len(got) != 1 {
		t.Fatalf("pending = %d, want 1", len(got))
	}
	p := got[0]
	if p.ID != "ap-0e8d1a35" || p.Step != 3 || p.StepName != "REVIEW" || p.Title != "Title" ||
		p.Body != "Body" || p.Cwd != "festivals/active/f/001" || p.Writer != "wabc" ||
		p.SourceKind != SourceFestivalCheckpoint || p.Scope.Festival != "F1" || p.RequestedBy.Name != "a" {
		t.Fatalf("unexpected pending: %+v", p)
	}
}

func TestPlan(t *testing.T) {
	const src = "s"
	at := func(step int) Pending { return Pending{Source: src, Key: RequestKey(src, step), Step: step} }
	other := Pending{Source: "other", Key: RequestKey("other", 1), Step: 1}
	want := func(step int) *Request { return &Request{Source: src, Step: step} }

	tests := []struct {
		name        string
		pending     []Pending
		want        *Request
		wantResolve []int
		wantRequest bool
	}{
		{"nothing pending, nothing wanted", nil, nil, nil, false},
		{"first request", nil, want(2), nil, true},
		{"already pending is a no-op", []Pending{at(2)}, want(2), nil, false},
		{"no longer waiting resolves it", []Pending{at(2)}, nil, []int{2}, false},
		{"moved to a later step resolves the old and requests the new", []Pending{at(2)}, want(4), []int{2}, true},
		{"other sources are untouched", []Pending{other, at(2)}, nil, []int{2}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolve, request := Plan(tt.pending, src, tt.want)
			var steps []int
			for _, p := range resolve {
				steps = append(steps, p.Step)
			}
			if len(steps) != len(tt.wantResolve) {
				t.Fatalf("resolve = %v, want %v", steps, tt.wantResolve)
			}
			for i := range steps {
				if steps[i] != tt.wantResolve[i] {
					t.Fatalf("resolve = %v, want %v", steps, tt.wantResolve)
				}
			}
			if request != tt.wantRequest {
				t.Fatalf("request = %v, want %v", request, tt.wantRequest)
			}
		})
	}
}

func TestFind(t *testing.T) {
	pending := []Pending{
		{ID: "ap-aaaa1111", EventID: "019a-1"},
		{ID: "ap-aaaa2222", EventID: "019a-2"},
	}
	if p, err := Find(pending, "ap-aaaa1111"); err != nil || p.EventID != "019a-1" {
		t.Fatalf("exact id: %+v, %v", p, err)
	}
	if p, err := Find(pending, "019a-2"); err != nil || p.ID != "ap-aaaa2222" {
		t.Fatalf("event id: %+v, %v", p, err)
	}
	if p, err := Find(pending, "ap-aaaa2"); err != nil || p.ID != "ap-aaaa2222" {
		t.Fatalf("unique prefix: %+v, %v", p, err)
	}
	if _, err := Find(pending, "ap-aaaa"); err == nil {
		t.Fatal("ambiguous prefix must fail")
	}
	if _, err := Find(pending, "nope"); err == nil {
		t.Fatal("unknown selector must fail")
	}
	if _, err := Find(pending, " "); err == nil {
		t.Fatal("empty selector must fail")
	}
}

func TestIsCampRelative(t *testing.T) {
	for p, want := range map[string]bool{
		".": true, "workflow/explore/x": true, "a/../b": true,
		"": false, " ": false, "/etc": false, "..": false, "../x": false, "a/../../x": false,
	} {
		if got := IsCampRelative(p); got != want {
			t.Errorf("IsCampRelative(%q) = %v, want %v", p, got, want)
		}
	}
}

func request(source string, step int) *Request {
	return &Request{
		Source: source, SourceKind: SourceStandaloneCheckpoint, Step: step, StepName: "REVIEW",
		Title: "Review step", Body: "Please review.", Cwd: "workflow/explore/x",
		Doc: "workflow/explore/x/WORKFLOW.md", Scope: ledgerkit.Scope{Campaign: "c"},
	}
}

func recorder(t *testing.T, root, writer string) *Recorder {
	t.Helper()
	r, err := NewRecorder(root, writer, ledgerkit.Actor{Type: ledgerkit.ActorAgent, Name: "test"})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestSetWaitingLifecycle(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	const src = "standalone:workflow/explore/x"
	r := recorder(t, root, "wone")

	change, err := r.SetWaiting(ctx, src, request(src, 2), OutcomeWithdrawn, "")
	if err != nil {
		t.Fatal(err)
	}
	if change.Requested == nil || len(change.Resolved) != 0 {
		t.Fatalf("first call: %+v", change)
	}

	// The same call again records nothing.
	change, err = r.SetWaiting(ctx, src, request(src, 2), OutcomeWithdrawn, "")
	if err != nil || !change.Empty() {
		t.Fatalf("repeat call recorded something: %+v, %v", change, err)
	}
	pending, err := Load(ctx, root)
	if err != nil || len(pending) != 1 || pending[0].Step != 2 || pending[0].Writer != "wone" {
		t.Fatalf("pending after request: %+v, %v", pending, err)
	}

	// Approving step 2 while step 4 starts waiting records both.
	change, err = r.SetWaiting(ctx, src, request(src, 4), OutcomeApproved, "looks right")
	if err != nil {
		t.Fatal(err)
	}
	if change.Requested == nil || change.Requested.Step != 4 || len(change.Resolved) != 1 || change.Resolved[0].Step != 2 {
		t.Fatalf("approve-and-advance: %+v", change)
	}

	change, err = r.SetWaiting(ctx, src, nil, OutcomeRejected, "missing evidence")
	if err != nil || len(change.Resolved) != 1 || change.Requested != nil {
		t.Fatalf("reject: %+v, %v", change, err)
	}
	pending, err = Load(ctx, root)
	if err != nil || len(pending) != 0 {
		t.Fatalf("pending after reject: %+v, %v", pending, err)
	}

	// The record keeps every event, with the kinds the outcomes map to.
	reader, err := ledgerkit.NewReaderIn(root, Dir)
	if err != nil {
		t.Fatal(err)
	}
	events, _, err := reader.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var kinds []string
	for _, e := range events {
		kinds = append(kinds, string(e.Kind))
	}
	want := "approval_requested,approval_approved,approval_requested,approval_rejected"
	if strings.Join(kinds, ",") != want {
		t.Fatalf("ledger kinds = %v, want %s", kinds, want)
	}
	if events[3].Why != "missing evidence" {
		t.Fatalf("rejection reason not recorded: %+v", events[3])
	}
}

func TestSetWaitingValidates(t *testing.T) {
	ctx := context.Background()
	r := recorder(t, t.TempDir(), "wone")
	const src = "s"

	bad := map[string]func(*Request){
		"other source":  func(q *Request) { q.Source = "x" },
		"no kind":       func(q *Request) { q.SourceKind = "" },
		"step zero":     func(q *Request) { q.Step = 0 },
		"no title":      func(q *Request) { q.Title = " " },
		"absolute cwd":  func(q *Request) { q.Cwd = "/etc" },
		"escaping cwd":  func(q *Request) { q.Cwd = "../outside" },
		"absolute doc":  func(q *Request) { q.Doc = "/etc/passwd" },
		"empty cwd":     func(q *Request) { q.Cwd = "" },
		"escaping doc2": func(q *Request) { q.Doc = "a/../../b" },
	}
	for name, mutate := range bad {
		t.Run(name, func(t *testing.T) {
			q := request(src, 1)
			mutate(q)
			if _, err := r.SetWaiting(ctx, src, q, OutcomeWithdrawn, ""); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
	if _, err := r.SetWaiting(ctx, " ", nil, OutcomeWithdrawn, ""); err == nil {
		t.Fatal("empty source must fail")
	}
}

func TestLoadEmptyCampaign(t *testing.T) {
	pending, err := Load(context.Background(), t.TempDir())
	if err != nil || len(pending) != 0 {
		t.Fatalf("empty camp: %+v, %v", pending, err)
	}
}

func TestIndexIsDisposableAndCatchesUp(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	const src = "s"
	if _, err := recorder(t, root, "wone").SetWaiting(ctx, src, request(src, 1), OutcomeWithdrawn, ""); err != nil {
		t.Fatal(err)
	}
	indexPath := filepath.Join(root, filepath.FromSlash(IndexRel))

	// Deleting the index loses nothing.
	if err := os.Remove(indexPath); err != nil {
		t.Fatal(err)
	}
	pending, err := Load(ctx, root)
	if err != nil || len(pending) != 1 {
		t.Fatalf("after deleting the index: %+v, %v", pending, err)
	}
	if _, err := os.Stat(indexPath); err != nil {
		t.Fatalf("index was not rebuilt: %v", err)
	}

	// A corrupt index is rebuilt rather than trusted.
	if err := os.WriteFile(indexPath, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if pending, err = Load(ctx, root); err != nil || len(pending) != 1 {
		t.Fatalf("after corrupting the index: %+v, %v", pending, err)
	}

	// Another machine's shard arriving (as a pull would deliver it) is picked
	// up, including a resolution of a request this machine recorded.
	other, err := ledgerkit.NewWriterIn(root, Dir, "wtwo")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().Add(time.Second)
	arrive := func(id string, kind ledgerkit.Kind, payload map[string]any) {
		t.Helper()
		e := ev(id, ledgerkit.NowUTC(now), kind, payload)
		now = now.Add(time.Second)
		if err := other.Append(ctx, e); err != nil {
			t.Fatal(err)
		}
	}
	arrive("remote-1", KindRequested, reqPayload("remote", 7))
	if pending, err = Load(ctx, root); err != nil || len(pending) != 2 {
		t.Fatalf("after a remote request arrived: %+v, %v", pending, err)
	}
	arrive("remote-2", KindApproved, map[string]any{"key": RequestKey(src, 1)})
	pending, err = Load(ctx, root)
	if err != nil || len(pending) != 1 || pending[0].Source != "remote" {
		t.Fatalf("after a remote approval arrived: %+v, %v", pending, err)
	}

	// A shard that disappears (a branch switch, say) invalidates the index.
	if err := os.RemoveAll(filepath.Join(root, filepath.FromSlash(Dir))); err != nil {
		t.Fatal(err)
	}
	if pending, err = Load(ctx, root); err != nil || len(pending) != 0 {
		t.Fatalf("after the ledger was removed: %+v, %v", pending, err)
	}
}

func TestIndexFastPathLeavesIndexAlone(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	if _, err := recorder(t, root, "wone").SetWaiting(ctx, "s", request("s", 1), OutcomeWithdrawn, ""); err != nil {
		t.Fatal(err)
	}
	indexPath := filepath.Join(root, filepath.FromSlash(IndexRel))
	// Mark the index so a rewrite would be visible.
	var idx map[string]any
	data, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &idx); err != nil {
		t.Fatal(err)
	}
	idx["built_at"] = "marker"
	marked, _ := json.Marshal(idx)
	if err := os.WriteFile(indexPath, marked, 0o644); err != nil {
		t.Fatal(err)
	}
	if pending, err := Load(ctx, root); err != nil || len(pending) != 1 {
		t.Fatalf("load: %+v, %v", pending, err)
	}
	after, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(after), "marker") {
		t.Fatal("an unchanged ledger must not rebuild the index")
	}
}

func TestConcurrentWritersLoseNothing(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	const writers, each = 4, 10
	var wg sync.WaitGroup
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			r, err := NewRecorder(root, "w"+string(rune('a'+w)), ledgerkit.Actor{Type: ledgerkit.ActorAgent})
			if err != nil {
				t.Error(err)
				return
			}
			for i := 1; i <= each; i++ {
				src := "src-" + string(rune('a'+w)) + "-" + string(rune('a'+i))
				if _, err := r.SetWaiting(ctx, src, request(src, 1), OutcomeWithdrawn, ""); err != nil {
					t.Error(err)
				}
			}
		}(w)
	}
	wg.Wait()
	pending, err := Load(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != writers*each {
		t.Fatalf("pending = %d, want %d", len(pending), writers*each)
	}
}

func TestBodyIsBounded(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	q := request("s", 1)
	q.Body = strings.Repeat("é", maxBodyBytes) // 2 bytes each, so well over the cap
	if _, err := recorder(t, root, "wone").SetWaiting(ctx, "s", q, OutcomeWithdrawn, ""); err != nil {
		t.Fatal(err)
	}
	pending, err := Load(ctx, root)
	if err != nil || len(pending) != 1 {
		t.Fatalf("load: %+v, %v", pending, err)
	}
	body := pending[0].Body
	if len(body) > maxBodyBytes+len("\n[truncated]") || !strings.HasSuffix(body, "[truncated]") {
		t.Fatalf("body not bounded: %d bytes", len(body))
	}
	if strings.ContainsRune(body, '�') {
		t.Fatal("truncation split a UTF-8 sequence")
	}
}

func TestResolveCwd(t *testing.T) {
	root := t.TempDir()
	inside := filepath.Join(root, "workflow", "explore", "x")
	if err := os.MkdirAll(inside, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "file"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	got, err := ResolveCwd(root, "workflow/explore/x")
	if err != nil {
		t.Fatal(err)
	}
	wantAbs, _ := filepath.EvalSymlinks(inside)
	if got != wantAbs {
		t.Fatalf("ResolveCwd = %q, want %q", got, wantAbs)
	}
	for _, bad := range []string{"/etc", "../x", "missing", "file", "escape", ""} {
		if _, err := ResolveCwd(root, bad); err == nil {
			t.Errorf("ResolveCwd(%q) must fail", bad)
		}
	}
}

func TestLedgerDirIsSeparateFromEvents(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	if _, err := recorder(t, root, "wone").SetWaiting(ctx, "s", request("s", 1), OutcomeWithdrawn, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(ledgerkit.EventsDir))); !os.IsNotExist(err) {
		t.Fatalf("approvals must not write the campaign event ledger: %v", err)
	}
	matches, _ := filepath.Glob(filepath.Join(root, filepath.FromSlash(Dir), "*", "wone.jsonl"))
	if len(matches) != 1 {
		t.Fatalf("expected one approvals shard, got %v", matches)
	}
}
