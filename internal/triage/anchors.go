package triage

import "strings"

// AnchorKind tags which fact an anchor records.
type AnchorKind string

const (
	// AnchorKindPR is a pull request and the state it was observed in.
	AnchorKindPR AnchorKind = "pr"
	// AnchorKindPath is a campaign-relative path and its content hash.
	AnchorKindPath AnchorKind = "path"
	// AnchorKindFestival is a festival id and its observed status.
	AnchorKindFestival AnchorKind = "festival"
	// AnchorKindWorkitem is another workitem and its observed stage.
	AnchorKindWorkitem AnchorKind = "workitem"
)

// AnchorKinds returns the anchor kind vocabulary.
func AnchorKinds() []string {
	return []string{
		string(AnchorKindPR),
		string(AnchorKindPath),
		string(AnchorKindFestival),
		string(AnchorKindWorkitem),
	}
}

// ObservedUncheckedOffline is what refresh records for an anchor it could not
// re-check without network or credentials. An anchor that cannot be verified
// says so; it never passes silently.
const ObservedUncheckedOffline = "unchecked-offline"

// PathHashPrefix is the algorithm prefix a path anchor's hash carries.
const PathHashPrefix = "sha256:"

// Anchor is one fact with an observed value that the refresh phase re-checks
// mechanically. Anchors are what make a verdict expire honestly: when a PR
// merges or a file's hash changes, the verdict that depended on it goes stale
// instead of applying against a world that moved.
//
// It is a union tagged by Kind. Each kind owns a subset of the fields below
// and every rule about that subset lives in one switch (validateKind), so
// adding a kind is: add the constant, add its fields, add one case.
type Anchor struct {
	Kind AnchorKind `json:"kind"`

	// Repo and Number identify a pr anchor; SHA is the optional head commit.
	Repo   string `json:"repo,omitempty"`
	Number int    `json:"number,omitempty"`
	SHA    string `json:"sha,omitempty"`

	// Path and Hash identify a path anchor. Hash is that anchor's observed
	// value: content is the fact.
	Path string `json:"path,omitempty"`
	Hash string `json:"hash,omitempty"`

	// ID identifies a festival anchor.
	ID string `json:"id,omitempty"`
	// StableID identifies a workitem anchor.
	StableID string `json:"stable_id,omitempty"`

	// Observed is the recorded value for pr and festival anchors, or
	// ObservedUncheckedOffline.
	Observed string `json:"observed,omitempty"`
	// ObservedStage is the recorded value for workitem anchors.
	ObservedStage string `json:"observed_stage,omitempty"`
}

// Validate returns every rule this anchor violates.
func (a Anchor) Validate() []Violation {
	return a.validate("anchor")
}

// validate reports violations prefixed with the anchor's path in its parent
// document.
func (a Anchor) validate(path string) []Violation {
	switch a.Kind {
	case AnchorKindPR:
		return a.validateKind(path, anchorFieldsPR)
	case AnchorKindPath:
		return a.validateKind(path, anchorFieldsPath)
	case AnchorKindFestival:
		return a.validateKind(path, anchorFieldsFestival)
	case AnchorKindWorkitem:
		return a.validateKind(path, anchorFieldsWorkitem)
	default:
		return checkEnum(joinPath(path, "kind"), string(a.Kind), AnchorKinds())
	}
}

// anchorFields names the fields one kind requires and, by omission, the ones
// it forbids. Keeping the sets as data means the "field does not belong to
// this kind" rule is written once instead of per kind.
type anchorFields struct {
	required map[string]bool
	optional map[string]bool
}

var (
	anchorFieldsPR = anchorFields{
		required: set("repo", "number", "observed"),
		optional: set("sha"),
	}
	anchorFieldsPath = anchorFields{
		required: set("path", "hash"),
	}
	anchorFieldsFestival = anchorFields{
		required: set("id", "observed"),
	}
	anchorFieldsWorkitem = anchorFields{
		required: set("stable_id", "observed_stage"),
	}
)

// validateKind checks the required fields for a kind are present, the fields
// that belong to other kinds are absent, and the per-kind value rules hold.
func (a Anchor) validateKind(path string, fields anchorFields) []Violation {
	var out []Violation
	present := a.presentFields()

	for _, name := range anchorFieldNames {
		if fields.required[name] {
			if !present[name] {
				out = append(out, Violation{
					Field:   joinPath(path, name),
					Message: "is required for kind " + quote(string(a.Kind)),
				})
			}
			continue
		}
		if fields.optional[name] {
			continue
		}
		if present[name] {
			out = append(out, Violation{
				Field:   joinPath(path, name),
				Message: "is not valid for kind " + quote(string(a.Kind)),
			})
		}
	}

	switch a.Kind {
	case AnchorKindPR:
		if a.Repo != "" && strings.Count(a.Repo, "/") != 1 {
			out = append(out, Violation{
				Field:   joinPath(path, "repo"),
				Message: "must be owner/name, got " + quote(a.Repo),
			})
		}
		if a.Number < 0 {
			out = append(out, checkMinInt(joinPath(path, "number"), a.Number, 1)...)
		}
	case AnchorKindPath:
		if a.Hash != "" && !strings.HasPrefix(a.Hash, PathHashPrefix) {
			out = append(out, Violation{
				Field:   joinPath(path, "hash"),
				Message: "must start with " + quote(PathHashPrefix),
			})
		}
		if strings.HasPrefix(a.Path, "/") {
			out = append(out, Violation{
				Field:   joinPath(path, "path"),
				Message: "must be campaign-relative, got an absolute path",
			})
		}
		if a.Path == ".." || strings.HasPrefix(a.Path, "../") {
			out = append(out, Violation{
				Field:   joinPath(path, "path"),
				Message: "must stay inside the campaign root",
			})
		}
	}
	return out
}

// anchorFieldNames is every union field, in declaration order, so diagnostics
// come out in a stable order regardless of which kind is being checked.
var anchorFieldNames = []string{
	"repo", "number", "sha", "path", "hash", "id", "stable_id",
	"observed", "observed_stage",
}

// presentFields reports which union fields carry a value. Number is treated as
// present when non-zero, which is also the JSON omitempty rule.
func (a Anchor) presentFields() map[string]bool {
	return map[string]bool{
		"repo":           a.Repo != "",
		"number":         a.Number != 0,
		"sha":            a.SHA != "",
		"path":           a.Path != "",
		"hash":           a.Hash != "",
		"id":             a.ID != "",
		"stable_id":      a.StableID != "",
		"observed":       a.Observed != "",
		"observed_stage": a.ObservedStage != "",
	}
}

// set builds a lookup from a list of names.
func set(names ...string) map[string]bool {
	out := make(map[string]bool, len(names))
	for _, n := range names {
		out[n] = true
	}
	return out
}
