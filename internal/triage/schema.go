// Package triage implements `camp triage`: the versioned `triage/v1alpha1`
// document formats that live under `.campaign/triage/runs/<run-id>/` and the
// run store that reads and writes them.
//
// A triage run is a recorded, resumable session over a campaign's workitems.
// Camp owns the state machine and every mutation; judgment (evidence,
// proposals) enters only as data validated against these schemas. Nothing in
// this package calls a model.
//
// Two decoding modes exist on purpose. Documents supplied from outside camp
// (an agent's evidence record, a proposal's rationale) are parsed with Strict,
// which reports every unknown field and every rule violation at once. The
// store reads its own files with Lenient, which tolerates additive fields
// within a schema version so a run written by a newer camp still opens.
//
// Reference: workflow/design/camp-triage/04-data-schemas-and-phases.md
package triage

import (
	"strings"
	"time"

	"github.com/Obedience-Corp/camp/internal/workitem"
)

// SchemaVersion is the format version every triage document carries. Reads
// reject any other value rather than guessing at a foreign layout.
//
// Changelog:
//   - v1alpha1: initial format — manifest, run state, evidence record,
//     decision event, apply plan, receipt, verification report.
const SchemaVersion = "triage/v1alpha1"

// RunMode is how a run selected the rows in its manifest.
type RunMode string

const (
	// RunModeIncremental carries verdicts forward from a base run for rows
	// whose identity and evidence anchors are unchanged (D4, the default).
	RunModeIncremental RunMode = "incremental"
	// RunModeFull re-reviews every row in scope.
	RunModeFull RunMode = "full"
)

// RunModes returns the run mode vocabulary.
func RunModes() []string {
	return []string{string(RunModeIncremental), string(RunModeFull)}
}

// EvidenceDepth is how much evidence a row's policy asks for.
type EvidenceDepth string

const (
	// EvidenceDepthDeep reads the workitem against code, PRs, and festivals.
	EvidenceDepthDeep EvidenceDepth = "deep"
	// EvidenceDepthMetadata uses marker and registry signals only.
	EvidenceDepthMetadata EvidenceDepth = "metadata"
	// EvidenceDepthNone gathers no evidence; the row is decided from the
	// review card alone.
	EvidenceDepthNone EvidenceDepth = "none"
)

// EvidenceDepths returns the evidence depth vocabulary.
func EvidenceDepths() []string {
	return []string{
		string(EvidenceDepthDeep),
		string(EvidenceDepthMetadata),
		string(EvidenceDepthNone),
	}
}

// RoutingTier is the advisory model class a driver should use for a row.
// Camp never acts on it; it is passed through to drivers verbatim (D5).
type RoutingTier string

const (
	RoutingTierCheap   RoutingTier = "cheap"
	RoutingTierDefault RoutingTier = "default"
	RoutingTierStrong  RoutingTier = "strong"
)

// RoutingTiers returns the routing tier vocabulary.
func RoutingTiers() []string {
	return []string{
		string(RoutingTierCheap),
		string(RoutingTierDefault),
		string(RoutingTierStrong),
	}
}

// AttentionStages returns the attention-stage vocabulary a manifest row may
// report, sourced from the workitem package so triage cannot drift from what
// camp actually stores.
func AttentionStages() []string {
	return workitem.AttentionStages()
}

// Manifest is `manifest.json`: the frozen snapshot a run was started from.
// The resolved profile is embedded so the run stays reproducible even after
// `.campaign/triage/profile.yaml` changes.
type Manifest struct {
	SchemaVersion string          `json:"schema_version"`
	RunID         string          `json:"run_id"`
	Mode          RunMode         `json:"mode"`
	Profile       ManifestProfile `json:"profile"`
	// BaseRunID names the run an incremental run diffed against. Nil on a
	// bootstrap or full run — the field is always present so a reader never
	// has to guess whether an older writer simply omitted it.
	BaseRunID *string `json:"base_run_id"`
	// ScopeExpressions are the `--scope key:value` expressions the run was
	// started with, frozen for the same reason the resolved profile is: a run
	// has to stay reproducible.
	//
	// Refresh is what makes this load-bearing rather than decorative. It
	// re-walks the campaign and asks which discoveries the snapshot does not
	// carry; without the run's own scope it would answer "all of them" on any
	// run narrowed by --scope and append the whole campaign to the manifest.
	// The profile's own scope keys survive inside Profile.Resolved; these are
	// the per-invocation expressions layered on top.
	ScopeExpressions []string  `json:"scope_expressions"`
	CreatedAt        time.Time `json:"created_at"`
	// CarryLosses names every row that held a verdict in the base run and did
	// not carry it into this one, with the reason.
	//
	// Frozen here because the reason is snapshot data — a fact about how this
	// run was built — and because spec doc 04 requires `camp triage status`
	// answer why a row was re-queued. A loss recorded only in the start
	// command's output stops being answerable the moment that output scrolls
	// away, which is the case where an operator most wants to ask.
	//
	// Always present, `[]` on a run that lost nothing, so a reader never has to
	// tell absent from empty.
	CarryLosses []CarryLoss   `json:"carry_losses"`
	Rows        []ManifestRow `json:"rows"`
}

// ManifestProfile is the profile a run resolved, by name and by value.
type ManifestProfile struct {
	Name     string          `json:"name"`
	Resolved ResolvedProfile `json:"resolved"`
	// TypePolicies are the per-type vocabularies the run resolved, frozen
	// alongside the profile for the same reason: a verdict has to stay
	// explainable after the campaign's types/*.yaml move on. Without this the
	// profile was frozen but the vocabulary that produced the disposition was
	// re-read live, so a renamed label could make an old verdict unreadable.
	TypePolicies map[string]TypePolicy `json:"type_policies,omitempty"`
}

// PolicyFor returns the type policy this run judged a type under, falling back
// to the shipped `_default` and then to camp's built-in.
//
// Every consumer of a row's vocabulary goes through here so approve, carry,
// and the plan compiler all read the policy the run actually used rather than
// whatever the campaign's files say today.
func (m *Manifest) PolicyFor(wfType string) TypePolicy {
	if m != nil {
		if policy, ok := m.Profile.TypePolicies[wfType]; ok {
			return policy
		}
		if policy, ok := m.Profile.TypePolicies[DefaultTypePolicyName]; ok {
			return policy
		}
	}
	return TypePolicyFor(wfType)
}

// DefaultTypePolicyName is the shipped fallback policy's file name.
const DefaultTypePolicyName = "_default"

// ManifestRow is one workitem frozen into a run's snapshot.
type ManifestRow struct {
	// StableID is the workitem's durable identity; it survives moves and is
	// the key every other run file joins on.
	StableID string `json:"stable_id"`
	// Ref is the short workitem ref (WI-xxxxxx). Empty for legacy items that
	// have no marker yet; those carry an IdentityException instead.
	Ref string `json:"ref"`
	// Key is the discovery key, "<type>:<campaign-relative path>".
	Key string `json:"key"`
	// ItemKind is how the workitem is stored: a directory under
	// `workflow/<type>/<slug>/`, or a single markdown file such as an intent
	// under `.campaign/intents/`.
	//
	// Frozen because the plan compiler needs it and cannot derive it. Camp's
	// lifecycle verbs are split along this line — `camp workitem stage` and
	// `camp workitem promote` act on directory-backed items, `camp idea move`
	// on file-backed ones — and a compiler that is pure over the snapshot
	// cannot stat the filesystem to tell which it is holding. It is camp's own
	// `ItemKind`, recorded rather than re-derived, so the two cannot drift.
	//
	// Added 2026-08-05, blessed under Lance's in-session delegation, after the
	// phase 006 acceptance run found that no verdict on an intent could be
	// applied at all: every command the compiler emitted named a verb that
	// refuses file-backed workitems, and it shipped because every plan it had
	// been checked against held directory-backed rows.
	ItemKind       string `json:"item_kind"`
	Type           string `json:"type"`
	Title          string `json:"title"`
	RelativePath   string `json:"relative_path"`
	LifecycleStage string `json:"lifecycle_stage"`
	AttentionStage string `json:"attention_stage"`
	// Batch is the 1-based review batch this row was partitioned into.
	Batch  int       `json:"batch"`
	Policy RowPolicy `json:"policy"`
	// CarriedFrom names the run whose verdict this row carried forward (D4),
	// or nil when the row was queued for fresh judgment.
	CarriedFrom *string `json:"carried_from"`
	// IdentityException records the explicit path-bound legacy allowance
	// granted when preflight could not repair the row's identity (FT-008),
	// or nil when identity resolved normally.
	IdentityException *IdentityException `json:"identity_exception"`
}

// RowPolicy is the slice of the resolved profile that applies to one row.
// It is frozen into the manifest so a verdict can be explained later without
// re-deriving policy from a profile that may since have changed.
type RowPolicy struct {
	Evidence    EvidenceDepth `json:"evidence"`
	RoutingTier RoutingTier   `json:"routing_tier"`
}

// IdentityException is the path-bound legacy allowance from FT-008: a row
// whose marker identity could not be repaired during preflight still triages,
// but only while it lives at the recorded path. Any move invalidates the
// exception, because path is the only identity such a row has.
type IdentityException struct {
	// Reason is why repair did not happen, in the preflight's own words.
	// Free text rather than an enum: preflight owns the vocabulary and lands
	// in a later sequence.
	Reason string `json:"reason"`
	// Path is the campaign-relative path the exception is bound to.
	Path      string    `json:"path"`
	GrantedBy string    `json:"granted_by"`
	GrantedAt time.Time `json:"granted_at"`
}

// Normalize implements Document.
func (m *Manifest) Normalize() {
	m.SchemaVersion = SchemaVersion
	normalizeTime(&m.CreatedAt)
	if m.Rows == nil {
		m.Rows = []ManifestRow{}
	}
	if m.ScopeExpressions == nil {
		m.ScopeExpressions = []string{}
	}
	if m.CarryLosses == nil {
		m.CarryLosses = []CarryLoss{}
	}
	m.Profile.Resolved.Normalize()
	for i := range m.Rows {
		if m.Rows[i].IdentityException != nil {
			normalizeTime(&m.Rows[i].IdentityException.GrantedAt)
		}
	}
}

func (m *Manifest) kind() string    { return "run manifest" }
func (m *Manifest) version() string { return m.SchemaVersion }

// Validate implements Document, reporting every rule the manifest violates.
func (m *Manifest) Validate() []Violation {
	out := checkRequired("run_id", m.RunID)
	return append(out, m.validateContent()...)
}

// validateContent checks everything a snapshot is responsible for — every rule
// except the run id, which the store assigns from its clock at creation time.
// Splitting it this way lets the snapshot reject a bad row where it was built,
// while the store still validates the complete document before any write.
func (m *Manifest) validateContent() []Violation {
	var out []Violation
	out = append(out, checkEnum("mode", string(m.Mode), RunModes())...)
	out = append(out, checkTimeSet("created_at", m.CreatedAt)...)
	out = append(out, checkRequired("profile.name", m.Profile.Name)...)
	out = append(out, m.Profile.Resolved.validate("profile.resolved")...)

	if m.BaseRunID != nil {
		out = append(out, checkRequired("base_run_id", *m.BaseRunID)...)
	}
	if m.Mode == RunModeIncremental && m.BaseRunID == nil {
		out = append(out, Violation{
			Field:   "base_run_id",
			Message: "is required for an incremental run",
		})
	}

	seen := make(map[string]int, len(m.Rows))
	for i := range m.Rows {
		path := indexPath("rows", i)
		out = append(out, m.Rows[i].validate(path)...)
		if id := m.Rows[i].StableID; id != "" {
			if first, dup := seen[id]; dup {
				out = append(out, Violation{
					Field:   joinPath(path, "stable_id"),
					Message: "duplicate of " + indexPath("rows", first),
				})
			} else {
				seen[id] = i
			}
		}
	}
	return out
}

// validate reports every rule this row violates, prefixed with its path in the
// manifest.
func (r *ManifestRow) validate(path string) []Violation {
	var out []Violation
	out = append(out, checkRequired(joinPath(path, "stable_id"), r.StableID)...)
	out = append(out, checkRequired(joinPath(path, "key"), r.Key)...)
	out = append(out, checkRequired(joinPath(path, "type"), r.Type)...)
	out = append(out, checkRequired(joinPath(path, "relative_path"), r.RelativePath)...)
	out = append(out, checkMinInt(joinPath(path, "batch"), r.Batch, 1)...)
	out = append(out, checkEnum(
		joinPath(path, "policy.evidence"), string(r.Policy.Evidence), EvidenceDepths())...)
	out = append(out, checkEnum(
		joinPath(path, "policy.routing_tier"), string(r.Policy.RoutingTier), RoutingTiers())...)
	out = append(out, checkOptionalEnum(
		joinPath(path, "attention_stage"), r.AttentionStage, AttentionStages())...)

	// The lifecycle vocabulary is camp's, not triage's: ask the workitem
	// package rather than restating it here. Types are open (a campaign may
	// invent one), so the check is "some type accepts this stage".
	if stage := r.LifecycleStage; stage != "" {
		if !workitem.IsValidStageForTypes(workitem.LifecycleStage(stage), nil) {
			out = append(out, Violation{
				Field:   joinPath(path, "lifecycle_stage"),
				Message: "unknown value " + quote(stage),
				Allowed: workitem.AllLifecycleStages(),
			})
		}
	} else {
		out = append(out, Violation{
			Field:   joinPath(path, "lifecycle_stage"),
			Message: "is required",
			Allowed: workitem.AllLifecycleStages(),
		})
	}

	// A ref is optional (FT-008 legacy rows have none) but a malformed one is
	// a bug in whatever wrote the manifest, not a legacy condition.
	if r.Ref != "" && !strings.HasPrefix(r.Ref, workitem.RefPrefix) {
		out = append(out, Violation{
			Field:   joinPath(path, "ref"),
			Message: "must start with " + quote(workitem.RefPrefix),
		})
	}

	if r.CarriedFrom != nil {
		out = append(out, checkRequired(joinPath(path, "carried_from"), *r.CarriedFrom)...)
	}
	if ex := r.IdentityException; ex != nil {
		p := joinPath(path, "identity_exception")
		out = append(out, checkRequired(joinPath(p, "reason"), ex.Reason)...)
		out = append(out, checkRequired(joinPath(p, "path"), ex.Path)...)
		out = append(out, checkRequired(joinPath(p, "granted_by"), ex.GrantedBy)...)
		out = append(out, checkTimeSet(joinPath(p, "granted_at"), ex.GrantedAt)...)
	}
	return out
}
