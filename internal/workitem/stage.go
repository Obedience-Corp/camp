package workitem

// LifecycleStage identifies the workflow stage of a work item.
type LifecycleStage string

const (
	LifecycleStageNone     LifecycleStage = "none"
	LifecycleStageInbox    LifecycleStage = "inbox"
	LifecycleStageActive   LifecycleStage = "active"
	LifecycleStageReady    LifecycleStage = "ready"
	LifecycleStagePlanning LifecycleStage = "planning"
	LifecycleStageRitual   LifecycleStage = "ritual"
	LifecycleStageChains   LifecycleStage = "chains"
)

// StagesByType lists valid lifecycle stages per built-in workflow type.
var StagesByType = map[WorkflowType][]LifecycleStage{
	WorkflowTypeIntent: {
		LifecycleStageInbox,
		LifecycleStageActive,
		LifecycleStageReady,
	},
	WorkflowTypeDesign: {
		LifecycleStageNone,
		LifecycleStageActive,
		LifecycleStageReady,
	},
	WorkflowTypeExplore: {
		LifecycleStageNone,
		LifecycleStageActive,
		LifecycleStageReady,
	},
	WorkflowTypeFestival: {
		LifecycleStageNone,
		LifecycleStagePlanning,
		LifecycleStageReady,
		LifecycleStageActive,
		LifecycleStageRitual,
		LifecycleStageChains,
	},
}

// ValidStagesForType returns the stages accepted for a workflow type. Custom
// workflow types are directory-backed workitems that are active by location,
// same as design/explore.
func ValidStagesForType(wt WorkflowType) []LifecycleStage {
	if stages, ok := StagesByType[wt]; ok {
		return stages
	}
	return []LifecycleStage{LifecycleStageNone, LifecycleStageActive}
}

func IsValidStageForTypes(stage LifecycleStage, types []string) bool {
	if len(types) == 0 {
		for wt := range StagesByType {
			if stageAllowed(wt, stage) {
				return true
			}
		}
		return stage == LifecycleStageNone
	}
	for _, raw := range types {
		if stageAllowed(WorkflowType(raw), stage) {
			return true
		}
	}
	return false
}

// AllLifecycleStages returns every lifecycle stage any workflow type accepts,
// in declaration order. Use it for diagnostics that must list what a stage
// field allows; use ValidStagesForType when the type is known.
func AllLifecycleStages() []string {
	all := []LifecycleStage{
		LifecycleStageNone,
		LifecycleStageInbox,
		LifecycleStageActive,
		LifecycleStageReady,
		LifecycleStagePlanning,
		LifecycleStageRitual,
		LifecycleStageChains,
	}
	out := make([]string, len(all))
	for i, stage := range all {
		out[i] = string(stage)
	}
	return out
}

// AttentionStages returns the attention-stage vocabulary. This is the single
// source of truth for the values --json publishes and other packages validate
// against, so the two cannot drift.
func AttentionStages() []string {
	return []string{"current", "next", "active", "parked"}
}

func StageVocabulary() map[string][]string {
	vocab := make(map[string][]string, len(StagesByType))
	for wt, stages := range StagesByType {
		values := make([]string, len(stages))
		for i, stage := range stages {
			values[i] = string(stage)
		}
		vocab[string(wt)] = values
	}
	return vocab
}

func stageAllowed(wt WorkflowType, stage LifecycleStage) bool {
	for _, allowed := range ValidStagesForType(wt) {
		if allowed == stage {
			return true
		}
	}
	return false
}
