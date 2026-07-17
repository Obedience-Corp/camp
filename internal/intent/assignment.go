package intent

import (
	"context"
	"strings"
	"time"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// ClaimOptions configures a Claim call.
type ClaimOptions struct {
	// Agent is the agent/session name claiming the intent. Required.
	Agent string

	// Refs are additional work references (PR URL, branch, or festival path)
	// to merge into the intent's existing work_ref list.
	Refs []string
}

// Claim assigns an intent to an agent, stamping assigned_to and assigned_at
// and merging any Refs into work_ref. Calling Claim again on an already
// claimed intent re-stamps assigned_at and merges in new refs without
// dropping ones already recorded — this is the expected way an agent records
// a PR URL once one is opened, after an initial claim at the start of work.
func (s *IntentService) Claim(ctx context.Context, id string, opts ClaimOptions) (*Intent, error) {
	if err := ctx.Err(); err != nil {
		return nil, camperrors.Wrap(err, "context cancelled")
	}

	agent := strings.TrimSpace(opts.Agent)
	if agent == "" {
		return nil, camperrors.Wrap(camperrors.ErrInvalidInput, "agent name is required to claim an intent")
	}

	i, err := s.Find(ctx, id)
	if err != nil {
		return nil, err
	}

	i.AssignedTo = agent
	i.AssignedAt = time.Now()
	i.WorkRef = mergeWorkRefs(i.WorkRef, opts.Refs)
	i.UpdatedAt = time.Now()

	if err := s.Save(ctx, i); err != nil {
		return nil, camperrors.Wrap(err, "saving claimed intent")
	}

	s.emitClaimed(ctx, i)
	return i, nil
}

// Release clears an intent's assignment (assigned_to, assigned_at), returning
// it to the unclaimed pool. Recorded work_ref entries are left in place so a
// later camp intent sync can still resolve any tracked PR.
func (s *IntentService) Release(ctx context.Context, id string) (*Intent, error) {
	if err := ctx.Err(); err != nil {
		return nil, camperrors.Wrap(err, "context cancelled")
	}

	i, err := s.Find(ctx, id)
	if err != nil {
		return nil, err
	}

	previousAssignee := i.AssignedTo
	i.AssignedTo = ""
	i.AssignedAt = time.Time{}
	i.UpdatedAt = time.Now()

	if err := s.Save(ctx, i); err != nil {
		return nil, camperrors.Wrap(err, "saving released intent")
	}

	s.emitReleased(ctx, i, previousAssignee)
	return i, nil
}

// mergeWorkRefs unions existing and add, preserving order and dropping blanks
// and duplicates. existing entries are kept first so the earliest-recorded
// ref (typically the working branch) stays ahead of later additions (e.g. a
// PR URL recorded once one opens).
func mergeWorkRefs(existing, add []string) []string {
	seen := make(map[string]struct{}, len(existing)+len(add))
	out := make([]string, 0, len(existing)+len(add))
	for _, list := range [][]string{existing, add} {
		for _, ref := range list {
			ref = strings.TrimSpace(ref)
			if ref == "" {
				continue
			}
			if _, ok := seen[ref]; ok {
				continue
			}
			seen[ref] = struct{}{}
			out = append(out, ref)
		}
	}
	return out
}
