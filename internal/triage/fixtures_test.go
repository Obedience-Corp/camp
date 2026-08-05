package triage

import "time"

// testAt is the single fixed timestamp every fixture uses. Nothing in these
// schemas may depend on wall-clock time, so the tests do not read one.
var testAt = time.Date(2026, 8, 10, 14, 0, 0, 0, time.UTC)

func ptr[T any](v T) *T { return &v }

func validManifest() *Manifest {
	return &Manifest{
		SchemaVersion: SchemaVersion,
		RunID:         "run-20260810T140000Z",
		Mode:          RunModeIncremental,
		Profile: ManifestProfile{
			Name:     ProfileNameDefault,
			Resolved: DefaultProfile(),
		},
		BaseRunID: ptr("run-20260803T234110Z"),
		CreatedAt: testAt,
		Rows: []ManifestRow{
			{
				StableID:       "design-festival-hub-control-plane-2026-08-04",
				Ref:            "WI-fa0c2a",
				Key:            "design:workflow/design/festival-hub-control-plane",
				Type:           "design",
				Title:          "Festival hub control plane",
				RelativePath:   "workflow/design/festival-hub-control-plane",
				LifecycleStage: "active",
				AttentionStage: "active",
				Batch:          2,
				Policy: RowPolicy{
					Evidence:    EvidenceDepthDeep,
					RoutingTier: RoutingTierDefault,
				},
			},
			{
				StableID:       "legacy-design-no-marker",
				Key:            "design:workflow/design/legacy",
				Type:           "design",
				Title:          "Legacy design",
				RelativePath:   "workflow/design/legacy",
				LifecycleStage: "none",
				Batch:          1,
				Policy: RowPolicy{
					Evidence:    EvidenceDepthMetadata,
					RoutingTier: RoutingTierCheap,
				},
				CarriedFrom: ptr("run-20260803T234110Z"),
				IdentityException: &IdentityException{
					Reason:    "marker repair refused by policy",
					Path:      "workflow/design/legacy",
					GrantedBy: "lancekrogers",
					GrantedAt: testAt,
				},
			},
		},
	}
}

func validRunState() *RunState {
	return &RunState{
		SchemaVersion: SchemaVersion,
		RunID:         "run-20260810T140000Z",
		Phase:         PhaseReviewing,
		PhaseHistory: []PhaseTransition{
			{Phase: PhaseCreated, At: testAt},
			{Phase: PhaseSnapshotted, At: testAt.Add(time.Second)},
			{Phase: PhaseJudging, At: testAt.Add(2 * time.Second)},
			{Phase: PhaseReviewing, At: testAt.Add(3 * time.Second), Note: "batch 1 ready"},
		},
	}
}

func validEvidence() *EvidenceRecord {
	return &EvidenceRecord{
		SchemaVersion:    SchemaVersion,
		StableID:         "design-festival-hub-control-plane-2026-08-04",
		OriginalGoal:     "Ship the hub control plane",
		Delivered:        []string{"launchpad entries"},
		Missing:          []string{"per-entry modes"},
		StaleAssumptions: []string{"installer ships first"},
		Related:          []string{"WI-a1b2c3", "festival:CI0009", "pr:obey#239"},
		OpenDecisions:    []string{"tutorial surface"},
		Confidence:       ConfidenceMedium,
		Anchors: []Anchor{
			{Kind: AnchorKindPR, Repo: "Obedience-Corp/obey", Number: 239, Observed: "merged", SHA: "abc123"},
			{Kind: AnchorKindPath, Path: "projects/camp/internal/triage", Hash: PathHashPrefix + "deadbeef"},
			{Kind: AnchorKindFestival, ID: "CI0009", Observed: "active"},
			{Kind: AnchorKindWorkitem, StableID: "other-item", ObservedStage: "active"},
		},
		ProducedBy: ProducedBy{
			Role:    EvidenceRoleEvidence,
			Runtime: "claude-code",
			At:      testAt,
		},
	}
}

func validDecision() *DecisionEvent {
	return &DecisionEvent{
		SchemaVersion:   SchemaVersion,
		Event:           DecisionApproved,
		StableID:        "design-festival-hub-control-plane-2026-08-04",
		Disposition:     "completed",
		CanonicalAction: "dungeon/completed",
		RationaleRef:    "evidence/design-festival-hub-control-plane-2026-08-04.json",
		Actor:           "lancekrogers",
		At:              testAt,
		Note:            "delivered in PR 239",
	}
}

func validPlan() *ApplyPlan {
	return &ApplyPlan{
		SchemaVersion: SchemaVersion,
		RunID:         "run-20260810T140000Z",
		CreatedAt:     testAt,
		Entries: []ApplyPlanEntry{
			{
				StableID:  "design-umbrella",
				VerdictAt: testAt,
				Commands: []PlanCommand{
					{Argv: []string{"camp", "workitem", "split", "design-umbrella", "--into", "part-a"}, Kind: CommandKindSplit},
					{Argv: []string{"camp", "workitem", "promote", "design-umbrella", "--target", "completed"}, Kind: CommandKindDungeon},
				},
				Preconditions: []Precondition{
					{Kind: PreconditionSuccessorsExist, IDs: []string{"part-a"}},
					{Kind: PreconditionRowFresh},
				},
				// A real restore. `camp flow move` does not exist; modelling
				// it here is what let the same mistake sit unnoticed in the
				// production mapping.
				Undo: []string{"camp move workflow/design/.dungeon/completed/2026-08-10/design-umbrella workflow/design/design-umbrella"},
			},
		},
	}
}

func validReceipt() *Receipt {
	return &Receipt{
		SchemaVersion: SchemaVersion,
		StableID:      "design-umbrella",
		Argv:          []string{"camp", "workitem", "promote", "design-umbrella", "--target", "completed"},
		Kind:          CommandKindDungeon,
		StartedAt:     testAt,
		FinishedAt:    testAt.Add(2 * time.Second),
		Result:        ReceiptApplied,
		Undo:          "camp move workflow/design/.dungeon/completed/2026-08-10/design-umbrella workflow/design/design-umbrella",
		Commit:        "0123456789abcdef",
	}
}

func validVerification() *VerificationReport {
	return &VerificationReport{
		SchemaVersion: SchemaVersion,
		RunID:         "run-20260810T140000Z",
		CheckedAt:     testAt,
		Rows: []VerificationRow{
			{
				StableID:        "design-umbrella",
				ExpectedPath:    ".dungeon/completed/2026-08-10/design-umbrella",
				ExpectedStage:   "none",
				DiscoveredPath:  ".dungeon/completed/2026-08-10/design-umbrella",
				DiscoveredStage: "none",
				Result:          VerificationMatch,
			},
			{
				StableID:       "design-other",
				ExpectedPath:   ".dungeon/archived/2026-08-10/design-other",
				DiscoveredPath: "workflow/design/design-other",
				Result:         VerificationMismatch,
				Explanation:    "moved back by hand after apply",
			},
		},
		Totals: VerificationTotals{Checked: 2, Matched: 1, Mismatched: 1},
	}
}

// allValidDocuments returns one fresh valid instance of every format, paired
// with an empty instance of the same type to decode into.
func allValidDocuments() []struct {
	name  string
	doc   Document
	empty func() Document
	jsonl bool
} {
	return []struct {
		name  string
		doc   Document
		empty func() Document
		jsonl bool
	}{
		{"manifest", validManifest(), func() Document { return &Manifest{} }, false},
		{"run state", validRunState(), func() Document { return &RunState{} }, false},
		{"evidence record", validEvidence(), func() Document { return &EvidenceRecord{} }, false},
		{"apply plan", validPlan(), func() Document { return &ApplyPlan{} }, false},
		{"verification report", validVerification(), func() Document { return &VerificationReport{} }, false},
		{"decision event", validDecision(), func() Document { return &DecisionEvent{} }, true},
		{"receipt", validReceipt(), func() Document { return &Receipt{} }, true},
	}
}
