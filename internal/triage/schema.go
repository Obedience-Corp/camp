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
	BaseRunID *string       `json:"base_run_id"`
	CreatedAt time.Time     `json:"created_at"`
	Rows      []ManifestRow `json:"rows"`
}

// ManifestProfile is the profile a run resolved, by name and by value.
type ManifestProfile struct {
	Name     string          `json:"name"`
	Resolved ResolvedProfile `json:"resolved"`
}

// ManifestRow is one workitem frozen into a run's snapshot.
type ManifestRow struct {
	// StableID is the workitem's durable identity; it survives moves and is
	// the key every other run file joins on.
	StableID string `json:"stable_id"`
	// Ref is the short workitem ref (WI-xxxxxx). Empty for legacy items that
	// have no marker yet; those carry an IdentityException instead.
	Ref string `json:"ref"`
	// Key is the discovery key, "<type>:<campaign-relative path>".
	Key            string `json:"key"`
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
	var out []Violation
	out = append(out, checkRequired("run_id", m.RunID)...)
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
