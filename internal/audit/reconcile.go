package audit

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/pkg/ledgerkit"
)

// DerivedFact is one event derived from a campaign state file, before it is
// checked against the ledger. Backfill emits every fact; reconciliation emits
// only facts the ledger does not already capture.
type DerivedFact struct {
	Kind     ledgerkit.Kind
	Scope    ledgerkit.Scope
	TS       string
	Why      string
	Payload  map[string]any
	Evidence []ledgerkit.Evidence
	// IdentityKey is the stable content identity used to derive the event id and
	// to detect whether the ledger already captures this fact.
	IdentityKey string
}

// DeriveFacts reads the campaign's state files (intent frontmatter and festival
// status histories) and returns the events they imply. It is the shared
// derivation used by both reconciliation and backfill; it is deterministic
// (same disk state yields the same facts in the same order).
func DeriveFacts(ctx context.Context, campaignRoot, campaignID string) ([]DerivedFact, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var facts []DerivedFact
	facts = append(facts, deriveIntentFacts(campaignRoot, campaignID)...)
	festFacts, err := deriveFestivalFacts(ctx, campaignRoot, campaignID)
	if err != nil {
		return nil, err
	}
	facts = append(facts, festFacts...)
	return facts, nil
}

// deriveIntentFacts derives a created fact per intent on disk.
func deriveIntentFacts(campaignRoot, campaignID string) []DerivedFact {
	root := filepath.Join(campaignRoot, ".campaign", "intents")
	var facts []DerivedFact
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Ext(p) != ".md" || strings.HasSuffix(p, "OBEY.md") {
			return nil
		}
		fm := parseFrontmatter(p)
		if fm["id"] == "" {
			return nil
		}
		facts = append(facts, DerivedFact{
			Kind:        ledgerkit.KindCreated,
			Scope:       ledgerkit.Scope{Campaign: campaignID, Intent: fm["id"]},
			TS:          normalizeTS(firstNonEmpty(fm["created_at"], fm["updated_at"])),
			Why:         fm["title"],
			Payload:     map[string]any{"status": fm["status"], "type": fm["type"]},
			IdentityKey: "intent-created:" + fm["id"],
		})
		return nil
	})
	return facts
}

// deriveFestivalFacts derives a transitioned fact per festival status-history
// entry.
func deriveFestivalFacts(ctx context.Context, campaignRoot, campaignID string) ([]DerivedFact, error) {
	festRoot := filepath.Join(campaignRoot, "festivals")
	var facts []DerivedFact
	err := filepath.WalkDir(festRoot, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if d.IsDir() || filepath.Base(p) != "status_history.json" {
			return nil
		}
		fest := festivalID(campaignRoot, p)
		var hist []struct {
			Timestamp  string `json:"timestamp"`
			FromStatus string `json:"from_status"`
			ToStatus   string `json:"to_status"`
		}
		if b, readErr := os.ReadFile(p); readErr == nil {
			_ = json.Unmarshal(b, &hist)
		}
		for i, h := range hist {
			facts = append(facts, DerivedFact{
				Kind:        ledgerkit.KindTransitioned,
				Scope:       ledgerkit.Scope{Campaign: campaignID, Festival: fest},
				TS:          normalizeTS(h.Timestamp),
				Payload:     map[string]any{"from": h.FromStatus, "to": h.ToStatus},
				IdentityKey: fmtKey("fest-transitioned", fest, h.FromStatus, h.ToStatus, itoaSmall(i)),
			})
		}
		return nil
	})
	if err != nil {
		return nil, camperrors.Wrap(err, "audit: walk festival histories")
	}
	return facts, nil
}

// Reconcile derives expected events from state files, diffs them against the
// current ledger, and returns reconciled events for facts the ledger does not
// already capture. It is idempotent: reconciled event ids are content-derived
// (rc_ prefix), so once emitted they are captured and skipped on the next run.
func Reconcile(ctx context.Context, campaignRoot, campaignID string) ([]*ledgerkit.Event, error) {
	facts, err := DeriveFacts(ctx, campaignRoot, campaignID)
	if err != nil {
		return nil, err
	}
	reader, err := ledgerkit.NewReader(campaignRoot)
	if err != nil {
		return nil, err
	}
	events, _, err := reader.Read(ctx)
	if err != nil {
		return nil, err
	}
	captured := capturedIndex(events)

	var reconciled []*ledgerkit.Event
	for _, f := range facts {
		if captured[factCoverageKey(f)] {
			continue // the ledger already captures this fact (live or backfilled)
		}
		reconciled = append(reconciled, &ledgerkit.Event{
			V:       ledgerkit.EnvelopeVersion,
			ID:      ledgerkit.DerivedID("rc", f.IdentityKey),
			TS:      f.TS,
			Kind:    f.Kind,
			Scope:   f.Scope,
			Actor:   ledgerkit.Actor{Type: ledgerkit.ActorUnknown},
			Why:     f.Why,
			Payload: f.Payload,
			Source:  ledgerkit.SourceReconciled,
		})
	}
	return reconciled, nil
}

// capturedIndex builds the set of coverage keys already present in the ledger.
// A created event covers its scope entity; a transitioned event covers its
// scope entity's from/to move.
func capturedIndex(events []*ledgerkit.Event) map[string]bool {
	idx := make(map[string]bool, len(events))
	for _, e := range events {
		switch e.Kind {
		case ledgerkit.KindCreated:
			if e.Scope.Intent != "" {
				idx["intent-created:"+e.Scope.Intent] = true
			}
		case ledgerkit.KindTransitioned:
			if e.Scope.Festival != "" {
				from, _ := e.Payload["from"].(string)
				to, _ := e.Payload["to"].(string)
				// Occurrence-faithful: each from→to edge keeps a sequential
				// index so bouncing festivals (ready→active→ready→active) are
				// not collapsed when any live edge already exists.
				base := fmtKey("fest-transitioned", e.Scope.Festival, from, to)
				i := 0
				for {
					key := fmtKey(base, itoaSmall(i))
					if !idx[key] {
						idx[key] = true
						break
					}
					i++
				}
			}
		}
		// Commit evidence (live or backfilled) covers a commit fact regardless of
		// the event kind, so backfill does not re-attach an already-recorded sha.
		// Shas are normalized to a short prefix so live short shas and backfill
		// full shas for the same commit match.
		for _, ev := range e.Evidence {
			if ev.Type == ledgerkit.EvidenceCommit && ev.SHA != "" {
				idx["commit:"+ev.Repo+"@"+normSHA(ev.SHA)] = true
			}
		}
	}
	return idx
}

// factCoverageKey is the key a fact would occupy in capturedIndex, so a derived
// fact can be matched against events regardless of their id scheme.
func factCoverageKey(f DerivedFact) string {
	switch f.Kind {
	case ledgerkit.KindCreated:
		if f.Scope.Intent != "" {
			return "intent-created:" + f.Scope.Intent
		}
	case ledgerkit.KindTransitioned:
		// Prefer the occurrence-indexed IdentityKey (set by deriveFestivalFacts).
		if f.IdentityKey != "" {
			return f.IdentityKey
		}
		if f.Scope.Festival != "" {
			from, _ := f.Payload["from"].(string)
			to, _ := f.Payload["to"].(string)
			return fmtKey("fest-transitioned", f.Scope.Festival, from, to, "0")
		}
	case ledgerkit.KindEvidenceAttached:
		if len(f.Evidence) > 0 && f.Evidence[0].Type == ledgerkit.EvidenceCommit {
			return "commit:" + f.Evidence[0].Repo + "@" + normSHA(f.Evidence[0].SHA)
		}
	}
	return f.IdentityKey
}

// normSHA normalizes a commit sha to a short prefix so a short sha stored by
// live capture and a full sha derived by backfill for the same commit compare
// equal in coverage keys.
func normSHA(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

// ---- small helpers ----

func parseFrontmatter(path string) map[string]string {
	out := map[string]string{}
	f, err := os.Open(path)
	if err != nil {
		return out
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	first := true
	inFM := false
	for sc.Scan() {
		line := sc.Text()
		if first {
			first = false
			if strings.TrimSpace(line) == "---" {
				inFM = true
				continue
			}
			return out
		}
		if !inFM {
			break
		}
		if strings.TrimSpace(line) == "---" {
			break
		}
		k, v, ok := strings.Cut(line, ":")
		if ok {
			out[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	return out
}

func normalizeTS(s string) string {
	if s == "" {
		return "1970-01-01T00:00:00Z"
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05Z07:00"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC().Format(time.RFC3339Nano)
		}
	}
	return s
}

func festivalID(campaignRoot, historyPath string) string {
	rel, _ := filepath.Rel(campaignRoot, historyPath)
	parts := strings.Split(rel, string(os.PathSeparator))
	for i, seg := range parts {
		if seg == ".fest" && i > 0 {
			return canonicalFestivalID(parts[i-1])
		}
	}
	return rel
}

// canonicalFestivalID parses the trailing id token (e.g. CA0002) from a festival
// directory name, per D001's canonical-festival-id rule; falls back to the dir
// name when no id token is present.
func canonicalFestivalID(dirName string) string {
	if i := strings.LastIndex(dirName, "-"); i >= 0 && i < len(dirName)-1 {
		tail := dirName[i+1:]
		if looksLikeFestID(tail) {
			return tail
		}
	}
	return dirName
}

func looksLikeFestID(s string) bool {
	if len(s) < 3 {
		return false
	}
	hasAlpha, hasDigit := false, false
	for _, r := range s {
		switch {
		case r >= 'A' && r <= 'Z':
			hasAlpha = true
		case r >= '0' && r <= '9':
			hasDigit = true
		default:
			return false
		}
	}
	return hasAlpha && hasDigit
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func fmtKey(parts ...string) string { return strings.Join(parts, ":") }

func itoaSmall(n int) string {
	if n == 0 {
		return "0"
	}
	var b [12]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
