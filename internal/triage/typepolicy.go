package triage

import (
	"sort"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// TypePolicySchemaVersion is the format version of a per-type policy file.
const TypePolicySchemaVersion = "triage-type/v1alpha1"

// TypePolicy is what one workitem type may be decided into.
//
// Labels are the campaign's vocabulary; canonical actions are camp's. A
// campaign can rename "completed" to "concluded" without triage learning a new
// mutation, because the label is only ever a key into this map.
type TypePolicy struct {
	SchemaVersion string `json:"schema_version"`
	// Dispositions maps an offered label to the action it performs.
	Dispositions map[string]CanonicalAction `json:"dispositions"`
	Evidence     EvidenceDepth              `json:"evidence"`
	RoutingTier  RoutingTier                `json:"routing_tier"`
}

// Labels returns the dispositions this policy offers, in stable order.
func (p TypePolicy) Labels() []string {
	labels := make([]string, 0, len(p.Dispositions))
	for label := range p.Dispositions {
		labels = append(labels, label)
	}
	sort.Strings(labels)
	return labels
}

// ResolveDisposition maps a label to the action camp will actually perform.
//
// An unknown label is refused with the vocabulary that would have worked,
// rather than guessed at: a disposition that resolved to the wrong action
// would move real work to the wrong place, and the label is the only thing the
// operator typed.
func ResolveDisposition(policy TypePolicy, label string) (CanonicalAction, error) {
	if label == "" {
		return "", camperrors.NewValidation("disposition",
			"is required", camperrors.ErrInvalidInput)
	}
	action, ok := policy.Dispositions[label]
	if !ok {
		return "", &ValidationError{
			Kind: "disposition",
			Violations: []Violation{{
				Field:   "disposition",
				Message: "unknown value " + quote(label) + " for this type",
				Allowed: policy.Labels(),
			}},
		}
	}
	return action, nil
}

// TypePolicyFor returns the policy for a workitem type.
//
// The built-ins below are spec doc 05's shipped `types/*.yaml` expressed as Go
// values. The profile sequence replaces the source with those files; this
// signature is the seam, so nothing that calls it has to change.
func TypePolicyFor(wfType string) TypePolicy {
	if policy, ok := builtinTypePolicies()[wfType]; ok {
		return policy
	}
	return defaultTypePolicy()
}

// builtinTypePolicies is the shipped per-type vocabulary.
func builtinTypePolicies() map[string]TypePolicy {
	return map[string]TypePolicy{
		// Designs are checked against code and PRs, so they get the full
		// vocabulary including consolidation.
		"design": {
			SchemaVersion: TypePolicySchemaVersion,
			Evidence:      EvidenceDepthDeep,
			RoutingTier:   RoutingTierDefault,
			Dispositions: map[string]CanonicalAction{
				"current":     "attention/current",
				"next":        "attention/next",
				"parked":      "attention/parked",
				"ready":       "rail/ready",
				"active":      "rail/active",
				"festival":    "rail/festival",
				"completed":   "dungeon/completed",
				"archived":    "dungeon/archived",
				"someday":     "dungeon/someday",
				"consolidate": ActionSplit,
			},
		},
		"explore": {
			SchemaVersion: TypePolicySchemaVersion,
			Evidence:      EvidenceDepthDeep,
			RoutingTier:   RoutingTierDefault,
			Dispositions: map[string]CanonicalAction{
				"current":     "attention/current",
				"next":        "attention/next",
				"parked":      "attention/parked",
				"ready":       "rail/ready",
				"completed":   "dungeon/completed",
				"archived":    "dungeon/archived",
				"someday":     "dungeon/someday",
				"consolidate": ActionSplit,
			},
		},
		// An inbox intent decision is seconds, not a code trace, so the
		// vocabulary is deliberately short.
		"intent": {
			SchemaVersion: TypePolicySchemaVersion,
			Evidence:      EvidenceDepthMetadata,
			RoutingTier:   RoutingTierCheap,
			Dispositions: map[string]CanonicalAction{
				"promote": "rail/ready",
				"next":    "attention/next",
				"park":    "attention/parked",
				"drop":    "dungeon/archived",
				"someday": "dungeon/someday",
				"keep":    ActionNone,
			},
		},
		// Festivals are fest's to review deeply; triage only records where
		// they stand.
		"festival": {
			SchemaVersion: TypePolicySchemaVersion,
			Evidence:      EvidenceDepthNone,
			RoutingTier:   RoutingTierCheap,
			Dispositions: map[string]CanonicalAction{
				"current":   "attention/current",
				"next":      "attention/next",
				"parked":    "attention/parked",
				"completed": "dungeon/completed",
				"archived":  "dungeon/archived",
				"someday":   "dungeon/someday",
				"keep":      ActionNone,
			},
		},
	}
}

// defaultTypePolicy is what a type camp has never heard of inherits, so a
// campaign that invents a workitem type can triage it with zero configuration.
func defaultTypePolicy() TypePolicy {
	return TypePolicy{
		SchemaVersion: TypePolicySchemaVersion,
		Evidence:      EvidenceDepthMetadata,
		RoutingTier:   RoutingTierDefault,
		Dispositions: map[string]CanonicalAction{
			"current":     "attention/current",
			"next":        "attention/next",
			"active":      "attention/active",
			"parked":      "attention/parked",
			"ready":       "rail/ready",
			"festival":    "rail/festival",
			"completed":   "dungeon/completed",
			"archived":    "dungeon/archived",
			"someday":     "dungeon/someday",
			"consolidate": ActionSplit,
			"keep":        ActionNone,
		},
	}
}
