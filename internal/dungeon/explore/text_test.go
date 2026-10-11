package explore

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestCleanTextStripsControls(t *testing.T) {
	cases := map[string]string{
		"FA\u009b31m01":                    "FA31m01",
		"evil\x1b]52;c;ZXZpbA==\x07name":   "evilname",
		"red\x1b[31m text\x1b[0m":          "red text",
		"line\none\ttab\rreturn\u0085next": "line one tab returnnext",
		"café ✓":                           "café ✓",
	}
	for in, want := range cases {
		if got := CleanText(in); got != want {
			t.Errorf("CleanText(%q) = %q, want %q", in, got, want)
		}
	}
	if got := CleanDocument("# Title\n\n\tbody\u009b\x1b[2J\r\n"); got != "# Title\n\n\tbody\n" {
		t.Errorf("CleanDocument = %q", got)
	}
	encoded, err := json.Marshal(Item{ID: CleanText("FA\u009b01")})
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encoded, []byte("\u009b")) {
		t.Fatalf("JSON carries a raw C1 control: %q", encoded)
	}
}
