package workitem

import "strings"

var displayStatusVocabulary = []string{
	"current", "next", "active", "parked",
	"inbox", "ready", "plan", "ritual", "chains", "none",
}

// DisplayStatus returns the status shown for a workitem row. Directory-backed
// attention work uses its attention stage; other work uses lifecycle stage.
func DisplayStatus(item WorkItem) string {
	if item.AttentionStage != "" {
		return item.AttentionStage
	}
	switch item.LifecycleStage {
	case LifecycleStagePlanning:
		return "plan"
	case LifecycleStageNone, "":
		return "none"
	default:
		return string(item.LifecycleStage)
	}
}

func NormalizeDisplayStatus(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "planning" {
		return "plan"
	}
	return value
}

func IsDisplayStatus(value string) bool {
	value = NormalizeDisplayStatus(value)
	for _, status := range displayStatusVocabulary {
		if value == status {
			return true
		}
	}
	return false
}

func DisplayStatusVocabulary() []string {
	return append([]string(nil), displayStatusVocabulary...)
}
