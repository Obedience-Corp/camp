package triage

import "time"

// Confidence is how much the producer trusts its own reading of a row.
type Confidence string

const (
	ConfidenceHigh   Confidence = "high"
	ConfidenceMedium Confidence = "medium"
	ConfidenceLow    Confidence = "low"
)

// Confidences returns the confidence vocabulary.
func Confidences() []string {
	return []string{
		string(ConfidenceHigh),
		string(ConfidenceMedium),
		string(ConfidenceLow),
	}
}

// EvidenceRole is what produced a record.
type EvidenceRole string

const (
	// EvidenceRoleEvidence is a per-row read; the queue's default role.
	EvidenceRoleEvidence EvidenceRole = "evidence"
	// EvidenceRoleSynthesis is a batch pass over already-gathered evidence.
	EvidenceRoleSynthesis EvidenceRole = "synthesis"
	// EvidenceRoleHuman is a record a person wrote directly (doc 08's
	// unassisted driver).
	EvidenceRoleHuman EvidenceRole = "human"
	// EvidenceRoleDeterministic is a record camp pre-filled from marker and
	// registry signals alone, with no judgment added — the zero-judgment path.
	EvidenceRoleDeterministic EvidenceRole = "deterministic"
)

// EvidenceRoles returns the producer role vocabulary.
func EvidenceRoles() []string {
	return []string{
		string(EvidenceRoleEvidence),
		string(EvidenceRoleSynthesis),
		string(EvidenceRoleHuman),
		string(EvidenceRoleDeterministic),
	}
}

// EvidenceRecord is `evidence/<stable-id>.json`: what was found out about one
// row. Everything here is advisory data reviewed by a human; camp never treats
// a record as authority to mutate anything.
type EvidenceRecord struct {
	SchemaVersion string `json:"schema_version"`
	StableID      string `json:"stable_id"`

	// NoEvidence marks the explicit "decided without a gathered record" case
	// (doc 08, driver 2). It satisfies the judging -> reviewing gate the same
	// way a full record does, while recording honestly that no reading was
	// done: a no-evidence record carries no judgment fields.
	NoEvidence bool `json:"no_evidence,omitempty"`

	OriginalGoal     string   `json:"original_goal"`
	Delivered        []string `json:"delivered"`
	Missing          []string `json:"missing"`
	StaleAssumptions []string `json:"stale_assumptions"`
	// Related names other work by ref or qualified id, e.g. "WI-a1b2c3",
	// "festival:CI0009", "pr:obey#239".
	Related       []string   `json:"related"`
	OpenDecisions []string   `json:"open_decisions"`
	Confidence    Confidence `json:"confidence"`

	// ConfidenceNotes qualifies what the confidence applies to.
	//
	// The field trial's records did not report one confidence per row; 12 of
	// 20 scoped it ("high for implementation state; medium-high for the
	// routing diagnosis"). A single enum cannot hold that, and dropping the
	// qualification loses the part a reviewer actually acts on. The enum stays
	// as spec doc 04 defines it so it remains sortable and filterable; this
	// carries what it is about.
	ConfidenceNotes string `json:"confidence_notes,omitempty"`

	// Signals are deterministic facts camp itself resolved — age, stage,
	// latest run status — as opposed to anything a reader judged. They are
	// what `evidence template` pre-fills so the zero-judgment path (doc 08,
	// driver 2) has something to propose from. Kept separate from the
	// judgment fields on purpose: mixing a fact camp measured into
	// `delivered` would make the record claim someone concluded it.
	Signals map[string]string `json:"signals,omitempty"`
	// Anchors are the re-checkable facts this record rests on. A record with
	// no anchors can still be valid, but nothing about it can go stale, so
	// the verdict it supports carries no expiry.
	Anchors    []Anchor   `json:"anchors"`
	ProducedBy ProducedBy `json:"produced_by"`
}

// ProducedBy is the provenance of an evidence record.
type ProducedBy struct {
	Role EvidenceRole `json:"role"`
	// Runtime is free text naming what produced the record ("claude-code",
	// "human", "camp evidence template"). Camp does not interpret it; it
	// exists so a reader can tell where a record came from.
	Runtime string    `json:"runtime"`
	At      time.Time `json:"at"`
}

// Normalize implements Document.
func (r *EvidenceRecord) Normalize() {
	r.SchemaVersion = SchemaVersion
	r.Delivered = normalizeStrings(r.Delivered)
	r.Missing = normalizeStrings(r.Missing)
	r.StaleAssumptions = normalizeStrings(r.StaleAssumptions)
	r.Related = normalizeStrings(r.Related)
	r.OpenDecisions = normalizeStrings(r.OpenDecisions)
	if r.Anchors == nil {
		r.Anchors = []Anchor{}
	}
	normalizeTime(&r.ProducedBy.At)
}

func (r *EvidenceRecord) kind() string    { return "evidence record" }
func (r *EvidenceRecord) version() string { return r.SchemaVersion }

// Validate implements Document.
func (r *EvidenceRecord) Validate() []Violation {
	var out []Violation
	out = append(out, checkRequired("stable_id", r.StableID)...)
	out = append(out, checkEnum("produced_by.role", string(r.ProducedBy.Role), EvidenceRoles())...)
	out = append(out, checkRequired("produced_by.runtime", r.ProducedBy.Runtime)...)
	out = append(out, checkTimeSet("produced_by.at", r.ProducedBy.At)...)

	if r.NoEvidence {
		const because = "on a no_evidence record"
		out = append(out, checkEmptyString("original_goal", r.OriginalGoal, because)...)
		out = append(out, checkEmptyString("confidence", string(r.Confidence), because)...)
		out = append(out, checkEmptyString("confidence_notes", r.ConfidenceNotes, because)...)
		out = append(out, checkEmptySlice("delivered", r.Delivered, because)...)
		out = append(out, checkEmptySlice("missing", r.Missing, because)...)
		out = append(out, checkEmptySlice("stale_assumptions", r.StaleAssumptions, because)...)
		out = append(out, checkEmptySlice("open_decisions", r.OpenDecisions, because)...)
	} else {
		out = append(out, checkRequired("original_goal", r.OriginalGoal)...)
		out = append(out, checkEnum("confidence", string(r.Confidence), Confidences())...)
	}

	for i := range r.Anchors {
		out = append(out, r.Anchors[i].validate(indexPath("anchors", i))...)
	}
	return out
}
