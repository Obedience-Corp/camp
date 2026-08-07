package triage

import (
	"time"

	"github.com/Obedience-Corp/camp/internal/workitem"
)

// ProfileSchemaVersion is the format version of a triage profile. It is
// separate from SchemaVersion because a profile is a user-edited file with its
// own lifecycle, while run documents are camp-written.
const ProfileSchemaVersion = "triage-profile/v1alpha1"

// DefaultAnchorRecheckMinutes is the shipped remote-anchor throttle window.
const DefaultAnchorRecheckMinutes = 5

// ProfileNameDefault is the built-in profile every campaign starts from.
const ProfileNameDefault = "default"

// ResolvedProfile is a profile after built-in defaults, the campaign's
// profile.yaml, and any named profile have been merged. Every run embeds the
// one it resolved, so a verdict can always be explained against the policy
// that produced it rather than against whatever the profile says today.
//
// This phase ships the built-in default only; the file layer, per-type
// policies, and named profiles land with the profile sequence.
type ResolvedProfile struct {
	SchemaVersion string           `json:"schema_version" yaml:"schema_version"`
	Scope         ProfileScope     `json:"scope" yaml:"scope"`
	Runs          ProfileRuns      `json:"runs" yaml:"runs"`
	Preflight     ProfilePreflight `json:"preflight" yaml:"preflight"`
	Review        ProfileReview    `json:"review" yaml:"review"`
	Evidence      ProfileEvidence  `json:"evidence" yaml:"evidence"`
	Routing       ProfileRouting   `json:"routing" yaml:"routing"`
	Anchors       ProfileAnchors   `json:"anchors" yaml:"anchors"`
	Apply         ProfileApply     `json:"apply" yaml:"apply"`
	Outputs       ProfileOutputs   `json:"outputs" yaml:"outputs"`
}

// ProfilePreflight controls what start does about rows whose identity is not
// durable yet (FT-008).
type ProfilePreflight struct {
	Identity IdentityPolicy `json:"identity" yaml:"identity"`
}

// IdentityPolicy is how the preflight treats a workitem directory that should
// carry a .workitem marker but does not.
type IdentityPolicy string

const (
	// IdentityPolicyRepair adopts the directory and reports what it did. The
	// default: an unadopted directory is a gap camp can close itself, and
	// stopping to ask would turn every messy campaign into a chore before
	// triage could begin.
	IdentityPolicyRepair IdentityPolicy = "repair"
	// IdentityPolicyStrict refuses the run and lists every unrepaired row,
	// for campaigns that want adoption to be a deliberate act.
	IdentityPolicyStrict IdentityPolicy = "strict"
)

// IdentityPolicies returns the identity-policy vocabulary.
func IdentityPolicies() []string {
	return []string{string(IdentityPolicyRepair), string(IdentityPolicyStrict)}
}

// ProfileScope selects which workitems a run considers.
type ProfileScope struct {
	// IncludeParked keeps parked items visible. Parked is a decision to
	// revisit, not a decision to forget, so the default reviews them.
	IncludeParked bool     `json:"include_parked" yaml:"include_parked"`
	ExcludeTypes  []string `json:"exclude_types" yaml:"exclude_types"`
	// ExcludePaths are campaign-relative glob patterns.
	ExcludePaths []string `json:"exclude_paths" yaml:"exclude_paths"`
}

// ProfileRuns controls run mode and the staleness notice.
type ProfileRuns struct {
	Mode RunMode `json:"mode" yaml:"mode"`
	// StaleAfterDays is how old the last run may be before high-traffic
	// commands suggest running triage again.
	StaleAfterDays int `json:"stale_after_days" yaml:"stale_after_days"`
}

// ProfileReview controls how rows are grouped and approved.
type ProfileReview struct {
	GroupBy   ReviewGroupBy   `json:"group_by" yaml:"group_by"`
	BatchSize int             `json:"batch_size" yaml:"batch_size"`
	Approval  ApprovalGranule `json:"approval" yaml:"approval"`
	// RequireRationale rejects a proposal that carries no rationale.
	RequireRationale bool `json:"require_rationale" yaml:"require_rationale"`
}

// ReviewGroupBy is how rows are batched for review.
type ReviewGroupBy string

const (
	GroupByType           ReviewGroupBy = "type"
	GroupByProject        ReviewGroupBy = "project"
	GroupByTag            ReviewGroupBy = "tag"
	GroupByAttentionStage ReviewGroupBy = "attention_stage"
)

// ReviewGroupBys returns the grouping vocabulary.
func ReviewGroupBys() []string {
	return []string{
		string(GroupByType),
		string(GroupByProject),
		string(GroupByTag),
		string(GroupByAttentionStage),
	}
}

// ApprovalGranule is what one approval covers by default in the review flow.
// Terminal rows always confirm individually regardless of this setting.
type ApprovalGranule string

const (
	ApprovalRow   ApprovalGranule = "row"
	ApprovalLane  ApprovalGranule = "lane"
	ApprovalBatch ApprovalGranule = "batch"
)

// ApprovalGranules returns the approval granularity vocabulary.
func ApprovalGranules() []string {
	return []string{
		string(ApprovalRow),
		string(ApprovalLane),
		string(ApprovalBatch),
	}
}

// ProfileEvidence sets evidence depth per attention lane.
type ProfileEvidence struct {
	// DepthByStage is keyed by attention stage plus "none" for items with no
	// stage set.
	DepthByStage map[string]EvidenceDepth `json:"depth_by_stage" yaml:"depth_by_stage"`
}

// EvidenceStageNone is the DepthByStage key for items with no attention stage.
const EvidenceStageNone = "none"

// ProfileRouting is advisory instruction for whatever drives the evidence
// phase. Camp passes it through verbatim and never acts on it.
type ProfileRouting struct {
	EvidenceTier   RoutingTier `json:"evidence_tier" yaml:"evidence_tier"`
	EscalationTier RoutingTier `json:"escalation_tier" yaml:"escalation_tier"`
	SynthesisTier  RoutingTier `json:"synthesis_tier" yaml:"synthesis_tier"`
	MaxConcurrent  int         `json:"max_concurrent" yaml:"max_concurrent"`
}

// ProfileAnchors controls how refresh re-checks anchors that need the network.
type ProfileAnchors struct {
	// RecheckMinutes is how long a cached remote verdict answers before
	// refresh calls out again. Zero means never cache: every refresh checks.
	//
	// The default is deliberately short. FT-013 measured evidence going stale
	// in minutes — a PR merging shortly after the snapshot — so a long window
	// would cache away the exact failure this phase exists to catch. Local
	// anchors ignore this entirely; hashing a file is cheaper than deciding
	// whether to.
	RecheckMinutes int `json:"recheck_minutes" yaml:"recheck_minutes"`
}

// AnchorRecheckInterval is the throttle window as a duration.
func (p ProfileAnchors) AnchorRecheckInterval() time.Duration {
	if p.RecheckMinutes <= 0 {
		return 0
	}
	return time.Duration(p.RecheckMinutes) * time.Minute
}

// ProfileApply controls how non-terminal changes reach disk. Terminal moves,
// splits, and festival promotions always require recorded human approval;
// that is product behavior and has no profile key.
type ProfileApply struct {
	AttentionChanges AttentionChangePolicy `json:"attention_changes" yaml:"attention_changes"`
}

// AttentionChangePolicy is when approved attention-stage changes execute.
type AttentionChangePolicy string

const (
	// AttentionChangesOnApply applies them with the batch.
	AttentionChangesOnApply AttentionChangePolicy = "on-apply"
	// AttentionChangesManual prints them as commands instead.
	AttentionChangesManual AttentionChangePolicy = "manual"
)

// AttentionChangePolicies returns the attention-change vocabulary.
func AttentionChangePolicies() []string {
	return []string{
		string(AttentionChangesOnApply),
		string(AttentionChangesManual),
	}
}

// ProfileOutputs controls the rendered copies a run leaves behind.
type ProfileOutputs struct {
	// PrioritiesExport is a campaign-relative path for a rendered copy of
	// PRIORITIES.md. Empty means no export; the run's own copy always exists.
	PrioritiesExport string `json:"priorities_export" yaml:"priorities_export"`
	// ScaffoldWorkflowDoc generates the companion WORKFLOW.md in the run.
	ScaffoldWorkflowDoc bool `json:"scaffold_workflow_doc" yaml:"scaffold_workflow_doc"`
}

// DefaultProfile returns the built-in `default` profile: the values a campaign
// with no `.campaign/triage/profile.yaml` runs under.
func DefaultProfile() ResolvedProfile {
	p := ResolvedProfile{
		SchemaVersion: ProfileSchemaVersion,
		Scope: ProfileScope{
			IncludeParked: true,
			ExcludeTypes:  []string{},
			ExcludePaths:  []string{},
		},
		Runs: ProfileRuns{
			Mode:           RunModeIncremental,
			StaleAfterDays: 14,
		},
		Preflight: ProfilePreflight{
			Identity: IdentityPolicyRepair,
		},
		Review: ProfileReview{
			GroupBy:          GroupByType,
			BatchSize:        5,
			Approval:         ApprovalLane,
			RequireRationale: true,
		},
		Evidence: ProfileEvidence{
			DepthByStage: map[string]EvidenceDepth{
				"current":         EvidenceDepthDeep,
				"next":            EvidenceDepthDeep,
				"active":          EvidenceDepthDeep,
				"parked":          EvidenceDepthMetadata,
				EvidenceStageNone: EvidenceDepthMetadata,
			},
		},
		Anchors: ProfileAnchors{RecheckMinutes: DefaultAnchorRecheckMinutes},
		Routing: ProfileRouting{
			EvidenceTier:   RoutingTierCheap,
			EscalationTier: RoutingTierStrong,
			SynthesisTier:  RoutingTierStrong,
			MaxConcurrent:  4,
		},
		Apply: ProfileApply{
			AttentionChanges: AttentionChangesOnApply,
		},
		Outputs: ProfileOutputs{
			PrioritiesExport:    "",
			ScaffoldWorkflowDoc: true,
		},
	}
	return p
}

// Normalize puts the profile in canonical form. It is called through the
// manifest that embeds it rather than on its own, so it is not a Document.
func (p *ResolvedProfile) Normalize() {
	if p.SchemaVersion == "" {
		p.SchemaVersion = ProfileSchemaVersion
	}
	p.Scope.ExcludeTypes = normalizeStrings(p.Scope.ExcludeTypes)
	p.Scope.ExcludePaths = normalizeStrings(p.Scope.ExcludePaths)
	if p.Evidence.DepthByStage == nil {
		p.Evidence.DepthByStage = map[string]EvidenceDepth{}
	}
}

// evidenceStageKeys returns the keys DepthByStage may carry: every attention
// stage plus "none".
func evidenceStageKeys() []string {
	return append(workitem.AttentionStages(), EvidenceStageNone)
}

// validate reports every rule the profile violates, prefixed with its path in
// the embedding document.
func (p *ResolvedProfile) validate(path string) []Violation {
	var out []Violation
	if p.SchemaVersion != ProfileSchemaVersion {
		out = append(out, Violation{
			Field:   joinPath(path, "schema_version"),
			Message: "unsupported profile schema version " + quote(p.SchemaVersion),
			Allowed: []string{ProfileSchemaVersion},
		})
	}
	out = append(out, checkEnum(joinPath(path, "runs.mode"), string(p.Runs.Mode), RunModes())...)
	out = append(out, checkMinInt(joinPath(path, "runs.stale_after_days"), p.Runs.StaleAfterDays, 0)...)
	out = append(out, checkEnum(
		joinPath(path, "preflight.identity"), string(p.Preflight.Identity), IdentityPolicies())...)
	out = append(out, checkEnum(
		joinPath(path, "review.group_by"), string(p.Review.GroupBy), ReviewGroupBys())...)
	out = append(out, checkMinInt(joinPath(path, "review.batch_size"), p.Review.BatchSize, 1)...)
	out = append(out, checkEnum(
		joinPath(path, "review.approval"), string(p.Review.Approval), ApprovalGranules())...)
	out = append(out, checkEnum(
		joinPath(path, "routing.evidence_tier"), string(p.Routing.EvidenceTier), RoutingTiers())...)
	out = append(out, checkEnum(
		joinPath(path, "routing.escalation_tier"), string(p.Routing.EscalationTier), RoutingTiers())...)
	out = append(out, checkEnum(
		joinPath(path, "routing.synthesis_tier"), string(p.Routing.SynthesisTier), RoutingTiers())...)
	out = append(out, checkMinInt(
		joinPath(path, "routing.max_concurrent"), p.Routing.MaxConcurrent, 1)...)
	out = append(out, checkEnum(
		joinPath(path, "apply.attention_changes"),
		string(p.Apply.AttentionChanges), AttentionChangePolicies())...)

	depthPath := joinPath(path, "evidence.depth_by_stage")
	allowedKeys := evidenceStageKeys()
	for _, key := range sortedKeys(p.Evidence.DepthByStage) {
		if !contains(allowedKeys, key) {
			out = append(out, Violation{
				Field:   joinPath(depthPath, key),
				Message: "unknown attention stage",
				Allowed: allowedKeys,
			})
			continue
		}
		out = append(out, checkEnum(
			joinPath(depthPath, key), string(p.Evidence.DepthByStage[key]), EvidenceDepths())...)
	}
	for _, key := range allowedKeys {
		if _, ok := p.Evidence.DepthByStage[key]; !ok {
			out = append(out, Violation{
				Field:   joinPath(depthPath, key),
				Message: "is required: every lane needs a depth",
				Allowed: EvidenceDepths(),
			})
		}
	}
	return out
}

// contains reports whether values holds want.
func contains(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}
