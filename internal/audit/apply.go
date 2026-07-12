package audit

import (
	"context"
	"strings"
	"time"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/pkg/ledgerkit"
)

// Apply appends already-built events (reconciled or repaired) to the campaign
// ledger. Ids are set by the caller (content-derived for reconciled events), so
// re-applying is idempotent at the read layer: duplicate ids dedupe on read.
// It returns the number of events written.
func Apply(ctx context.Context, campaignRoot string, events []*ledgerkit.Event) (int, error) {
	if len(events) == 0 {
		return 0, nil
	}
	writerID, err := ledgerkit.ResolveWriterID(ctx)
	if err != nil {
		return 0, camperrors.Wrap(err, "audit: resolve writer id")
	}
	w, err := ledgerkit.NewWriter(campaignRoot, writerID)
	if err != nil {
		return 0, camperrors.Wrap(err, "audit: init writer")
	}
	written := 0
	for _, ev := range events {
		if err := ctx.Err(); err != nil {
			return written, err
		}
		if err := w.Append(ctx, ev); err != nil {
			return written, camperrors.Wrapf(err, "audit: append %s", ev.ID)
		}
		written++
	}
	return written, nil
}

// RepairInput describes an opt-in repair: attributing a commit (or other
// evidence) to a workitem or festival after the fact.
type RepairInput struct {
	CampaignID string
	Repo       string // evidence repo label (campaign-relative)
	SHA        string
	Workitem   string
	Festival   string
	Why        string
	Actor      ledgerkit.Actor
}

// BuildRepair constructs a repaired event attributing the evidence to a
// workitem/festival. It never rewrites git history; it only appends an
// attribution event (D004). The id is content-derived so re-running the same
// repair converges (callers must skip when the id is already on disk).
func BuildRepair(in RepairInput) (*ledgerkit.Event, error) {
	if in.SHA == "" {
		return nil, camperrors.NewValidation("sha", "a commit sha is required to repair", nil)
	}
	if in.Workitem == "" && in.Festival == "" {
		return nil, camperrors.NewValidation("scope", "repair needs a --workitem or --festival to attribute the commit to", nil)
	}
	if strings.TrimSpace(in.Why) == "" {
		return nil, camperrors.NewValidation("why", "a non-empty --why reason is required for repair", nil)
	}
	scope := ledgerkit.Scope{Campaign: in.CampaignID, Workitem: in.Workitem, Festival: in.Festival}
	evidence := []ledgerkit.Evidence{{Type: ledgerkit.EvidenceCommit, Repo: in.Repo, SHA: in.SHA}}
	return &ledgerkit.Event{
		V:        ledgerkit.EnvelopeVersion,
		ID:       ledgerkit.DerivedID("rp", "repair", in.Repo, in.SHA, in.Workitem, in.Festival),
		TS:       ledgerkit.NowUTC(time.Now()),
		Kind:     ledgerkit.KindRepaired,
		Scope:    scope,
		Actor:    in.Actor,
		Why:      strings.TrimSpace(in.Why),
		Evidence: evidence,
		Source:   ledgerkit.SourceReconciled,
	}, nil
}

// EventIDPresent reports whether the campaign ledger already holds an event
// with the given id (used so repair skips instead of re-appending).
func EventIDPresent(ctx context.Context, campaignRoot, eventID string) (bool, error) {
	if eventID == "" {
		return false, nil
	}
	reader, err := ledgerkit.NewReader(campaignRoot)
	if err != nil {
		return false, err
	}
	events, _, err := reader.Query(ctx, ledgerkit.Filter{})
	if err != nil {
		return false, err
	}
	for _, ev := range events {
		if ev.ID == eventID {
			return true, nil
		}
	}
	return false, nil
}
