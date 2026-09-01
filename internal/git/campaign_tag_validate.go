package git

import (
	"strconv"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// tagDependency is one parent requirement in the festival locator chain: a
// phase indexes into a festival and a sequence into a phase, so a segment
// whose parent is absent locates nothing. Both the parser (as a warning) and
// ValidateTagComponents (as an error) read the chain from here, so the rule
// has one definition.
type tagDependency struct {
	field  string
	parent string
	value  func(TagComponents) string
	// anchor returns the parent value that survives the emit, so a parent
	// that FormatTag would drop reads as no anchor at all.
	anchor func(TagComponents) string
	// present reports whether the parent was supplied, which separates "you
	// gave no phase" from "the phase you gave was dropped".
	present func(TagComponents) bool
}

// tagDependencies lists every parent a segment needs, including the
// transitive one: a sequence is anchored by its phase and, through it, by the
// festival ref, so a sequence carrying neither reports both breaks.
var tagDependencies = []tagDependency{
	{
		field: "phase", parent: "festival",
		value:   func(tc TagComponents) string { return tc.Phase },
		anchor:  func(tc TagComponents) string { return tc.FestRef },
		present: func(tc TagComponents) bool { return tc.FestRef != "" },
	},
	{
		field: "sequence", parent: "phase",
		value:   func(tc TagComponents) string { return tc.Sequence },
		anchor:  validPhase,
		present: func(tc TagComponents) bool { return tc.Phase != "" },
	},
	{
		field: "sequence", parent: "festival",
		value:   func(tc TagComponents) string { return tc.Sequence },
		anchor:  func(tc TagComponents) string { return tc.FestRef },
		present: func(tc TagComponents) bool { return tc.FestRef != "" },
	},
}

// validPhase is the phase a sequence can hang off: one FormatTag will emit. A
// malformed phase is dropped, and it takes its sequence with it, so it cannot
// anchor anything. The parser zeroes a malformed phase before this runs, so
// this only ever differs from tc.Phase on the emit side.
func validPhase(tc TagComponents) string {
	if !tagPhaseRe.MatchString(tc.Phase) {
		return ""
	}
	return tc.Phase
}

// broken reports whether tc carries this segment without its parent.
func (d tagDependency) broken(tc TagComponents) bool {
	return d.value(tc) != "" && d.anchor(tc) == ""
}

// reason is the wording shared by the parse warning and the validation
// error. A parent that was supplied but will not survive the emit gets its
// own phrasing, since "without phase" would be false on a tc that has one.
func (d tagDependency) reason(tc TagComponents) string {
	if d.present(tc) {
		return "its " + d.parent + " was dropped"
	}
	return d.field + " segment without " + d.parent
}

// tagDependencyWarnings reports every broken link in the locator chain. The
// values are kept by the parser rather than zeroed: an unanchored phase or
// sequence is still the best locator the commit carries.
func tagDependencyWarnings(tc TagComponents) []TagParseWarning {
	var out []TagParseWarning
	for _, d := range tagDependencies {
		if d.broken(tc) {
			out = append(out, TagParseWarning{
				Field: d.field, Value: d.value(tc), Reason: d.reason(tc),
			})
		}
	}
	return out
}

// ValidateTagComponents reports where FormatTag's output would not faithfully
// carry tc. FormatTag is lenient by contract: it never fails, and it neither
// rejects nor reports a component it cannot carry, which leaves the caller no
// signal that something went missing. This is that signal.
//
// It returns nil when FormatTag's output reparses with zero warnings and
// components equal to tc after the normalization the emitter applies: the
// campaign id truncated to campaignTagMaxIDLen, the name slugified into the
// head or dropped for the legacy marker, and the WI- / NT- prefixes added to
// bare refs. Nil is therefore not a promise that tc is emitted verbatim (a
// 16-character campaign id validates clean and is still truncated), only that
// nothing is lost that the parser cannot give back.
//
// Otherwise it returns a joined error naming every problem: a missing
// campaign id (which suppresses the tag entirely) or one carrying a dash
// (which the parser truncates), a value that fails its segment's shape check,
// and a phase or sequence whose parent segment is missing or itself dropped.
// The message distinguishes the two ways a bad value goes wrong, because
// FormatTag guards only the phase and sequence: those are dropped, while the
// other segments are written into the tag as given and degrade only when it
// is parsed back.
//
// Each problem is a *camperrors.ValidationError carrying the field name.
// Callers outside camp cannot name that type, but the joined message names
// every field, and they can enumerate the problems by unwrapping the join
// through interface{ Unwrap() []error }.
func ValidateTagComponents(tc TagComponents) error {
	var problems []error

	switch shortID := shortCampaignID(tc.CampaignID); {
	case shortID == "":
		problems = append(problems, camperrors.NewValidation(
			"campaign_id", "required: FormatTag emits nothing without it", nil))
	case strings.Contains(shortID, "-"):
		problems = append(problems, camperrors.NewValidation("campaign_id",
			"truncated on reparse: "+strconv.Quote(shortID)+
				" contains a dash, and the parser ends the camp id at the first one", nil))
	}

	malformed := make(map[string]bool, len(tagSegments))
	for _, s := range tagSegments {
		value := tagSegmentValue(s, tc)
		if value == "" || s.shape.MatchString(value) {
			continue
		}
		malformed[s.field] = true
		problems = append(problems, camperrors.NewValidation(s.field,
			s.fate()+": "+strconv.Quote(value)+" fails its shape check (want "+s.want+")", nil))
	}

	for _, d := range tagDependencies {
		// A segment already reported as malformed is lost for that reason;
		// naming its parent too would say the same thing twice.
		if malformed[d.field] || !d.broken(tc) {
			continue
		}
		problems = append(problems, camperrors.NewValidation(d.field,
			"dropped on emit: "+d.reason(tc), nil))
	}

	return camperrors.Join(problems...)
}

// tagSegmentValue reads this segment's field from tc in the form FormatTag
// would emit, applying the prefix normalization the emitter applies so a bare
// workitem or note ref validates the same way it is written.
func tagSegmentValue(s tagSegment, tc TagComponents) string {
	value := *s.target(&tc)
	if value == "" || !s.normalize {
		return value
	}
	return ensureTagPrefix(value, s.prefix)
}
