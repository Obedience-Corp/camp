package explore

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCleanTextStripsFormatCharacters(t *testing.T) {
	const (
		bidiOverride = "\U0000202E"
		zeroWidth    = "\U0000200B"
		byteOrder    = "\U0000FEFF"
		joiner       = "\U0000200D"
		nonJoiner    = "\U0000200C"
	)
	cases := map[string]string{
		"invoice" + bidiOverride + "fdp.exe":                  "invoicefdp.exe",
		"a\U00002066b\U00002067c\U00002068d\U00002069e":       "abcde",
		"zero" + zeroWidth + "width":                          "zerowidth",
		byteOrder + "title":                                   "title",
		"left\U0000200Eright\U0000200Fmark":                   "leftrightmark",
		"soft\U000000ADhyphen":                                "softhyphen",
		"\U0001F469" + joiner + "\U0001F4BB dev":              "\U0001F469" + joiner + "\U0001F4BB dev",
		"\U00000645\U000006CC" + nonJoiner + "\U0000062E":     "\U00000645\U000006CC" + nonJoiner + "\U0000062E",
		"tag\U000E0067\U000E007F and\U00002060word joiner":    "tag andword joiner",
		"plain caf\U000000E9 \U00002713 stays as it is typed": "plain caf\U000000E9 \U00002713 stays as it is typed",
	}
	for in, want := range cases {
		if got := CleanText(in); got != want {
			t.Errorf("CleanText(%q) = %q, want %q", in, got, want)
		}
	}
	doc := byteOrder + "# Title\n\n\tbody" + bidiOverride + " reversed\U00002069\n"
	if got := CleanDocument(doc); got != "# Title\n\n\tbody reversed\n" {
		t.Errorf("CleanDocument = %q", got)
	}
	encoded, err := json.Marshal(Item{Title: CleanText("FA" + bidiOverride + "01" + zeroWidth)})
	if err != nil {
		t.Fatal(err)
	}
	if strings.ContainsAny(string(encoded), bidiOverride+zeroWidth) {
		t.Fatalf("JSON carries a format character: %q", encoded)
	}
}
