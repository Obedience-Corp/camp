package workitem

import "testing"

func TestParseFrontmatterHead(t *testing.T) {
	cases := []struct {
		name     string
		head     string
		wantOK   bool
		wantYAML string
	}{
		{"valid LF block", "---\nkind: workitem\nid: x\n---\n\n# Body", true, "kind: workitem\nid: x"},
		{"valid CRLF block keeps interior bytes", "---\r\nkind: workitem\r\n---\r\n", true, "kind: workitem\r"},
		{"no leading delimiter is not frontmatter", "kind: workitem\n---\n", false, ""},
		{"leading delimiter with no closing delimiter", "---\nkind: workitem\nno close here\n", false, ""},
		{"empty input", "", false, ""},
		{"only the opening delimiter", "---\n", false, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parseFrontmatterHead([]byte(tc.head))
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOK)
			}
			if ok && string(got) != tc.wantYAML {
				t.Errorf("block = %q, want %q", got, tc.wantYAML)
			}
		})
	}
}
