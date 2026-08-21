package git

import (
	"errors"
	"strings"
	"testing"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/slug"
)

// warningKeys renders warnings as "field:reason" so a table can pin both
// without a nested struct per case.
func warningKeys(warnings []TagParseWarning) []string {
	var out []string
	for _, w := range warnings {
		out = append(out, w.Field+":"+w.Reason)
	}
	return out
}

func assertKeys(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d entries %v, want %d %v", len(got), got, len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("entry[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestParseTagDetailed_OrphanSegments(t *testing.T) {
	cases := []struct {
		name         string
		subject      string
		wantPhase    string
		wantSequence string
		wantWarnings []string
	}{
		{
			name:      "phase without festival warns and keeps the value",
			subject:   "[obey-campaign:8deed8b4-PH-001] x",
			wantPhase: "001",
			wantWarnings: []string{
				"phase:phase segment without festival",
			},
		},
		{
			name:      "legacy head phase without festival warns too",
			subject:   "[OBEY-CAMPAIGN-8deed8b4-PH-001] x",
			wantPhase: "001",
			wantWarnings: []string{
				"phase:phase segment without festival",
			},
		},
		{
			name:         "phase and sequence without festival each warn once",
			subject:      "[obey-campaign:8deed8b4-PH-001-SQ-02] x",
			wantPhase:    "001",
			wantSequence: "02",
			wantWarnings: []string{
				"phase:phase segment without festival",
				"sequence:sequence segment without festival",
			},
		},
		{
			name:         "sequence with neither parent reports both breaks",
			subject:      "[obey-campaign:8deed8b4-SQ-02] x",
			wantSequence: "02",
			wantWarnings: []string{
				"sequence:sequence segment without phase",
				"sequence:sequence segment without festival",
			},
		},
		{
			name:         "sequence under a festival but no phase warns once",
			subject:      "[obey-campaign:8deed8b4-FE-CC0008-SQ-02] x",
			wantSequence: "02",
			wantWarnings: []string{
				"sequence:sequence segment without phase",
			},
		},
		{
			name:    "a malformed phase is zeroed, so no orphan warning follows",
			subject: "[obey-campaign:8deed8b4-PH-abc] x",
			wantWarnings: []string{
				"phase:shape check failed (want 1 to 4 digits)",
			},
		},
		{
			name:         "fully anchored phase and sequence parse clean",
			subject:      "[obey-campaign:8deed8b4-FE-CC0008-PH-001-SQ-02] x",
			wantPhase:    "001",
			wantSequence: "02",
		},
		{
			name:    "festival alone parses clean",
			subject: "[obey-campaign:8deed8b4-FE-CC0008] x",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, warnings := ParseTagDetailed(tc.subject)
			if got.Phase != tc.wantPhase {
				t.Errorf("Phase = %q, want %q", got.Phase, tc.wantPhase)
			}
			if got.Sequence != tc.wantSequence {
				t.Errorf("Sequence = %q, want %q", got.Sequence, tc.wantSequence)
			}
			assertKeys(t, warningKeys(warnings), tc.wantWarnings)
		})
	}
}

func TestFormatTag_ShapeGuardsOnEmit(t *testing.T) {
	base := TagComponents{CampaignName: "obey-campaign", CampaignID: "8deed8b4", FestRef: "CC0008"}
	withPhaseSeq := func(phase, sequence string) TagComponents {
		tc := base
		tc.Phase, tc.Sequence = phase, sequence
		return tc
	}
	cases := []struct {
		name string
		tc   TagComponents
		want string
	}{
		{
			name: "phase carrying a directory suffix is dropped",
			tc:   withPhaseSeq("001_IMPLEMENT", ""),
			want: "[obey-campaign:8deed8b4-FE-CC0008]",
		},
		{
			name: "phase carrying a dash is dropped rather than truncated",
			tc:   withPhaseSeq("1-2", ""),
			want: "[obey-campaign:8deed8b4-FE-CC0008]",
		},
		{
			name: "non-numeric phase is dropped",
			tc:   withPhaseSeq("abc", ""),
			want: "[obey-campaign:8deed8b4-FE-CC0008]",
		},
		{
			name: "over-long phase is dropped",
			tc:   withPhaseSeq("12345", ""),
			want: "[obey-campaign:8deed8b4-FE-CC0008]",
		},
		{
			name: "a dropped phase takes its sequence with it",
			tc:   withPhaseSeq("001_IMPLEMENT", "02"),
			want: "[obey-campaign:8deed8b4-FE-CC0008]",
		},
		{
			name: "sequence carrying a directory suffix is dropped, phase kept",
			tc:   withPhaseSeq("001", "02_camp_pilot"),
			want: "[obey-campaign:8deed8b4-FE-CC0008-PH-001]",
		},
		{
			name: "sequence carrying a dash is dropped, phase kept",
			tc:   withPhaseSeq("001", "0-2"),
			want: "[obey-campaign:8deed8b4-FE-CC0008-PH-001]",
		},
		{
			name: "sequence without a phase is dropped",
			tc:   withPhaseSeq("", "02"),
			want: "[obey-campaign:8deed8b4-FE-CC0008]",
		},
		{
			name: "phase without a festival is dropped",
			tc:   TagComponents{CampaignName: "obey-campaign", CampaignID: "8deed8b4", Phase: "001", Sequence: "02"},
			want: "[obey-campaign:8deed8b4]",
		},
		{
			name: "well-formed phase and sequence are emitted",
			tc:   withPhaseSeq("001", "02"),
			want: "[obey-campaign:8deed8b4-FE-CC0008-PH-001-SQ-02]",
		},
		{
			name: "single-digit phase and four-digit sequence are both in shape",
			tc:   withPhaseSeq("1", "0002"),
			want: "[obey-campaign:8deed8b4-FE-CC0008-PH-1-SQ-0002]",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := FormatTag(tc.tc); got != tc.want {
				t.Fatalf("FormatTag = %q, want %q", got, tc.want)
			}
		})
	}
}

// adversarialPhaseSequence covers the emit side of the grammar: every value a
// caller might reasonably pass for a phase or sequence, well-formed or not.
// The other components stay well-formed because FormatTag deliberately does
// not shape-guard them (guarding FE-, WI-, NT-, or qst_ would change what
// existing callers emit); ValidateTagComponents is the signal for those.
var adversarialPhaseSequence = []struct {
	phase, sequence string
}{
	{"", ""},
	{"001", ""},
	{"001", "02"},
	{"1", "0002"},
	{"", "02"},
	{"001_IMPLEMENT", ""},
	{"001_IMPLEMENT", "02"},
	{"1-2", ""},
	{"1-2", "3-4"},
	{"abc", "def"},
	{"12345", "02"},
	{"001", "02_camp_pilot"},
	{"001", "0-2"},
	{"001", "99999"},
	{"-", "-"},
	{"0", "0"},
	{"001", "SQ-02"},
	{"PH-001", "02"},
}

// TestFormatTag_GuardedSegmentsNeverEmitWhatTheParserDegrades covers the two
// segments FormatTag shape-guards. The unguarded segments are deliberately
// out of scope: a malformed festival ref, quest id, workitem ref, or note ref
// is emitted as given by design, so a base carrying one would fail this
// property for a reason the guards are not meant to fix.
// ValidateTagComponents is the signal for those.
func TestFormatTag_GuardedSegmentsNeverEmitWhatTheParserDegrades(t *testing.T) {
	bases := []TagComponents{
		{CampaignName: "obey-campaign", CampaignID: "8deed8b4", FestRef: "CC0008"},
		{CampaignID: "8deed8b4", FestRef: "CC0008"},
		{CampaignName: "obey-campaign", CampaignID: "8deed8b4"},
		{
			CampaignName: "obey-campaign", CampaignID: "8deed8b4", QuestID: "qst_abc",
			FestRef: "CC0008", WorkitemRef: "WI-abcdef", NoteRef: "NT-123456",
		},
		{CampaignName: "obey-campaign", CampaignID: "8deed8b4", FestRef: "CC0008", WorkitemRef: "abcdef"},
	}
	for _, base := range bases {
		for _, ps := range adversarialPhaseSequence {
			tc := base
			tc.Phase, tc.Sequence = ps.phase, ps.sequence
			tag := FormatTag(tc)
			if tag == "" {
				t.Fatalf("FormatTag(%+v) emitted nothing", tc)
			}
			got, warnings := ParseTagDetailed(tag + " subject")
			if len(warnings) != 0 {
				t.Errorf("FormatTag(%+v) = %q, which its own parser degrades: %+v", tc, tag, warnings)
				continue
			}
			if again := FormatTag(got); again != tag {
				t.Errorf("emit is not idempotent for %+v: %q -> parse -> %q", tc, tag, again)
			}
		}
	}
}

// validationFields lists the field of every problem in a joined validation
// error, asserting each one is a typed *camperrors.ValidationError.
func validationFields(t *testing.T, err error) []string {
	t.Helper()
	if err == nil {
		return nil
	}
	joined, ok := err.(interface{ Unwrap() []error })
	if !ok {
		t.Fatalf("expected a joined error, got %T: %v", err, err)
	}
	var fields []string
	for _, e := range joined.Unwrap() {
		var ve *camperrors.ValidationError
		if !errors.As(e, &ve) {
			t.Fatalf("expected *camperrors.ValidationError, got %T: %v", e, e)
		}
		fields = append(fields, ve.Field)
	}
	return fields
}

func TestValidateTagComponents(t *testing.T) {
	cases := []struct {
		name       string
		tc         TagComponents
		wantFields []string
	}{
		{
			name:       "missing campaign id suppresses the whole tag",
			tc:         TagComponents{FestRef: "CC0008", Phase: "001"},
			wantFields: []string{"campaign_id"},
		},
		{
			name:       "campaign id with a dash is truncated on reparse",
			tc:         TagComponents{CampaignID: "abc-def"},
			wantFields: []string{"campaign_id"},
		},
		{
			name:       "malformed phase",
			tc:         TagComponents{CampaignID: "8deed8b4", FestRef: "CC0008", Phase: "001_IMPLEMENT"},
			wantFields: []string{"phase"},
		},
		{
			name:       "phase carrying a dash",
			tc:         TagComponents{CampaignID: "8deed8b4", FestRef: "CC0008", Phase: "1-2"},
			wantFields: []string{"phase"},
		},
		{
			name:       "a malformed phase names the sequence it takes with it",
			tc:         TagComponents{CampaignID: "8deed8b4", FestRef: "CC0008", Phase: "abc", Sequence: "02"},
			wantFields: []string{"phase", "sequence"},
		},
		{
			name:       "a valid phase with a malformed sequence names only the sequence",
			tc:         TagComponents{CampaignID: "8deed8b4", FestRef: "CC0008", Phase: "001", Sequence: "02_camp_pilot"},
			wantFields: []string{"sequence"},
		},
		{
			name:       "both malformed names both, once each",
			tc:         TagComponents{CampaignID: "8deed8b4", FestRef: "CC0008", Phase: "abc", Sequence: "xy"},
			wantFields: []string{"phase", "sequence"},
		},
		{
			name:       "malformed sequence",
			tc:         TagComponents{CampaignID: "8deed8b4", FestRef: "CC0008", Phase: "001", Sequence: "02_camp_pilot"},
			wantFields: []string{"sequence"},
		},
		{
			name:       "phase without festival",
			tc:         TagComponents{CampaignID: "8deed8b4", Phase: "001"},
			wantFields: []string{"phase"},
		},
		{
			name:       "phase and sequence without festival",
			tc:         TagComponents{CampaignID: "8deed8b4", Phase: "001", Sequence: "02"},
			wantFields: []string{"phase", "sequence"},
		},
		{
			name:       "sequence with neither parent",
			tc:         TagComponents{CampaignID: "8deed8b4", Sequence: "02"},
			wantFields: []string{"sequence", "sequence"},
		},
		{
			name:       "sequence without phase under a festival",
			tc:         TagComponents{CampaignID: "8deed8b4", FestRef: "CC0008", Sequence: "02"},
			wantFields: []string{"sequence"},
		},
		{
			name:       "malformed quest id, which FormatTag emits unguarded",
			tc:         TagComponents{CampaignID: "8deed8b4", QuestID: "nope"},
			wantFields: []string{"quest_id"},
		},
		{
			name:       "malformed festival ref, which FormatTag emits unguarded",
			tc:         TagComponents{CampaignID: "8deed8b4", FestRef: "CC-0008"},
			wantFields: []string{"fest_ref"},
		},
		{
			name:       "malformed workitem ref is checked in its emitted form",
			tc:         TagComponents{CampaignID: "8deed8b4", WorkitemRef: "zz"},
			wantFields: []string{"workitem_ref"},
		},
		{
			name:       "malformed note ref",
			tc:         TagComponents{CampaignID: "8deed8b4", NoteRef: "NT-ZZZZZZ"},
			wantFields: []string{"note_ref"},
		},
		{
			// The dependency checks ask whether the parent segment is present,
			// not whether it is well formed: a malformed festival ref is still
			// emitted, so it still anchors the sequence, and it is reported on
			// its own line instead.
			name: "every problem at once is reported together",
			tc: TagComponents{
				QuestID: "nope", FestRef: "CC-0008", Sequence: "02", WorkitemRef: "zz",
			},
			wantFields: []string{"campaign_id", "quest_id", "fest_ref", "workitem_ref", "sequence"},
		},
		{
			name:       "a missing festival ref anchors nothing, so both breaks show",
			tc:         TagComponents{QuestID: "nope", Sequence: "02"},
			wantFields: []string{"campaign_id", "quest_id", "sequence", "sequence"},
		},
		{
			name: "fully anchored components validate clean",
			tc: TagComponents{
				CampaignName: "obey-campaign", CampaignID: "8deed8b4", QuestID: "qst_abc",
				FestRef: "CC0008", Phase: "001", Sequence: "02",
				WorkitemRef: "WI-abcdef", NoteRef: "NT-123456",
			},
		},
		{
			name: "bare workitem and note refs are normalized before checking",
			tc:   TagComponents{CampaignID: "8deed8b4", WorkitemRef: "abcdef", NoteRef: "123456"},
		},
		{
			name: "campaign id alone validates clean",
			tc:   TagComponents{CampaignID: "8deed8b4"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateTagComponents(tc.tc)
			if len(tc.wantFields) == 0 && err != nil {
				t.Fatalf("ValidateTagComponents = %v, want nil", err)
			}
			assertKeys(t, validationFields(t, err), tc.wantFields)
		})
	}
}

// normalizedForRoundTrip is what a faithful round trip is allowed to change:
// the emitter truncates the campaign id, slugifies the name into the head or
// falls back to the legacy marker (which carries no name at all), and adds
// the WI- / NT- prefixes to bare refs.
func normalizedForRoundTrip(tc TagComponents) TagComponents {
	want := tc
	want.CampaignID = shortCampaignID(tc.CampaignID)
	if nameSlug := slug.Generate(tc.CampaignName); nameSlug != "" && tagNameStyleIDRe.MatchString(want.CampaignID) {
		want.CampaignName = nameSlug
	} else {
		want.CampaignName = ""
	}
	if want.WorkitemRef != "" {
		want.WorkitemRef = ensureTagPrefix(want.WorkitemRef, tagWorkitemPrefix)
	}
	if want.NoteRef != "" {
		want.NoteRef = ensureTagPrefix(want.NoteRef, tagNotePrefix)
	}
	return want
}

func TestValidateTagComponents_NilMeansFaithfulRoundTripAfterNormalization(t *testing.T) {
	// A clean validation is a promise about FormatTag: the tag reparses with
	// zero warnings and gives every component back, modulo the normalization
	// the emitter applies. It is not a promise that tc is emitted verbatim.
	cases := []TagComponents{
		{CampaignID: "8deed8b4"},
		{CampaignID: "8deed8b4abcdef0123456789"},
		{CampaignName: "obey-campaign", CampaignID: "8deed8b4abcdef0123456789", FestRef: "CC0008", Phase: "001"},
		{CampaignName: "Brainshare Planning", CampaignID: "8deed8b4"},
		{CampaignName: "!!!", CampaignID: "8deed8b4", FestRef: "CC0008", Phase: "001", Sequence: "02"},
		{CampaignName: "obey-campaign", CampaignID: "8deed8b4", FestRef: "CC0008", Phase: "001"},
		{CampaignName: "obey-campaign", CampaignID: "8deed8b4", FestRef: "CC0008", Phase: "001", Sequence: "02"},
		{CampaignID: "8deed8b4", WorkitemRef: "abcdef", NoteRef: "123456"},
		{
			CampaignName: "obey-campaign", CampaignID: "8deed8b4", QuestID: "qst_abc",
			FestRef: "CC0008", Phase: "0001", Sequence: "0002",
			WorkitemRef: "WI-abcdef", NoteRef: "NT-123456",
		},
	}
	for _, tc := range cases {
		if err := ValidateTagComponents(tc); err != nil {
			t.Fatalf("ValidateTagComponents(%+v) = %v, want nil", tc, err)
		}
		want := normalizedForRoundTrip(tc)
		tag := FormatTag(tc)
		got, warnings := ParseTagDetailed(tag + " subject")
		if len(warnings) != 0 {
			t.Errorf("%q reparsed with warnings: %+v", tag, warnings)
		}
		if got != want {
			t.Errorf("%q reparsed as %+v, want %+v", tag, got, want)
		}
	}
}

func TestValidateTagComponents_DistinguishesDroppedFromDegraded(t *testing.T) {
	// The two kinds of problem differ in what the emitted tag contains, so
	// each case asserts the wording and the fact behind it together.
	cases := []struct {
		name      string
		tc        TagComponents
		bad       string
		wantNote  string
		wantInTag bool
	}{
		{
			name:     "a guarded phase is dropped",
			tc:       TagComponents{CampaignID: "8deed8b4", FestRef: "CC0008", Phase: "001_IMPLEMENT"},
			bad:      "001_IMPLEMENT",
			wantNote: "dropped on emit",
		},
		{
			name:     "a guarded sequence is dropped",
			tc:       TagComponents{CampaignID: "8deed8b4", FestRef: "CC0008", Phase: "001", Sequence: "02_camp_pilot"},
			bad:      "02_camp_pilot",
			wantNote: "dropped on emit",
		},
		{
			name:      "an unguarded festival ref reaches the tag",
			tc:        TagComponents{CampaignID: "8deed8b4", FestRef: "RI-XX0001", Phase: "001"},
			bad:       "RI-XX0001",
			wantNote:  "emitted verbatim but reparses degraded",
			wantInTag: true,
		},
		{
			name:      "an unguarded quest id reaches the tag",
			tc:        TagComponents{CampaignID: "8deed8b4", QuestID: "nope"},
			bad:       "nope",
			wantNote:  "emitted verbatim but reparses degraded",
			wantInTag: true,
		},
		{
			name:      "an unguarded workitem ref reaches the tag",
			tc:        TagComponents{CampaignID: "8deed8b4", WorkitemRef: "WI-ZZZZZZ"},
			bad:       "WI-ZZZZZZ",
			wantNote:  "emitted verbatim but reparses degraded",
			wantInTag: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateTagComponents(tc.tc)
			if err == nil {
				t.Fatalf("ValidateTagComponents(%+v) = nil, want an error", tc.tc)
			}
			if !strings.Contains(err.Error(), tc.wantNote) {
				t.Errorf("error %q does not describe the value as %q", err, tc.wantNote)
			}
			tag := FormatTag(tc.tc)
			if got := strings.Contains(tag, tc.bad); got != tc.wantInTag {
				t.Errorf("tag %q contains %q = %v, want %v (the error says %q)",
					tag, tc.bad, got, tc.wantInTag, tc.wantNote)
			}
		})
	}
}
