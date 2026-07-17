package workitem

import (
	"sort"
	"testing"
)

func findingCodes(findings []validateFinding) []string {
	codes := make([]string, 0, len(findings))
	for _, f := range findings {
		codes = append(codes, f.Code)
	}
	sort.Strings(codes)
	return codes
}

func hasCode(findings []validateFinding, code string) bool {
	for _, f := range findings {
		if f.Code == code {
			return true
		}
	}
	return false
}

func TestClassifyMarker(t *testing.T) {
	const relPath = "workflow/design/foo"
	const pathType = "design"

	cases := []struct {
		name      string
		present   bool
		raw       string
		wantCodes []string
	}{
		{
			name:      "missing marker",
			present:   false,
			wantCodes: []string{codeMarkerMissing},
		},
		{
			name:      "unparseable marker",
			present:   true,
			raw:       "version: v1alpha7\n[not: yaml{{{\n",
			wantCodes: []string{codeMarkerMalformed},
		},
		{
			name:      "valid current marker",
			present:   true,
			raw:       "version: v1alpha7\nkind: workitem\nid: design-foo-2026-05-25\ntype: design\ntitle: Foo\nref: WI-abc123\n",
			wantCodes: nil,
		},
		{
			name:      "missing ref only",
			present:   true,
			raw:       "version: v1alpha7\nkind: workitem\nid: design-foo-2026-05-25\ntype: design\ntitle: Foo\n",
			wantCodes: []string{codeMissingRefField},
		},
		{
			name:      "legacy accepted version",
			present:   true,
			raw:       "version: v1alpha5\nkind: workitem\nid: design-foo-2026-05-25\ntype: design\ntitle: Foo\nref: WI-abc123\n",
			wantCodes: []string{codeSchemaOutdated},
		},
		{
			name:      "type mismatch",
			present:   true,
			raw:       "version: v1alpha7\nkind: workitem\nid: design-foo-2026-05-25\ntype: feature\ntitle: Foo\nref: WI-abc123\n",
			wantCodes: []string{codeTypeMismatch},
		},
		{
			name:      "unsupported version is malformed",
			present:   true,
			raw:       "version: v1alpha1\nkind: workitem\nid: design-foo-2026-05-25\ntype: design\nref: WI-abc123\n",
			wantCodes: []string{codeMarkerMalformed},
		},
		{
			name:      "empty id and wrong kind are malformed",
			present:   true,
			raw:       "version: v1alpha7\nkind: note\ntype: design\nref: WI-abc123\n",
			wantCodes: []string{codeMarkerMalformed},
		},
		{
			name:      "invalid ref shape is malformed",
			present:   true,
			raw:       "version: v1alpha7\nkind: workitem\nid: design-foo-2026-05-25\ntype: design\nref: nope\n",
			wantCodes: []string{codeMarkerMalformed},
		},
		{
			name:    "legacy marker with mismatch and missing ref",
			present: true,
			raw:     "version: v1alpha5\nkind: workitem\nid: design-foo-2026-05-25\ntype: feature\ntitle: Foo\n",
			wantCodes: []string{
				codeMissingRefField,
				codeSchemaOutdated,
				codeTypeMismatch,
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			findings := classifyMarker(relPath, pathType, c.present, []byte(c.raw))
			got := findingCodes(findings)
			want := append([]string(nil), c.wantCodes...)
			sort.Strings(want)
			if len(got) != len(want) {
				t.Fatalf("codes = %v, want %v", got, want)
			}
			for i := range got {
				if got[i] != want[i] {
					t.Fatalf("codes = %v, want %v", got, want)
				}
			}
			for _, f := range findings {
				if f.Target != relPath {
					t.Errorf("finding target = %q, want %q", f.Target, relPath)
				}
				if f.RepairCommand != repairCommandFor(relPath) {
					t.Errorf("finding repair_command = %q, want %q", f.RepairCommand, repairCommandFor(relPath))
				}
			}
		})
	}
}

func TestClassifyMarker_UnparseableNotRepairable(t *testing.T) {
	findings := classifyMarker("workflow/design/foo", "design", true, []byte("[not yaml{{{"))
	if len(findings) != 1 {
		t.Fatalf("findings = %d, want 1", len(findings))
	}
	if findings[0].Repairable {
		t.Error("unparseable marker finding must be non-repairable")
	}
}

func TestClassifyMarker_MissingIsRepairable(t *testing.T) {
	findings := classifyMarker("workflow/design/foo", "design", false, nil)
	if len(findings) != 1 || !findings[0].Repairable {
		t.Fatalf("missing marker must be a single repairable finding, got %+v", findings)
	}
	if !hasCode(findings, codeMarkerMissing) {
		t.Errorf("want %s, got %v", codeMarkerMissing, findingCodes(findings))
	}
}
