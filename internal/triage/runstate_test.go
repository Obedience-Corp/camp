package triage

import (
	"testing"
	"time"

	"github.com/Obedience-Corp/camp/internal/dungeon/scaffold"
	"github.com/Obedience-Corp/camp/internal/workitem"
	"github.com/stretchr/testify/assert"
)

// --- run state machine -------------------------------------------------

// TestRunStateRejectsIllegalHistory: a run whose history does not explain its
// phase is not resumable, which defeats the reason the history exists.
func TestRunStateRejectsIllegalHistory(t *testing.T) {
	tests := []struct {
		name       string
		mutate     func(*RunState)
		wantFields []string
	}{
		{
			name:       "no history at all",
			mutate:     func(s *RunState) { s.PhaseHistory = nil },
			wantFields: []string{"phase_history"},
		},
		{
			name: "history does not start at created",
			mutate: func(s *RunState) {
				s.PhaseHistory = s.PhaseHistory[1:]
				s.Phase = PhaseReviewing
			},
			wantFields: []string{"phase_history[0].phase"},
		},
		{
			name: "skips a phase",
			mutate: func(s *RunState) {
				s.PhaseHistory = []PhaseTransition{
					{Phase: PhaseCreated, At: testAt},
					{Phase: PhaseApplying, At: testAt.Add(time.Second)},
				}
				s.Phase = PhaseApplying
			},
			wantFields: []string{"phase_history[1].phase"},
		},
		{
			name: "phase disagrees with the last transition",
			mutate: func(s *RunState) {
				s.Phase = PhaseApplying
			},
			wantFields: []string{"phase"},
		},
		{
			name: "transitions go backwards in time",
			mutate: func(s *RunState) {
				s.PhaseHistory[3].At = testAt.Add(-time.Hour)
			},
			wantFields: []string{"phase_history[3].at"},
		},
		{
			name: "abandon reason on a live run",
			mutate: func(s *RunState) {
				s.AbandonReason = ptr("changed my mind")
			},
			wantFields: []string{"abandon_reason"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			state := validRunState()
			tc.mutate(state)

			fields := fieldsOf(state.Validate())

			for _, want := range tc.wantFields {
				assert.Contains(t, fields, want)
			}
		})
	}
}

// TestRunStateAcceptsLegalWalks covers the two edges that are not simple
// forward steps, plus abandonment from mid-run.
func TestRunStateAcceptsLegalWalks(t *testing.T) {
	tests := []struct {
		name    string
		phase   Phase
		history []Phase
	}{
		{
			name:    "apply returns to review for the next batch",
			phase:   PhaseReviewing,
			history: []Phase{PhaseCreated, PhaseSnapshotted, PhaseJudging, PhaseReviewing, PhaseApplying, PhaseReviewing},
		},
		{
			name:    "refresh sends stale rows back to judging",
			phase:   PhaseJudging,
			history: []Phase{PhaseCreated, PhaseSnapshotted, PhaseJudging, PhaseReviewing, PhaseJudging},
		},
		{
			name:    "abandoned mid-run",
			phase:   PhaseAbandoned,
			history: []Phase{PhaseCreated, PhaseSnapshotted, PhaseAbandoned},
		},
		{
			name:    "verified",
			phase:   PhaseVerified,
			history: []Phase{PhaseCreated, PhaseSnapshotted, PhaseJudging, PhaseReviewing, PhaseApplying, PhaseVerified},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			state := &RunState{
				SchemaVersion: SchemaVersion,
				RunID:         "run-1",
				Phase:         tc.phase,
				PhaseHistory:  historyOf(tc.history),
			}
			assert.Empty(t, state.Validate())
		})
	}
}

// TestTerminalPhasesHaveNoExit pins the closed end of the machine.
func TestTerminalPhasesHaveNoExit(t *testing.T) {
	assert.True(t, PhaseVerified.Terminal())
	assert.True(t, PhaseAbandoned.Terminal())
	assert.Empty(t, PhaseVerified.NextPhases())
	assert.Empty(t, PhaseAbandoned.NextPhases())
	assert.False(t, PhaseReviewing.Terminal())
}

// TestEveryPhaseCanBeAbandoned: abandon is reachable from any live phase, so
// a stuck run is always closable without hand-editing state.
func TestEveryPhaseCanBeAbandoned(t *testing.T) {
	for _, name := range Phases() {
		phase := Phase(name)
		if phase.Terminal() {
			continue
		}
		assert.True(t, phase.CanTransitionTo(PhaseAbandoned), "phase %s", phase)
	}
}

// --- decision events ---------------------------------------------------

// TestDecisionEventRejects covers the rules that keep the verdict stream
// readable: every event names its row, its actor, and a real action.
func TestDecisionEventRejects(t *testing.T) {
	tests := []struct {
		name       string
		mutate     func(*DecisionEvent)
		wantFields []string
	}{
		{
			name:       "unknown event kind",
			mutate:     func(e *DecisionEvent) { e.Event = "maybe" },
			wantFields: []string{"event"},
		},
		{
			name:       "no row",
			mutate:     func(e *DecisionEvent) { e.StableID = "" },
			wantFields: []string{"stable_id"},
		},
		{
			name:       "no actor",
			mutate:     func(e *DecisionEvent) { e.Actor = "" },
			wantFields: []string{"actor"},
		},
		{
			name:       "no timestamp",
			mutate:     func(e *DecisionEvent) { e.At = time.Time{} },
			wantFields: []string{"at"},
		},
		{
			name:       "disposition with no canonical action",
			mutate:     func(e *DecisionEvent) { e.CanonicalAction = "" },
			wantFields: []string{"canonical_action"},
		},
		{
			name:       "canonical action outside camp's vocabulary",
			mutate:     func(e *DecisionEvent) { e.CanonicalAction = "delete/everything" },
			wantFields: []string{"canonical_action"},
		},
		{
			name:       "approval with no disposition",
			mutate:     func(e *DecisionEvent) { e.Disposition = "" },
			wantFields: []string{"disposition"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			event := validDecision()
			tc.mutate(event)

			fields := fieldsOf(event.Validate())

			for _, want := range tc.wantFields {
				assert.Contains(t, fields, want)
			}
		})
	}
}

// TestRetiringEventsNeedNoVerdict: stale and superseded end a verdict rather
// than expressing one, so they carry no disposition.
func TestRetiringEventsNeedNoVerdict(t *testing.T) {
	for _, kind := range []DecisionEventKind{DecisionStale, DecisionSuperseded} {
		t.Run(string(kind), func(t *testing.T) {
			event := &DecisionEvent{
				SchemaVersion: SchemaVersion,
				Event:         kind,
				StableID:      "row",
				Actor:         "lancekrogers",
				At:            testAt,
			}
			assert.Empty(t, event.Validate())
		})
	}
}

// TestCanonicalActionParts checks the family/target split callers switch on.
func TestCanonicalActionParts(t *testing.T) {
	tests := []struct {
		action     CanonicalAction
		family     string
		target     string
		isTerminal bool
	}{
		{"none", "none", "", false},
		{"split", "split", "", true},
		{"attention/parked", ActionFamilyAttention, "parked", false},
		{"rail/ready", ActionFamilyRail, "ready", false},
		{"dungeon/completed", ActionFamilyDungeon, "completed", true},
	}

	for _, tc := range tests {
		t.Run(string(tc.action), func(t *testing.T) {
			assert.Equal(t, tc.family, tc.action.Family())
			assert.Equal(t, tc.target, tc.action.Target())
			assert.Equal(t, tc.isTerminal, tc.action.Terminal())
		})
	}
}

// TestCanonicalActionsMatchCampVocabulary is a drift guard, not a tautology:
// triage's action list is derived from camp's own stage and dungeon
// vocabularies, so adding a stage or a dungeon status to camp without
// deciding what triage does with it fails here.
func TestCanonicalActionsMatchCampVocabulary(t *testing.T) {
	actions := CanonicalActions()

	for _, stage := range workitem.AttentionStages() {
		assert.Contains(t, actions, ActionFamilyAttention+"/"+stage)
	}
	for _, status := range scaffold.StandardStatuses {
		assert.Contains(t, actions, ActionFamilyDungeon+"/"+status)
		assert.True(t, CanonicalAction(ActionFamilyDungeon+"/"+status).Terminal())
	}
	assert.Contains(t, actions, string(ActionNone))
	assert.Contains(t, actions, string(ActionSplit))
	assert.Len(t, actions,
		2+len(workitem.AttentionStages())+len(railTargets)+len(scaffold.StandardStatuses))
}

func historyOf(phases []Phase) []PhaseTransition {
	out := make([]PhaseTransition, len(phases))
	for i, phase := range phases {
		out[i] = PhaseTransition{Phase: phase, At: testAt.Add(time.Duration(i) * time.Second)}
	}
	return out
}
