package skills

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"slices"
	"strings"
	"unicode/utf8"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// PluginSkillsSchemaVersion is the plugin skills document camp reads.
const PluginSkillsSchemaVersion = "camp-plugin-skills/v1alpha1"

const (
	maxPluginBundles   = 8
	maxPluginFileBytes = 256 << 10
	pluginSkillFile    = "SKILL.md"
)

// PluginBundle is one validated skill bundle a plugin offers. Its files carry
// the bundle name, the plugin and the plugin version.
type PluginBundle struct {
	Name    string
	Plugin  string
	Version string
	Files   []ShippedFile
}

type pluginDocument struct {
	SchemaVersion string            `json:"schemaVersion"`
	Plugin        string            `json:"plugin"`
	Version       string            `json:"version"`
	Bundles       []pluginDocBundle `json:"bundles"`
}

type pluginDocBundle struct {
	Name  string          `json:"name"`
	Files []pluginDocFile `json:"files"`
}

type pluginDocFile struct {
	Path    string `json:"path"`
	SHA256  string `json:"sha256"`
	Content string `json:"content"`
}

// errNoContract marks an answer that holds no skills document camp reads.
var errNoContract = camperrors.New("no skill contract")

// errNewerContract marks a document in a schema newer than this camp reads.
var errNewerContract = camperrors.New("newer skill contract")

// errMoreThanSkill marks a bundle with files beside SKILL.md, which a later
// camp may install and this one does not.
var errMoreThanSkill = camperrors.New("a skill with more than SKILL.md")

// parsePluginAnswer reads what a plugin printed for `skill --json`. It returns
// errNoContract or errNewerContract when the plugin offers no document this
// camp reads, and any other error when the answer is unusable. exited reports
// a non-zero exit, which only a refusal envelope (ok false) can explain.
func parsePluginAnswer(raw []byte, exited bool, plugin string, campSlugs []string) ([]PluginBundle, string, error) {
	if !utf8.Valid(raw) {
		return nil, "", camperrors.New("the answer is not valid UTF-8")
	}
	top, err := oneJSONObject(raw)
	if err != nil {
		return nil, "", err
	}
	docRaw, err := unwrapEnvelope(top, raw)
	if err != nil {
		return nil, "", err
	}
	if exited {
		return nil, "", camperrors.New("it exited with an error")
	}
	return parsePluginDocument(docRaw, plugin, campSlugs)
}

func oneJSONObject(raw []byte) (map[string]json.RawMessage, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	var top map[string]json.RawMessage
	if err := dec.Decode(&top); err != nil || top == nil {
		return nil, camperrors.New("the answer is not one JSON object")
	}
	if err := dec.Decode(&json.RawMessage{}); !errors.Is(err, io.EOF) {
		return nil, camperrors.New("the answer holds more than one JSON value")
	}
	return top, nil
}

// unwrapEnvelope returns the document: the data of a result envelope, an
// object with both ok and data, or else the object itself.
func unwrapEnvelope(top map[string]json.RawMessage, raw []byte) (json.RawMessage, error) {
	okRaw, hasOK := top["ok"]
	data, hasData := top["data"]
	if !hasOK || !hasData {
		return raw, nil
	}
	var ok bool
	if err := json.Unmarshal(okRaw, &ok); err != nil {
		return nil, camperrors.New("the result envelope's ok is not a boolean")
	}
	if !ok {
		return nil, errNoContract
	}
	return data, nil
}

func parsePluginDocument(raw json.RawMessage, plugin string, campSlugs []string) ([]PluginBundle, string, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, "", camperrors.New("the skills document is not a JSON object")
	}
	if _, ok := fields["schemaVersion"]; !ok {
		return nil, "", errNoContract
	}
	if _, ok := fields["bundles"]; !ok {
		return nil, "", errNoContract
	}
	var doc pluginDocument
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, "", camperrors.Wrap(err, "the skills document has a field of the wrong type")
	}
	if doc.SchemaVersion != PluginSkillsSchemaVersion {
		return nil, doc.Version, errNewerContract
	}
	bundles, err := validatePluginDocument(doc, plugin, campSlugs)
	return bundles, doc.Version, err
}

func validatePluginDocument(doc pluginDocument, plugin string, campSlugs []string) ([]PluginBundle, error) {
	switch {
	case doc.Plugin != plugin:
		return nil, camperrors.Newf("the document names plugin %q, not %q", doc.Plugin, plugin)
	case doc.Version == "":
		return nil, camperrors.New("the document has no version")
	case len(doc.Bundles) > maxPluginBundles:
		return nil, camperrors.Newf("the document offers %d bundles; at most %d are read", len(doc.Bundles), maxPluginBundles)
	}
	bundles := make([]PluginBundle, 0, len(doc.Bundles))
	for _, b := range doc.Bundles {
		bundle, err := validatePluginBundle(b, doc, campSlugs)
		if errors.Is(err, errMoreThanSkill) {
			return nil, err
		}
		if err != nil {
			return nil, camperrors.Wrapf(err, "bundle %q", b.Name)
		}
		if slices.ContainsFunc(bundles, func(seen PluginBundle) bool { return seen.Name == b.Name }) {
			return nil, camperrors.Newf("bundle %q is offered twice", b.Name)
		}
		bundles = append(bundles, bundle)
	}
	return bundles, nil
}

// validatePluginBundle accepts a bundle of exactly one file, SKILL.md. A
// bundle that also holds other files is errMoreThanSkill: this camp is behind
// the plugin, not the plugin broken.
func validatePluginBundle(b pluginDocBundle, doc pluginDocument, campSlugs []string) (PluginBundle, error) {
	switch {
	case !ValidPluginBundleName(b.Name):
		return PluginBundle{}, camperrors.New("the name is not a valid bundle name")
	case slices.Contains(campSlugs, b.Name):
		return PluginBundle{}, camperrors.New("camp ships a bundle with this name")
	}
	i := slices.IndexFunc(b.Files, func(f pluginDocFile) bool { return f.Path == pluginSkillFile })
	switch {
	case i < 0:
		return PluginBundle{}, camperrors.New("it has no SKILL.md")
	case len(b.Files) > 1:
		return PluginBundle{}, errMoreThanSkill
	}
	f := b.Files[i]
	if err := validatePluginFile(f.Path, f.SHA256, f.Content); err != nil {
		return PluginBundle{}, err
	}
	if err := checkSkillFrontmatter(f.Content, b.Name); err != nil {
		return PluginBundle{}, err
	}
	return PluginBundle{Name: b.Name, Plugin: doc.Plugin, Version: doc.Version, Files: []ShippedFile{{
		Bundle: b.Name, Path: pluginSkillFile, Content: []byte(f.Content),
		Plugin: doc.Plugin, PluginVersion: doc.Version,
	}}}, nil
}

func validatePluginFile(path, sum, content string) error {
	switch {
	case len(content) > maxPluginFileBytes:
		return camperrors.Newf("file %q holds more than %d bytes", path, maxPluginFileBytes)
	case !utf8.ValidString(content):
		return camperrors.Newf("file %q is not valid UTF-8", path)
	case len(sum) != 64 || !isLowerHex(sum):
		return camperrors.Newf("file %q has a sha256 that is not 64 lowercase hex digits", path)
	case HashContent([]byte(content)) != sum:
		return camperrors.Newf("file %q does not match its sha256", path)
	}
	return nil
}

// checkSkillFrontmatter reads the lines between the opening --- and the next
// --- as single-line key: value pairs, without a YAML parser.
func checkSkillFrontmatter(content, name string) error {
	lines := strings.Split(content, "\n")
	if strings.TrimRight(lines[0], "\r") != "---" {
		return camperrors.New("SKILL.md does not start with frontmatter")
	}
	fields := make(map[string]string)
	closed := false
	for _, line := range lines[1:] {
		line = strings.TrimRight(line, "\r")
		if line == "---" {
			closed = true
			break
		}
		key, value, ok := strings.Cut(line, ":")
		if ok {
			fields[strings.TrimSpace(key)] = unquote(strings.TrimSpace(value))
		}
	}
	switch {
	case !closed:
		return camperrors.New("SKILL.md frontmatter is not closed")
	case fields["name"] != name:
		return camperrors.Newf("SKILL.md frontmatter name %q is not the bundle name", fields["name"])
	case fields["description"] == "":
		return camperrors.New("SKILL.md frontmatter has no description")
	}
	return nil
}

func unquote(s string) string {
	if len(s) >= 2 && (s[0] == '"' || s[0] == '\'') && s[len(s)-1] == s[0] {
		return strings.TrimSpace(s[1 : len(s)-1])
	}
	return s
}
