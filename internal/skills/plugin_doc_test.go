package skills

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

var testCampSlugs = []string{"campaign-commit", "camp-navigation"}

func readProviderFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/provider/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func answer(raw []byte, exited bool) ProviderReport {
	return classifyAnswer(ProviderReport{Plugin: CampAppPlugin}, raw, exited, testCampSlugs)
}

// The real answer of the camp-app build that implements the contract, captured
// from its skill-contract worktree with a fixed request ID.
func TestProviderAnswer_RealCampAppContract(t *testing.T) {
	raw := readProviderFixture(t, "camp-app-contract.json")
	r := answer(raw, false)
	if r.Status != ProviderOK || r.Version != "0.3.0-dirty" {
		t.Fatalf("status = %q (%s), version = %q; want ok, 0.3.0-dirty", r.Status, r.Detail, r.Version)
	}
	b, ok := r.Offered("festival-app")
	if !ok || len(b.Files) != 1 || b.Files[0].Path != "SKILL.md" || b.Plugin != CampAppPlugin {
		t.Fatalf("offered bundle = %+v, want festival-app with SKILL.md", b)
	}
	var env struct {
		Data struct {
			Bundles []struct {
				Files []struct{ SHA256, Content string }
			}
		}
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatal(err)
	}
	if got := b.Files[0]; string(got.Content) != env.Data.Bundles[0].Files[0].Content || got.Hash() != env.Data.Bundles[0].Files[0].SHA256 {
		t.Fatal("bundle content is not exactly what camp-app printed")
	}
}

func TestProviderAnswer_NoContract(t *testing.T) {
	cases := map[string]struct {
		raw        []byte
		exited     bool
		campBehind bool
	}{
		"camp-app 0.2.1 has no skill command":  {raw: readProviderFixture(t, "camp-app-0.2.1.json"), exited: true},
		"camp-app 0.3.0 prints name and files": {raw: readProviderFixture(t, "camp-app-0.3.0.json")},
		"refusal envelope with exit 0":         {raw: []byte(`{"ok":false,"data":null}`)},
		"document without bundles":             {raw: []byte(`{"schemaVersion":"camp-plugin-skills/v1alpha1","plugin":"camp-app","version":"1.0.0"}`)},
		"newer contract": {
			raw:        []byte(`{"schemaVersion":"camp-plugin-skills/v2","plugin":"camp-app","version":"9.0.0","bundles":[]}`),
			campBehind: true,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			r := answer(tc.raw, tc.exited)
			if r.Status != ProviderNoContract || r.CampBehind != tc.campBehind {
				t.Fatalf("status = %q, campBehind = %v (%s); want no-contract, %v", r.Status, r.CampBehind, r.Detail, tc.campBehind)
			}
		})
	}
	old := answer(readProviderFixture(t, "camp-app-0.3.0.json"), false).SkipLine()
	if old != "skipped skills from camp-app: this camp-app does not offer skills to camp; update camp-app" {
		t.Errorf("old camp-app line = %q", old)
	}
	newer := answer([]byte(`{"schemaVersion":"x/v9","bundles":[]}`), false).SkipLine()
	if newer != "skipped skills from camp-app: it offers a newer skill contract than this camp reads; update camp" {
		t.Errorf("newer contract line = %q", newer)
	}
}

// contractDoc returns the real contract document as a mutable map.
func contractDoc(t *testing.T) map[string]any {
	t.Helper()
	var env map[string]any
	if err := json.Unmarshal(readProviderFixture(t, "camp-app-contract.json"), &env); err != nil {
		t.Fatal(err)
	}
	return env["data"].(map[string]any)
}

func docBundle(doc map[string]any) map[string]any {
	return doc["bundles"].([]any)[0].(map[string]any)
}

func docFile(doc map[string]any) map[string]any {
	return docBundle(doc)["files"].([]any)[0].(map[string]any)
}

func setFile(f map[string]any, path, content string) {
	f["path"], f["content"], f["sha256"] = path, content, HashContent([]byte(content))
}

func addFile(doc map[string]any, path, content string) {
	f := map[string]any{}
	setFile(f, path, content)
	b := docBundle(doc)
	b["files"] = append(b["files"].([]any), f)
}

func TestProviderAnswer_InvalidDocuments(t *testing.T) {
	skill := func(name, desc string) string {
		return "---\nname: " + name + "\ndescription: " + desc + "\n---\nbody\n"
	}
	cases := map[string]func(map[string]any){
		"wrong plugin":            func(d map[string]any) { d["plugin"] = "camp-other" },
		"no version":              func(d map[string]any) { d["version"] = "" },
		"bad bundle name":         func(d map[string]any) { docBundle(d)["name"] = "Festival_App" },
		"hidden bundle name":      func(d map[string]any) { docBundle(d)["name"] = ".backup" },
		"camp ships the name":     func(d map[string]any) { docBundle(d)["name"] = "campaign-commit" },
		"no SKILL.md at the root": func(d map[string]any) { setFile(docFile(d), "docs/SKILL.md", skill("festival-app", "x")) },
		"frontmatter name differs": func(d map[string]any) {
			setFile(docFile(d), "SKILL.md", skill("other", "x"))
		},
		"empty description":    func(d map[string]any) { setFile(docFile(d), "SKILL.md", skill("festival-app", "")) },
		"unclosed frontmatter": func(d map[string]any) { setFile(docFile(d), "SKILL.md", "---\nname: festival-app\ndescription: x\n") },
		"no frontmatter":       func(d map[string]any) { setFile(docFile(d), "SKILL.md", "name: festival-app\n") },
		"hash mismatch":        func(d map[string]any) { docFile(d)["content"] = "changed" },
		"uppercase hash": func(d map[string]any) {
			docFile(d)["sha256"] = strings.ToUpper(docFile(d)["sha256"].(string))
		},
		"single file not named SKILL.md": func(d map[string]any) { setFile(docFile(d), "skill.md", skill("festival-app", "x")) },
		"too many bundles": func(d map[string]any) {
			for range maxPluginBundles {
				d["bundles"] = append(d["bundles"].([]any), map[string]any{})
			}
		},
		"file too large": func(d map[string]any) {
			setFile(docFile(d), "SKILL.md", skill("festival-app", "x")+strings.Repeat("a", maxPluginFileBytes))
		},
		"bundle offered twice": func(d map[string]any) { d["bundles"] = append(d["bundles"].([]any), docBundle(d)) },
		"no files":             func(d map[string]any) { docBundle(d)["files"] = []any{} },
		"version is number":    func(d map[string]any) { d["version"] = 4 },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			doc := contractDoc(t)
			mutate(doc)
			raw, err := json.Marshal(map[string]any{"ok": true, "data": doc})
			if err != nil {
				t.Fatal(err)
			}
			if r := answer(raw, false); r.Status != ProviderInvalid || r.Detail == "" {
				t.Fatalf("status = %q (%s), want invalid with a detail", r.Status, r.Detail)
			}
		})
	}
}

// v1 installs only SKILL.md. A bundle offering more is not broken: this camp
// is behind the plugin, so it reports no contract and says to update camp.
func TestProviderAnswer_MoreThanSkillMeansUpdateCamp(t *testing.T) {
	for name, mutate := range map[string]func(map[string]any){
		"SKILL.md and a reference": func(d map[string]any) { addFile(d, "references/usage.md", "x") },
		"SKILL.md and a hidden or odd path": func(d map[string]any) {
			addFile(d, "../escape.md", "x")
		},
		"SKILL.md and a case twin": func(d map[string]any) { addFile(d, "skill.md", "x") },
	} {
		t.Run(name, func(t *testing.T) {
			doc := contractDoc(t)
			mutate(doc)
			raw, err := json.Marshal(map[string]any{"ok": true, "data": doc})
			if err != nil {
				t.Fatal(err)
			}
			r := answer(raw, false)
			if r.Status != ProviderNoContract || !r.CampBehind || len(r.Bundles) != 0 {
				t.Fatalf("status = %q (%s), campBehind = %v; want no-contract, camp behind, nothing offered", r.Status, r.Detail, r.CampBehind)
			}
			if got := r.SkipLine(); got != "skipped skills from camp-app: it offers a skill with more than SKILL.md, which this camp does not install; update camp" {
				t.Fatalf("line = %q", got)
			}
		})
	}
}

func TestProviderAnswer_InvalidStreams(t *testing.T) {
	valid := readProviderFixture(t, "camp-app-contract.json")
	cases := map[string]struct {
		raw    []byte
		exited bool
	}{
		"garbage":                   {raw: []byte("panic: runtime error")},
		"empty":                     {raw: nil},
		"two JSON values":           {raw: append(append([]byte{}, valid...), valid...)},
		"trailing garbage":          {raw: append(append([]byte{}, valid...), []byte("x")...)},
		"top-level array":           {raw: []byte(`[1]`)},
		"invalid UTF-8":             {raw: append([]byte(`{"x":"`), 0xff, '"', '}')},
		"success envelope, exit 1":  {raw: valid, exited: true},
		"bare document, exit 1":     {raw: []byte(`{"schemaVersion":"camp-plugin-skills/v1alpha1","bundles":[]}`), exited: true},
		"garbage, exit 2":           {raw: []byte("Error: unknown"), exited: true},
		"data is not an object":     {raw: []byte(`{"ok":true,"data":[1]}`)},
		"ok is not a boolean":       {raw: []byte(`{"ok":"yes","data":{}}`)},
		"lone surrogate in content": {raw: surrogateContent(t)},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if r := answer(tc.raw, tc.exited); r.Status != ProviderInvalid {
				t.Fatalf("status = %q (%s), want invalid", r.Status, r.Detail)
			}
		})
	}
}

// surrogateContent escapes a lone surrogate into SKILL.md. Go decodes it to
// U+FFFD, so the content no longer matches the hash camp-app computed.
func surrogateContent(t *testing.T) []byte {
	doc := contractDoc(t)
	content := docFile(doc)["content"].(string) + "MARK"
	docFile(doc)["content"] = content
	docFile(doc)["sha256"] = HashContent([]byte(content))
	raw, err := json.Marshal(map[string]any{"ok": true, "data": doc})
	if err != nil {
		t.Fatal(err)
	}
	return []byte(strings.Replace(string(raw), "MARK", `\ud800`, 1))
}

func TestCheckSkillFrontmatter_QuotedValues(t *testing.T) {
	content := "---\r\nname: \"festival-app\"\r\ndescription: 'Drive the app'\r\nextra: x: y\r\n---\r\n"
	if err := checkSkillFrontmatter(content, "festival-app"); err != nil {
		t.Fatalf("checkSkillFrontmatter() = %v", err)
	}
}
