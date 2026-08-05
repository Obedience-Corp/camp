package triage

import "time"

// Phase is a run's position in the fixed triage state machine:
//
//	created -> snapshotted -> judging -> reviewing -> applying -> verified
//
// The graph is product, not configuration (D1): resumability and audit
// guarantees are properties of a known machine. Profiles change what happens
// inside a phase, never the phase order. `abandoned` is reachable from any
// phase and keeps the run's state.
type Phase string

const (
	PhaseCreated     Phase = "created"
	PhaseSnapshotted Phase = "snapshotted"
	PhaseJudging     Phase = "judging"
	PhaseReviewing   Phase = "reviewing"
	PhaseApplying    Phase = "applying"
	PhaseVerified    Phase = "verified"
	PhaseAbandoned   Phase = "abandoned"
)

// Phases returns the phase vocabulary in machine order.
func Phases() []string {
	return []string{
		string(PhaseCreated),
		string(PhaseSnapshotted),
		string(PhaseJudging),
		string(PhaseReviewing),
		string(PhaseApplying),
		string(PhaseVerified),
		string(PhaseAbandoned),
	}
}

// phaseTransitions is the complete edge set of the machine.
//
// Two edges are not simple forward steps and are deliberate:
//   - applying -> reviewing, because partial approval is normal; apply runs
//     as often as rows are approved and returns to the approve loop.
//   - reviewing -> judging, because refresh downgrades verdicts to stale and
//     re-queues those rows for judgment.
var phaseTransitions = map[Phase][]Phase{
	PhaseCreated:     {PhaseSnapshotted, PhaseAbandoned},
	PhaseSnapshotted: {PhaseJudging, PhaseAbandoned},
	PhaseJudging:     {PhaseReviewing, PhaseAbandoned},
	PhaseReviewing:   {PhaseJudging, PhaseApplying, PhaseAbandoned},
	PhaseApplying:    {PhaseReviewing, PhaseVerified, PhaseAbandoned},
	PhaseVerified:    {},
	PhaseAbandoned:   {},
}

// NextPhases returns the phases reachable from p.
func (p Phase) NextPhases() []Phase {
	next := phaseTransitions[p]
	out := make([]Phase, len(next))
	copy(out, next)
	return out
}

// CanTransitionTo reports whether p -> next is a legal edge.
func (p Phase) CanTransitionTo(next Phase) bool {
	for _, allowed := range phaseTransitions[p] {
		if allowed == next {
			return true
		}
	}
	return false
}

// Terminal reports whether p ends the run.
func (p Phase) Terminal() bool {
	return p == PhaseVerified || p == PhaseAbandoned
}

// RunState is `run.json`: where a run is and how it got there.
//
// The history is the resume anchor. Every transition is recorded before the
// phase's side effects begin, so a killed process re-opens at the phase whose
// work may be incomplete and redoes it rather than skipping it.
type RunState struct {
	SchemaVersion string            `json:"schema_version"`
	RunID         string            `json:"run_id"`
	Phase         Phase             `json:"phase"`
	PhaseHistory  []PhaseTransition `json:"phase_history"`
	// AbandonReason is set only on an abandoned run, and only when the
	// operator supplied one.
	AbandonReason *string `json:"abandon_reason"`
}

// PhaseTransition is one recorded entry in a run's phase history.
type PhaseTransition struct {
	Phase Phase     `json:"phase"`
	At    time.Time `json:"at"`
	Note  string    `json:"note,omitempty"`
}

// Normalize implements Document.
func (s *RunState) Normalize() {
	s.SchemaVersion = SchemaVersion
	if s.PhaseHistory == nil {
		s.PhaseHistory = []PhaseTransition{}
	}
	for i := range s.PhaseHistory {
		normalizeTime(&s.PhaseHistory[i].At)
	}
}

func (s *RunState) kind() string    { return "run state" }
func (s *RunState) version() string { return s.SchemaVersion }

// Validate implements Document. Beyond field rules it checks that the recorded
// history is a legal walk of the machine ending at the current phase — a run
// whose history does not explain its phase is not resumable, which is the
// whole point of recording it.
func (s *RunState) Validate() []Violation {
	var out []Violation
	out = append(out, checkRequired("run_id", s.RunID)...)
	out = append(out, checkEnum("phase", string(s.Phase), Phases())...)

	if len(s.PhaseHistory) == 0 {
		out = append(out, Violation{
			Field:   "phase_history",
			Message: "is required: a run records how it reached its phase",
		})
	} else {
		if first := s.PhaseHistory[0].Phase; first != PhaseCreated {
			out = append(out, Violation{
				Field:   "phase_history[0].phase",
				Message: "must be " + quote(string(PhaseCreated)) + ", got " + quote(string(first)),
			})
		}
		for i := range s.PhaseHistory {
			path := indexPath("phase_history", i)
			entry := s.PhaseHistory[i]
			out = append(out, checkEnum(joinPath(path, "phase"), string(entry.Phase), Phases())...)
			out = append(out, checkTimeSet(joinPath(path, "at"), entry.At)...)
			if i == 0 {
				continue
			}
			prev := s.PhaseHistory[i-1]
			if !prev.Phase.CanTransitionTo(entry.Phase) {
				out = append(out, Violation{
					Field: joinPath(path, "phase"),
					Message: "illegal transition from " + quote(string(prev.Phase)) +
						" to " + quote(string(entry.Phase)),
					Allowed: phaseNames(prev.Phase.NextPhases()),
				})
			}
			if entry.At.Before(prev.At) {
				out = append(out, Violation{
					Field:   joinPath(path, "at"),
					Message: "is before the previous transition",
				})
			}
		}
		if last := s.PhaseHistory[len(s.PhaseHistory)-1].Phase; last != s.Phase && s.Phase != "" {
			out = append(out, Violation{
				Field: "phase",
				Message: "does not match the last recorded transition " +
					quote(string(last)),
			})
		}
	}

	if s.AbandonReason != nil && s.Phase != PhaseAbandoned {
		out = append(out, Violation{
			Field:   "abandon_reason",
			Message: "is only valid on an abandoned run",
		})
	}
	return out
}

// phaseNames renders phases as strings for a violation's allowed set.
func phaseNames(phases []Phase) []string {
	out := make([]string, len(phases))
	for i, p := range phases {
		out[i] = string(p)
	}
	return out
}

// CanTransition reports whether a run may move from one phase to another.
func CanTransition(from, to Phase) bool {
	for _, next := range phaseTransitions[from] {
		if next == to {
			return true
		}
	}
	return false
}
