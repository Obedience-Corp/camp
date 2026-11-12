package approvalkit

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/pkg/ledgerkit"
)

// indexVersion is the on-disk format of the pending index. A different version
// is treated as a missing index and rebuilt.
//
// Bump it whenever Fold's rules change, not only when the file's shape does.
// camp and fest each link their own copy of this package and share one index
// file, so a binary with older rules would otherwise leave behind a pending set
// that a newer binary reads as current.
const indexVersion = 3

// index is the cached pending set plus how much of each shard it has seen.
type index struct {
	V       int              `json:"v"`
	BuiltAt string           `json:"built_at"`
	Cursor  ledgerkit.Cursor `json:"cursor"`
	Pending []Pending        `json:"pending"`
}

// Load returns the requests currently pending in the campaign at campaignRoot,
// oldest first.
//
// The common case costs one small file read plus a size check per shard: the
// index is used as is when no shard has grown since it was built. When the
// ledger has content the index has not seen (a local write, a git pull, a
// removed shard) the pending set is rebuilt from the whole ledger. A rebuild
// rather than an incremental fold is deliberate: events pulled from another
// machine can be older than ones already folded, and the approvals ledger is
// small enough that replaying it is cheap.
//
// A campaign with no approvals ledger reads as empty. A failure to write the
// refreshed index is not an error: the index is a cache, and the pending set
// just read is still correct.
func Load(ctx context.Context, campaignRoot string) ([]Pending, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	reader, err := ledgerkit.NewReaderIn(campaignRoot, Dir)
	if err != nil {
		return nil, err
	}
	path := filepath.Join(campaignRoot, filepath.FromSlash(IndexRel))

	if cached, ok := readIndex(path); ok {
		since, err := reader.ReadSince(ctx, cached.Cursor)
		if err != nil {
			return nil, err
		}
		if indexCurrent(cached.Cursor, since) {
			return cached.Pending, nil
		}
	}

	full, err := reader.ReadSince(ctx, ledgerkit.Cursor{})
	if err != nil {
		return nil, err
	}
	pending := Fold(full.Events)
	writeIndex(path, index{
		V:       indexVersion,
		BuiltAt: ledgerkit.NowUTC(time.Now()),
		Cursor:  full.Next,
		Pending: pending,
	})
	return pending, nil
}

// indexCurrent reports whether an index built at cursor still describes the
// ledger: nothing new was read, no shard it counted has disappeared, and no
// shard shrank (ledgerkit reports a shrink as a skipped line and re-reads it,
// which also surfaces as new events).
func indexCurrent(cursor ledgerkit.Cursor, since *ledgerkit.SinceResult) bool {
	if len(since.Events) > 0 {
		return false
	}
	if since.Report != nil && len(since.Report.Skipped) > 0 {
		return false
	}
	for shard := range cursor.Offsets {
		if _, ok := since.Next.Offsets[shard]; !ok {
			return false
		}
	}
	for shard, offset := range since.Next.Offsets {
		if cursor.Offsets[shard] != offset {
			return false
		}
	}
	return true
}

// readIndex loads the cached index. Anything unreadable, malformed, or of
// another version reads as absent so the caller rebuilds.
func readIndex(path string) (index, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return index{}, false
	}
	var idx index
	if err := json.Unmarshal(data, &idx); err != nil || idx.V != indexVersion {
		return index{}, false
	}
	if idx.Pending == nil {
		idx.Pending = []Pending{}
	}
	return idx, true
}

// writeIndex replaces the cached index atomically. It is best-effort: the index
// is rebuilt from the ledger whenever it is missing or stale.
func writeIndex(path string, idx index) {
	if idx.Pending == nil {
		idx.Pending = []Pending{}
	}
	data, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	tmp, err := os.CreateTemp(dir, ".pending-*.json")
	if err != nil {
		return
	}
	tmpName := tmp.Name()
	_, writeErr := tmp.Write(append(data, '\n'))
	closeErr := tmp.Close()
	if writeErr != nil || closeErr != nil {
		_ = os.Remove(tmpName)
		return
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
	}
}

// Recorder appends approval events for one command. It is constructed with an
// explicit writer id and actor so the core carries no global state; callers
// resolve the machine's writer id with ledgerkit.ResolveWriterID.
type Recorder struct {
	campaignRoot string
	writerID     string
	writer       *ledgerkit.Writer
	actor        ledgerkit.Actor
	action       string
	now          func() time.Time
}

// NewRecorder returns a Recorder for the campaign at campaignRoot writing as
// writerID on behalf of actor.
func NewRecorder(campaignRoot, writerID string, actor ledgerkit.Actor) (*Recorder, error) {
	w, err := ledgerkit.NewWriterIn(campaignRoot, Dir, writerID)
	if err != nil {
		return nil, err
	}
	if actor.Type == "" {
		actor.Type = ledgerkit.ActorUnknown
	}
	return &Recorder{
		campaignRoot: campaignRoot,
		writerID:     writerID,
		writer:       w,
		actor:        actor,
		action:       ledgerkit.NewActionID(),
		now:          time.Now,
	}, nil
}

// Change reports what one SetWaiting call recorded.
type Change struct {
	// Requested is the request that started waiting, nil if none did.
	Requested *Pending
	// Resolved lists the requests that stopped waiting.
	Resolved []Pending
}

// Empty reports whether the call recorded nothing.
func (c Change) Empty() bool { return c.Requested == nil && len(c.Resolved) == 0 }

// SetWaiting records that source is now waiting on want, or on nothing when
// want is nil.
//
// It is the single call a source makes after it has changed its own state. Any
// request from the same source that is no longer the one waiting is resolved
// with outcome and reason, and want is recorded unless it is already pending.
// Calling it again with the same arguments records nothing, so a source can
// call it on every state transition without tracking what it reported before.
//
// The outcome applies to requests being resolved by this call. A source that
// approves step 3 and thereby starts waiting at step 5 passes want for step 5
// with OutcomeApproved: step 3 is recorded as approved and step 5 as requested.
func (r *Recorder) SetWaiting(ctx context.Context, source string, want *Request, outcome Outcome, reason string) (Change, error) {
	var change Change
	if err := ctx.Err(); err != nil {
		return change, err
	}
	if strings.TrimSpace(source) == "" {
		return change, camperrors.New("approvalkit: empty source")
	}
	if want != nil {
		if want.Source != source {
			return change, camperrors.New("approvalkit: request source does not match the source being updated")
		}
		if err := want.validate(); err != nil {
			return change, err
		}
	}

	pending, err := Load(ctx, r.campaignRoot)
	if err != nil {
		return change, err
	}
	resolve, request := Plan(pending, source, want)

	for _, p := range resolve {
		ev := r.resolutionEvent(p, outcome, reason)
		if err := r.writer.Append(ctx, ev); err != nil {
			return change, err
		}
		change.Resolved = append(change.Resolved, p)
	}
	if request {
		ev := r.requestEvent(*want)
		if err := r.writer.Append(ctx, ev); err != nil {
			return change, err
		}
		p := pendingFromEvent(ev, want.Key())
		change.Requested = &p
	}
	if !change.Empty() {
		// Refresh the index now so the next reader takes the fast path.
		if _, err := Load(ctx, r.campaignRoot); err != nil {
			return change, err
		}
	}
	return change, nil
}

func (r *Recorder) envelope(kind ledgerkit.Kind, scope ledgerkit.Scope) *ledgerkit.Event {
	return &ledgerkit.Event{
		V:      ledgerkit.EnvelopeVersion,
		ID:     ledgerkit.NewEventID(),
		TS:     ledgerkit.NowUTC(r.now()),
		Kind:   kind,
		Scope:  scope,
		Action: r.action,
		Actor:  r.actor,
		Source: ledgerkit.SourceCommand,
	}
}

func (r *Recorder) requestEvent(req Request) *ledgerkit.Event {
	ev := r.envelope(KindRequested, req.Scope)
	payload := map[string]any{
		"key":         req.Key(),
		"source":      req.Source,
		"source_kind": req.SourceKind,
		"step":        req.Step,
		"title":       strings.TrimSpace(req.Title),
		"cwd":         filepath.ToSlash(filepath.Clean(req.Cwd)),
		"writer":      r.writerID,
	}
	if name := strings.TrimSpace(req.StepName); name != "" {
		payload["step_name"] = name
	}
	if body := truncateBody(req.Body); body != "" {
		payload["body"] = body
	}
	if req.Doc != "" {
		payload["doc"] = filepath.ToSlash(filepath.Clean(req.Doc))
	}
	ev.Payload = payload
	return ev
}

func (r *Recorder) resolutionEvent(p Pending, outcome Outcome, reason string) *ledgerkit.Event {
	ev := r.envelope(outcome.kind(), p.Scope)
	payload := map[string]any{
		"key":        p.Key,
		"source":     p.Source,
		"request_id": p.EventID,
		"writer":     r.writerID,
	}
	if reason = strings.TrimSpace(reason); reason != "" {
		payload["reason"] = reason
		ev.Why = reason
	}
	ev.Payload = payload
	return ev
}

// truncateBody bounds stored request text without cutting a UTF-8 sequence.
func truncateBody(body string) string {
	body = strings.TrimSpace(body)
	if len(body) <= maxBodyBytes {
		return body
	}
	cut := maxBodyBytes
	for cut > 0 && body[cut]&0xC0 == 0x80 {
		cut--
	}
	return body[:cut] + "\n[truncated]"
}

// ResolveCwd turns a request's campaign-relative cwd into an absolute directory
// a reader may run the source's command in.
//
// It fails unless the path stays inside the campaign root after symlinks are
// resolved and names an existing directory. The ledger is a file any process
// can append to, so a request's cwd is untrusted input: this is the check that
// stops a forged request from pointing a command somewhere else.
func ResolveCwd(campaignRoot, cwd string) (string, error) {
	if !IsCampRelative(cwd) {
		return "", camperrors.New("approvalkit: cwd is not inside the camp: " + cwd)
	}
	root, err := filepath.EvalSymlinks(campaignRoot)
	if err != nil {
		return "", camperrors.Wrap(err, "approvalkit: resolve camp root")
	}
	abs, err := filepath.EvalSymlinks(filepath.Join(root, filepath.FromSlash(cwd)))
	if err != nil {
		return "", camperrors.Wrap(err, "approvalkit: resolve cwd "+cwd)
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil || rel == ".." || strings.HasPrefix(filepath.ToSlash(rel), "../") {
		return "", camperrors.New("approvalkit: cwd resolves outside the camp: " + cwd)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", camperrors.Wrap(err, "approvalkit: stat cwd "+cwd)
	}
	if !info.IsDir() {
		return "", camperrors.New("approvalkit: cwd is not a directory: " + cwd)
	}
	return abs, nil
}
