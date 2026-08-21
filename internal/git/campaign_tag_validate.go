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
	anchor func(TagComponents) string
}

// tagDependencies lists every parent a segment needs, including the
// transitive one: a sequence is anchored by its phase and, through it, by the
// festival ref, so a sequence carrying neither reports both breaks.
var tagDependencies = []tagDependency{
	{
		field: "phase", parent: "festival",
		value:  func(tc TagComponents) string { return tc.Phase },
		anchor: func(tc TagComponents) string { return tc.FestRef },
	},
	{
		field: "sequence", parent: "phase",
		value:  func(tc TagComponents) string { return tc.Sequence },
		anchor: func(tc TagComponents) string { return tc.Phase },
	},
	{
		field: "sequence", parent: "festival",
		value:  func(tc TagComponents) string { return tc.Sequence },
		anchor: func(tc TagComponents) string { return tc.FestRef },
	},
}

// broken reports whether tc carries this segment without its parent.
func (d tagDependency) broken(tc TagComponents) bool {
	return d.value(tc) != "" && d.anchor(tc) == ""
}

// reason is the wording shared by the parse warning and the validation error.
func (d tagDependency) reason() string {
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
				Field: d.field, Value: d.value(tc), Reason: d.reason(),
			})
		}
	}
	return out
}

// ValidateTagComponents reports what FormatTag would refuse to emit verbatim.
// FormatTag is lenient by contract (it never fails, and drops what it cannot
// emit), which leaves a caller no signal that a component went missing; this
// is that signal, and callers that care should run it first.
//
// It returns a joined error naming every problem, or nil when FormatTag would
// emit tc in full: a missing campaign id (which suppresses the tag entirely)
// or one carrying a dash (which the parser truncates), a value that fails its
// segment's shape check, and a phase or sequence whose parent segment is
// absent. Each problem is a *camperrors.ValidationError carrying the field
// name.
func ValidateTagComponents(tc TagComponents) error {
	var problems []error

	switch shortID := shortCampaignID(tc.CampaignID); {
	case shortID == "":
		problems = append(problems, camperrors.NewValidation(
			"campaign_id", "required: FormatTag emits nothing without it", nil))
	case strings.Contains(shortID, "-"):
		problems = append(problems, camperrors.NewValidation("campaign_id",
			"truncated on reparse: "+strconv.Quote(shortID)+
				" contains a dash, and the parser ends the campaign id at the first one", nil))
	}

	for _, s := range tagSegments {
		value := tagSegmentValue(s, tc)
		if value == "" || s.shape.MatchString(value) {
			continue
		}
		problems = append(problems, camperrors.NewValidation(s.field,
			"dropped on emit: "+strconv.Quote(value)+" fails its shape check (want "+s.want+")", nil))
	}

	for _, d := range tagDependencies {
		if d.broken(tc) {
			problems = append(problems, camperrors.NewValidation(d.field,
				"dropped on emit: "+d.reason(), nil))
		}
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
