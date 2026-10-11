package explore

import (
	"errors"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// mergeBesideMappingKey makes yaml.v3's struct decoder panic: it hashes every
// key of a mapping that holds a merge key, and a mapping cannot be hashed.
const mergeBesideMappingKey = "<<: {x: y}\n? {a: b}\n: v\n"

func TestParseMetadataDoesNotPanicOnAMergeBesideAMappingKey(t *testing.T) {
	for name, doc := range map[string]string{
		"document":      mergeBesideMappingKey + "id: kept\ntitle: Kept title\nref: WI-kept\n",
		"metadata":      "metadata:\n  <<: {x: y}\n  ? {a: b}\n  : v\n  id: kept\n  name: Kept title\n",
		"history entry": "metadata:\n  id: kept\n  name: Kept title\n  status_history:\n    - <<: {x: y}\n      ? {a: b}\n      : v\n      status: dungeon/completed\n      timestamp: 2026-08-01T10:00:00-06:00\n",
	} {
		t.Run(name, func(t *testing.T) {
			fest, festErr := parseFestMeta(doc)
			work, workErr := parseWorkitemMeta(doc)
			if name == "document" {
				if work != (workitemMeta{id: "kept", title: "Kept title", ref: "WI-kept"}) || !errors.Is(workErr, errYAMLKey) {
					t.Fatalf("workitem = %+v, %v; want the plain fields and the key reported", work, workErr)
				}
				return
			}
			if fest.id != "kept" || fest.name != "Kept title" || !errors.Is(festErr, errYAMLKey) {
				t.Fatalf("festival = %+v, %v; want the plain fields and the key reported", fest, festErr)
			}
			if name == "history entry" && !reflect.DeepEqual(fest.history, []historyEntry{{Status: "dungeon/completed", Timestamp: "2026-08-01T10:00:00-06:00"}}) {
				t.Fatalf("history = %+v, want the entry read past its odd key", fest.history)
			}
		})
	}
}

// The struct decoder formats one error for every pair of repeated keys: 3000
// of them cost it three seconds and three gigabytes. Here they cost one pass.
func TestParseMetadataReadsRepeatedKeysInOnePass(t *testing.T) {
	const keys = 3000
	for name, doc := range map[string]string{
		"document": "id: first\n" + strings.Repeat("a:\n", keys) + "id: second\n",
		"metadata": "metadata:\n  id: first\n" + strings.Repeat("  a:\n", keys) + "  id: second\n",
	} {
		t.Run(name, func(t *testing.T) {
			var id string
			var err error
			allocated := testing.AllocsPerRun(1, func() {
				if name == "document" {
					var meta workitemMeta
					meta, err = parseWorkitemMeta(doc)
					id = meta.id
					return
				}
				var meta festMeta
				meta, err = parseFestMeta(doc)
				id = meta.id
			})
			if id != "first" {
				t.Fatalf("id = %q, want the first value of a repeated key", id)
			}
			if err == nil || err.Error() != `key "a" is repeated` {
				t.Fatalf("error = %v, want the first repeated key named once", err)
			}
			// One pass allocates a few values per key. A pass per pair of
			// keys would allocate millions.
			if allocated > 40*keys {
				t.Fatalf("%v allocations for %d keys, want a count that grows with the keys and not with their pairs", allocated, keys)
			}
		})
	}
}

// An alias costs five bytes and can name a mapping of any size, so a file
// inside the read limit can ask for far more work than it has bytes.
func TestParseMetadataStopsWhenAliasesRepeatMoreThanTheFileHolds(t *testing.T) {
	const keys, aliases = 12000, 12000
	var doc strings.Builder
	doc.WriteString("big: &big\n")
	for i := range keys {
		doc.WriteString("  k" + strconv.Itoa(i) + ":\n")
	}
	doc.WriteString("metadata:\n  id: kept\n  status_history:\n")
	for range aliases {
		doc.WriteString("    - *big\n")
	}
	if doc.Len() > metaReadLimit {
		t.Fatalf("fixture is %d bytes, past the %d the feed reads", doc.Len(), metaReadLimit)
	}
	meta, err := parseFestMeta(doc.String())
	if !errors.Is(err, errYAMLTooMany) {
		t.Fatalf("error = %v, want the walk to stop", err)
	}
	if meta.id != "kept" {
		t.Fatalf("id = %q, want the field read before the history", meta.id)
	}
	// Every entry is the same 2*keys nodes. The walk reads entries until it
	// has visited as many nodes as the file may have bytes, and no further.
	if most := metaReadLimit/(2*keys) + 1; len(meta.history) > most {
		t.Fatalf("read %d of %d history entries, want at most %d", len(meta.history), aliases, most)
	}
}

func TestParseMetadataKeepsFieldsBesideABadOne(t *testing.T) {
	fest, err := parseFestMeta("metadata:\n  id: TE0001\n  name:\n    - a list\n  goal: \"  Ship it.  \"\n  status_history: not a list\n")
	if fest.id != "TE0001" || fest.name != "" || fest.goal != "Ship it." || fest.history != nil {
		t.Fatalf("festival = %+v", fest)
	}
	if err == nil || err.Error() != "name is not a single value" {
		t.Fatalf("error = %v, want the first bad field", err)
	}

	for doc, want := range map[string]string{
		"- a\n- b\n":                       "not a list of key: value lines",
		"just a sentence\n":                "not a list of key: value lines",
		"metadata: a sentence\n":           "metadata is not a list of key: value lines",
		"metadata:\n  status_history: x\n": "status_history is not a list",
		"metadata:\n  status_history:\n    - not an entry\n": "status_history is not a list of key: value lines",
	} {
		if _, err := parseFestMeta(doc); err == nil || err.Error() != want {
			t.Errorf("parseFestMeta(%q) error = %v, want %q", doc, err, want)
		}
	}
	for _, doc := range []string{"", "# only a comment\n", "metadata:\n", "metadata: ~\n", "other: value\n", "metadata:\n  status_history: ~\n"} {
		if meta, err := parseFestMeta(doc); err != nil || !reflect.DeepEqual(meta, festMeta{}) {
			t.Errorf("parseFestMeta(%q) = %+v, %v; want nothing and no error", doc, meta, err)
		}
	}
}

func TestParseMetadataReadsValuesAsWritten(t *testing.T) {
	work, err := parseWorkitemMeta("id: 0012\ntitle: &t 2026-10-01\nref: *t\nversion: v1alpha8\ntags: [a, b]\n")
	if err != nil || work != (workitemMeta{id: "0012", title: "2026-10-01", ref: "2026-10-01"}) {
		t.Fatalf("workitem = %+v, %v", work, err)
	}
	work, err = parseWorkitemMeta("id: ~\ntitle: null\nref: \"null\"\n")
	if err != nil || work != (workitemMeta{ref: "null"}) {
		t.Fatalf("workitem = %+v, %v; want a null read as no value and a quoted one as text", work, err)
	}
	if _, err := parseWorkitemMeta("title: [unclosed\n"); err == nil || !strings.HasPrefix(err.Error(), "yaml: ") {
		t.Fatalf("error = %v, want the parser's own", err)
	}
}

func TestParseYAMLReturnsAParserPanicAsAnError(t *testing.T) {
	parser := unmarshalYAML
	unmarshalYAML = func([]byte, any) error { panic("hash of unhashable type") }
	t.Cleanup(func() { unmarshalYAML = parser })

	if _, err := parseFestMeta("metadata:\n  id: x\n"); !errors.Is(err, errYAMLParse) {
		t.Fatalf("parseFestMeta error = %v", err)
	}
	if _, err := parseWorkitemMeta("id: x\n"); !errors.Is(err, errYAMLParse) {
		t.Fatalf("parseWorkitemMeta error = %v", err)
	}
	if _, _, err := splitFrontmatter("---\ntitle: x\n---\nbody\n"); !errors.Is(err, errYAMLParse) {
		t.Fatalf("splitFrontmatter error = %v", err)
	}
}

// The decoder these readers replaced is still the fastest way to say what a
// well-formed file holds. The two must agree on one.
func TestParseMetadataAgreesWithTheDecoderOnAWellFormedFile(t *testing.T) {
	const doc = "version: \"1.0\"\nmetadata:\n    id: AC0001\n    uuid: 92ef6a13\n    name: agent-command-restriction\n    goal: Restrict agent commands\n    created_at: 2026-02-07T00:46:50.84583Z\n    status_history:\n        - status: planned\n          timestamp: 2026-02-07T00:46:50.84583Z\n          notes: Festival created\n        - status: dungeon/completed\n          timestamp: 2026-02-11T09:00:00-07:00\nquality_gates:\n    enabled: true\n"
	var want struct {
		Metadata struct {
			ID      string `yaml:"id"`
			Name    string `yaml:"name"`
			Goal    string `yaml:"goal"`
			History []struct {
				Status    string `yaml:"status"`
				Timestamp string `yaml:"timestamp"`
			} `yaml:"status_history"`
		} `yaml:"metadata"`
	}
	if err := yaml.Unmarshal([]byte(doc), &want); err != nil {
		t.Fatal(err)
	}
	got, err := parseFestMeta(doc)
	if err != nil {
		t.Fatalf("parseFestMeta error = %v", err)
	}
	if got.id != want.Metadata.ID || got.name != want.Metadata.Name || got.goal != want.Metadata.Goal || len(got.history) != len(want.Metadata.History) {
		t.Fatalf("parseFestMeta = %+v, decoder = %+v", got, want.Metadata)
	}
	for i, entry := range want.Metadata.History {
		if got.history[i] != (historyEntry{Status: entry.Status, Timestamp: entry.Timestamp}) {
			t.Fatalf("history[%d] = %+v, decoder = %+v", i, got.history[i], entry)
		}
	}
}

func TestMetadataMergeSemanticsMatchYAMLDecoder(t *testing.T) {
	for name, doc := range map[string]string{
		"inline":          "<<: {title: Merged, ref: WI-merged}\n",
		"alias":           "base: &base {title: Merged, ref: WI-merged}\n<<: *base\n",
		"explicit before": "title: Explicit\n<<: {title: Merged, ref: WI-merged}\n",
		"explicit after":  "<<: {title: Merged, ref: WI-merged}\ntitle: Explicit\n",
		"sequence":        "<<: [{title: First}, {title: Second, ref: WI-second}]\n",
		"nested":          "base: &base {title: Ancestor, ref: WI-base}\n<<: [{<<: *base, title: Child}, {ref: WI-other}]\n",
		"null override":   "<<: {title: Merged, ref: WI-merged}\ntitle: null\n",
		"quoted key":      "'<<': {title: Not merged}\nref: WI-kept\n",
	} {
		t.Run(name, func(t *testing.T) {
			var want struct {
				Title string `yaml:"title"`
				Ref   string `yaml:"ref"`
			}
			if err := yaml.Unmarshal([]byte(doc), &want); err != nil {
				t.Fatal(err)
			}
			got, err := parseWorkitemMeta(doc)
			if err != nil || got.title != want.Title || got.ref != want.Ref {
				t.Fatalf("got %+v, %v; YAML decoder gives %+v", got, err, want)
			}
			fm, err := decodeFrontmatter(doc)
			if err != nil || fm["title"] != want.Title || fm["ref"] != want.Ref {
				t.Fatalf("frontmatter=%v, %v; want %+v", fm, err, want)
			}
		})
	}
}

func TestMetadataMergeRejectsInvalidSources(t *testing.T) {
	for _, source := range []string{"null", "not-a-map", "[null]", "[[{title: nested-list}]]"} {
		got, err := parseWorkitemMeta("title: Kept\n<<: " + source + "\n")
		if !errors.Is(err, errYAMLMerge) || got.title != "Kept" {
			t.Errorf("merge %s: %+v, %v", source, got, err)
		}
	}
	got, err := parseWorkitemMeta("<<: &self {<<: *self, title: Kept}\n")
	if !errors.Is(err, errYAMLMergeCycle) || got.title != "Kept" {
		t.Fatalf("cyclic merge: %+v, %v", got, err)
	}
}

func TestMetadataMergeExpansionIsBounded(t *testing.T) {
	var doc strings.Builder
	doc.WriteString("a0: &a0 {title: Kept}\n")
	for i := 1; i <= 20; i++ {
		previous := strconv.Itoa(i - 1)
		doc.WriteString("a" + strconv.Itoa(i) + ": &a" + strconv.Itoa(i) + " {<<: [*a" + previous + ", *a" + previous + "]}\n")
	}
	doc.WriteString("<<: *a20\n")
	got, err := parseWorkitemMeta(doc.String())
	if !errors.Is(err, errYAMLTooMany) || got.title != "Kept" {
		t.Fatalf("expanded aliases: %+v, %v", got, err)
	}
}

func TestMetadataMergeInFestivalHistory(t *testing.T) {
	meta, err := parseFestMeta("defaults: &defaults {name: Inherited, goal: Ship it}\nmetadata:\n  <<: *defaults\n  id: YE0001\n  status_history:\n    - <<: {status: dungeon/completed, timestamp: '2026-10-10T01:02:03Z'}\n")
	if err != nil || meta.name != "Inherited" || meta.goal != "Ship it" || len(meta.history) != 1 || meta.history[0].Status != "dungeon/completed" {
		t.Fatalf("festival merge: %+v, %v", meta, err)
	}
}
