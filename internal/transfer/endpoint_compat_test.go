package transfer

import "testing"

// TestEndpointCompatMatrix pins every currently valid endpoint form BEFORE the
// machine-first grammar exists, which is the order the festival constraints
// require: write the matrix against today's parser, watch it pass, then change
// the parser and watch only the intended rows move.
//
// Rows map one-to-one onto transfer-grammar-spec.md section 6.
func TestEndpointCompatMatrix(t *testing.T) {
	tests := []struct {
		row      string
		spec     string
		head     string
		rest     string
		hasColon bool
	}{
		{row: "B1", spec: "docs/x.md", head: "docs/x.md"},
		{row: "B2", spec: "./docs/x.md", head: "./docs/x.md"},
		{row: "B3", spec: "/abs/path/x.md", head: "/abs/path/x.md"},
		{row: "B4", spec: "other:docs/x.md", head: "other", rest: "docs/x.md", hasColon: true},
		{row: "B5", spec: "8dee:docs/x.md", head: "8dee", rest: "docs/x.md", hasColon: true},
		{row: "B6", spec: "8deed8b4-aaaa:docs/x.md", head: "8deed8b4-aaaa", rest: "docs/x.md", hasColon: true},
		{row: "B7", spec: "other:", head: "other", rest: "", hasColon: true},
		{row: "B8", spec: "other:festivals/plan.md", head: "other", rest: "festivals/plan.md", hasColon: true},
		// The split is at the FIRST colon, so a path containing a colon stays
		// part of the path.
		{row: "B9", spec: "other:a:b", head: "other", rest: "a:b", hasColon: true},
		{row: "B10", spec: "weird:name.md", head: "weird", rest: "name.md", hasColon: true},
		{row: "B16", spec: "local:docs/x.md", head: "local", rest: "docs/x.md", hasColon: true},
	}

	for _, tt := range tests {
		t.Run(tt.row+"_"+tt.spec, func(t *testing.T) {
			head, rest, hasColon := parseSpec(tt.spec)
			if head != tt.head || rest != tt.rest || hasColon != tt.hasColon {
				t.Errorf("parseSpec(%q) = (%q, %q, %v), want (%q, %q, %v)",
					tt.spec, head, rest, hasColon, tt.head, tt.rest, tt.hasColon)
			}
		})
	}
}

// TestParseEndpointCompatAfterChange re-runs the same matrix through the NEW
// grammar with a fleet configured, and asserts that only the two rows the spec
// flagged as behavior changes actually moved.
func TestParseEndpointCompatAfterChange(t *testing.T) {
	fleet := func(id string) bool { return id == "archdtop" }
	noCampaigns := func(string) bool { return false }

	unchanged := []struct {
		row      string
		spec     string
		wantSpec string
	}{
		{"B1", "docs/x.md", "docs/x.md"},
		{"B2", "./docs/x.md", "./docs/x.md"},
		{"B3", "/abs/path/x.md", "/abs/path/x.md"},
		{"B4", "other:docs/x.md", "other:docs/x.md"},
		{"B5", "8dee:docs/x.md", "8dee:docs/x.md"},
		{"B7", "other:", "other:"},
		{"B8", "other:festivals/plan.md", "other:festivals/plan.md"},
		{"B9", "other:a:b", "other:a:b"},
		{"B10", "weird:name.md", "weird:name.md"},
		// B15: a machine NAME that is not registered still falls through.
		{"B15", "archdtop-typo:docs/x.md", "archdtop-typo:docs/x.md"},
	}
	for _, tt := range unchanged {
		t.Run(tt.row+"_unchanged", func(t *testing.T) {
			got, err := ParseEndpoint(tt.spec, fleet, noCampaigns)
			if err != nil {
				t.Fatalf("ParseEndpoint(%q) errored: %v", tt.spec, err)
			}
			if got.IsRemote() {
				t.Errorf("ParseEndpoint(%q) became remote: %+v", tt.spec, got)
			}
			if got.Spec != tt.wantSpec {
				t.Errorf("ParseEndpoint(%q).Spec = %q, want %q", tt.spec, got.Spec, tt.wantSpec)
			}
		})
	}

	// B14: the ONE behavior change. A registered machine id as the head used to
	// fail a campaign lookup; it now errors asking for the campaign segment.
	t.Run("B14_changed", func(t *testing.T) {
		_, err := ParseEndpoint("archdtop:docs/x.md", fleet, noCampaigns)
		if err == nil {
			t.Fatal("want the missing-campaign error")
		}
		for _, want := range []string{"is a machine", "machine:campaign:path", "archdtop"} {
			if !contains(err.Error(), want) {
				t.Errorf("error %q missing %q", err.Error(), want)
			}
		}
	})

	// B16: local: is new capability, previously an error.
	t.Run("B16_new", func(t *testing.T) {
		got, err := ParseEndpoint("local:docs/x.md", fleet, noCampaigns)
		if err != nil {
			t.Fatal(err)
		}
		if got.IsRemote() || got.Spec != "docs/x.md" {
			t.Errorf("local: should force the campaign reading, got %+v", got)
		}
	})
}

func TestParseEndpointNewForms(t *testing.T) {
	fleet := func(id string) bool { return id == "archdtop" || id == "other" }

	t.Run("N1_remote_endpoint", func(t *testing.T) {
		got, err := ParseEndpoint("archdtop:obey-campaign:docs/x.md", fleet, nil)
		if err != nil {
			t.Fatal(err)
		}
		if got.Machine != "archdtop" || got.Campaign != "obey-campaign" || got.Path != "docs/x.md" {
			t.Errorf("got %+v", got)
		}
	})

	t.Run("N5_first_colon_applies_to_the_remainder_too", func(t *testing.T) {
		got, err := ParseEndpoint("archdtop:obey-campaign:a:b", fleet, nil)
		if err != nil {
			t.Fatal(err)
		}
		if got.Campaign != "obey-campaign" || got.Path != "a:b" {
			t.Errorf("got %+v", got)
		}
	})

	t.Run("N6_local_disambiguates_a_shadowed_name", func(t *testing.T) {
		got, err := ParseEndpoint("local:other:docs/x.md", fleet, nil)
		if err != nil {
			t.Fatal(err)
		}
		if got.IsRemote() {
			t.Errorf("local: must win over the machine reading, got %+v", got)
		}
		if got.Spec != "other:docs/x.md" {
			t.Errorf("Spec = %q", got.Spec)
		}
	})

	t.Run("shadowing_is_reported", func(t *testing.T) {
		isCampaign := func(head string) bool { return head == "archdtop" }
		got, err := ParseEndpoint("archdtop:obey-campaign:x.md", fleet, isCampaign)
		if err != nil {
			t.Fatal(err)
		}
		if !got.Shadowed {
			t.Error("a head that is both a machine and a campaign must be flagged")
		}
		note := ShadowNote(got.Machine)
		for _, want := range []string{"registered machine", "local:archdtop:"} {
			if !contains(note, want) {
				t.Errorf("note %q missing %q", note, want)
			}
		}
	})

	errorRows := []struct {
		name string
		spec string
	}{
		{"E1_machine_without_campaign", "archdtop:notes.md"},
		{"E2_empty_path", "archdtop:obey-campaign:"},
		{"E3_empty_campaign", "archdtop::notes.md"},
	}
	for _, tt := range errorRows {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := ParseEndpoint(tt.spec, fleet, nil); err == nil {
				t.Errorf("ParseEndpoint(%q) should error", tt.spec)
			}
		})
	}
}

func contains(haystack, needle string) bool {
	return len(needle) == 0 || (len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0)
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
