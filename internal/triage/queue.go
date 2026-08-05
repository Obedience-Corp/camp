package triage

import (
	"context"
	"errors"
	"io/fs"
	"os"
)

// QueueRole is the kind of work a queued row is waiting for.
type QueueRole string

const (
	// QueueRoleEvidence means the row has no evidence record yet: someone has
	// to read it.
	QueueRoleEvidence QueueRole = "evidence"
	// QueueRoleSynthesis means evidence exists but no proposal does: someone
	// has to turn the reading into a disposition.
	QueueRoleSynthesis QueueRole = "synthesis"
)

// QueueRoles returns the role vocabulary.
func QueueRoles() []string {
	return []string{string(QueueRoleEvidence), string(QueueRoleSynthesis)}
}

// QueueItem is one row awaiting work, with everything a driver needs to do it
// without asking camp a second question.
type QueueItem struct {
	StableID     string    `json:"stable_id"`
	Ref          string    `json:"ref,omitempty"`
	Title        string    `json:"title"`
	Type         string    `json:"type"`
	RelativePath string    `json:"relative_path"`
	Batch        int       `json:"batch"`
	Role         QueueRole `json:"role"`
	Policy       RowPolicy `json:"policy"`
}

// Queue is the dispatch surface `camp triage queue` reports.
//
// It is the seam doc 08 defines: camp says what needs judging and under what
// policy, a driver does the judging however it likes, and camp validates what
// comes back. Nothing here calls a model.
type Queue struct {
	RunID string `json:"run_id"`
	Phase Phase  `json:"phase"`
	// EvidenceSchemaVersion tells a driver which record format to produce.
	EvidenceSchemaVersion string `json:"evidence_schema_version"`
	// Routing is the profile's advisory block, passed through verbatim.
	Routing ProfileRouting `json:"routing"`
	Items   []QueueItem    `json:"items"`
	Counts  QueueCounts    `json:"counts"`
}

// QueueCounts tallies the whole run, not just the filtered items, so a driver
// can report progress without a second call.
type QueueCounts struct {
	Evidence  int `json:"evidence"`
	Synthesis int `json:"synthesis"`
	Done      int `json:"done"`
	Total     int `json:"total"`
}

// BuildQueue derives the work queue from run data.
//
// role filters the returned items; the counts always describe the whole run.
// An empty role returns everything still awaiting work.
func BuildQueue(ctx context.Context, store *Store, runID string, role QueueRole) (*Queue, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if role != "" {
		if v := checkEnum("role", string(role), QueueRoles()); len(v) > 0 {
			return nil, newValidationError("queue role", v)
		}
	}

	run, err := store.OpenRun(ctx, runID)
	if err != nil {
		return nil, err
	}
	verdicts, err := store.Verdicts(ctx, runID)
	if err != nil {
		return nil, err
	}

	queue := &Queue{
		RunID:                 run.ID,
		Phase:                 run.State.Phase,
		EvidenceSchemaVersion: SchemaVersion,
		Routing:               run.Manifest.Profile.Resolved.Routing,
		Items:                 []QueueItem{},
	}

	for _, row := range run.Manifest.Rows {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		hasEvidence, err := store.HasEvidence(ctx, runID, row.StableID)
		if err != nil {
			return nil, err
		}
		rowRole, waiting := queueRoleFor(row, verdicts[row.StableID], hasEvidence)
		if !waiting {
			queue.Counts.Done++
			queue.Counts.Total++
			continue
		}

		queue.Counts.Total++
		switch rowRole {
		case QueueRoleEvidence:
			queue.Counts.Evidence++
		case QueueRoleSynthesis:
			queue.Counts.Synthesis++
		}
		if role != "" && rowRole != role {
			continue
		}
		queue.Items = append(queue.Items, QueueItem{
			StableID:     row.StableID,
			Ref:          row.Ref,
			Title:        row.Title,
			Type:         row.Type,
			RelativePath: row.RelativePath,
			Batch:        row.Batch,
			Role:         rowRole,
			Policy:       row.Policy,
		})
	}
	return queue, nil
}

// queueRoleFor decides what a row is waiting for, and whether it is waiting at
// all.
//
// A carried row is not queued: its verdict came forward from a base run
// precisely so nobody re-reads it. A row that already holds a proposal is not
// queued either — the next thing it needs is a human approving it, which is
// the review surface's business rather than a driver's.
func queueRoleFor(row ManifestRow, verdict RowVerdict, hasEvidence bool) (QueueRole, bool) {
	if row.CarriedFrom != nil && verdict.State == VerdictNone {
		return "", false
	}
	switch verdict.State {
	case VerdictProposed, VerdictApproved:
		return "", false
	}
	if !hasEvidence {
		return QueueRoleEvidence, true
	}
	return QueueRoleSynthesis, true
}

// HasEvidence reports whether a row holds an evidence record, without paying
// to parse it.
func (s *Store) HasEvidence(ctx context.Context, runID, stableID string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	_, err := os.Stat(s.EvidencePath(runID, stableID))
	if err == nil {
		return true, nil
	}
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	return false, err
}
