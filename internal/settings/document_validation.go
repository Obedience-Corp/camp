package settings

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"gopkg.in/yaml.v3"

	"github.com/Obedience-Corp/camp/internal/artifacts"
	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/intent"
	"github.com/Obedience-Corp/camp/internal/leverage"
	"github.com/Obedience-Corp/camp/internal/notice"
	"github.com/Obedience-Corp/camp/internal/quest"
)

// SourceValidation is the pure result of checking candidate bytes.
// Previous is the current file body; nil or empty means the document is new
// and immutable identity is not compared.
type SourceValidation struct {
	Values      any
	Diagnostics []Diagnostic
}

// Valid is false when any diagnostic is an error.
func (v SourceValidation) Valid() bool {
	return !HasErrors(v.Diagnostics)
}

// ValidateSource parses candidate source for rel and runs the owner validators.
// It does not read or write the filesystem. previous, when non-empty, is the
// current document used for immutable manifest and idea/quest identity checks.
func ValidateSource(rel string, source, previous []byte) (SourceValidation, error) {
	clean, err := ResolveRef(rel)
	if err != nil {
		return SourceValidation{}, err
	}
	if len(source) > DefaultLimits().MaxDocumentBytes || len(previous) > DefaultLimits().MaxDocumentBytes {
		return SourceValidation{Diagnostics: []Diagnostic{{
			Severity: SeverityError,
			Message:  "document exceeds the size limit",
		}}}, nil
	}
	if bytes.IndexByte(source, 0) >= 0 {
		return SourceValidation{Diagnostics: []Diagnostic{{
			Severity: SeverityError,
			Message:  "document must not contain a null byte",
		}}}, nil
	}
	if !utf8.Valid(source) {
		return SourceValidation{Diagnostics: []Diagnostic{{
			Severity: SeverityError,
			Message:  "document must be valid UTF-8",
		}}}, nil
	}

	var result SourceValidation
	switch clean {
	case "campaign.yaml":
		result = validateManifest(source, previous)
	case "settings/jumps.yaml":
		result = validateJumps(source)
	case "settings/fresh.yaml":
		result = validateFresh(source)
	case "settings/local.json":
		result = validateLocal(source)
	case "settings/allowlist.json":
		result = validateAllowlist(source)
	case "artifacts.yaml":
		result = validateArtifacts(source)
	case "notices.yaml":
		result = validateNotices(source)
	case "leverage/config.json":
		result = validateLeverage(source)
	default:
		result = validateGeneric(clean, source, previous)
	}
	if result.Diagnostics == nil {
		result.Diagnostics = EmptyDiagnostics()
	}
	return result, nil
}

// ManifestIdentityDiagnostics reports changes to id or created_at.
// A zero created_at on both sides is unchanged.
func ManifestIdentityDiagnostics(previous, proposed config.CampaignConfig) []Diagnostic {
	var out []Diagnostic
	if previous.ID != proposed.ID {
		out = append(out, Diagnostic{Severity: SeverityError, Path: "id", Message: "id is immutable"})
	}
	if !previous.CreatedAt.Equal(proposed.CreatedAt) {
		out = append(out, Diagnostic{Severity: SeverityError, Path: "created_at", Message: "created_at is immutable"})
	}
	return out
}

// ParseManifest reads campaign.yaml without applying defaults or loading jumps.
func ParseManifest(source []byte) (config.CampaignConfig, []Diagnostic, error) {
	var cfg config.CampaignConfig
	if err := yaml.Unmarshal(source, &cfg); err != nil {
		return config.CampaignConfig{}, yamlDiags(err), camperrors.Wrap(err, "parse manifest")
	}
	return cfg, nil, nil
}

func validateManifest(source, previous []byte) SourceValidation {
	if _, err := inspectYAML(source); err != nil {
		return SourceValidation{Diagnostics: []Diagnostic{{Severity: SeverityError, Message: err.Error()}}}
	}
	cfg, diags, err := ParseManifest(source)
	if err != nil {
		return SourceValidation{Diagnostics: diags}
	}
	if verr := config.ValidateCampaignConfig(&cfg); verr != nil {
		diags = append(diags, Diagnostic{Severity: SeverityError, Message: verr.Error()})
	}
	if len(previous) > 0 {
		old, _, perr := ParseManifest(previous)
		if perr != nil {
			diags = append(diags, previousManifestDiagnostics(previous, cfg)...)
		} else {
			diags = append(diags, ManifestIdentityDiagnostics(old, cfg)...)
			diags = append(diags, projectLifecycleDiagnostics(old.Projects, cfg.Projects)...)
		}
	}
	values := map[string]any{}
	if uerr := yaml.Unmarshal(source, &values); uerr != nil {
		diags = append(diags, yamlDiags(uerr)...)
		return SourceValidation{Diagnostics: diags}
	}
	safe, serr := JSONSafe(values)
	if serr != nil {
		diags = append(diags, Diagnostic{Severity: SeverityError, Message: serr.Error()})
		return SourceValidation{Diagnostics: diags}
	}
	return SourceValidation{Values: safe, Diagnostics: diags}
}

func validateJumps(source []byte) SourceValidation {
	if _, err := inspectYAML(source); err != nil {
		return SourceValidation{Diagnostics: []Diagnostic{{Severity: SeverityError, Message: err.Error()}}}
	}
	var cfg config.JumpsConfig
	if err := yaml.Unmarshal(source, &cfg); err != nil {
		return SourceValidation{Diagnostics: yamlDiags(err)}
	}
	var diags []Diagnostic
	checkCampPath(&diags, "paths.projects", cfg.Paths.Projects)
	checkCampPath(&diags, "paths.worktrees", cfg.Paths.Worktrees)
	checkCampPath(&diags, "paths.ai_docs", cfg.Paths.AIDocs)
	checkCampPath(&diags, "paths.docs", cfg.Paths.Docs)
	checkCampPath(&diags, "paths.festivals", cfg.Paths.Festivals)
	checkCampPath(&diags, "paths.workflow", cfg.Paths.Workflow)
	checkCampPath(&diags, "paths.intents", cfg.Paths.Intents)
	checkCampPath(&diags, "paths.reviews", cfg.Paths.Reviews)
	checkCampPath(&diags, "paths.code_reviews", cfg.Paths.CodeReviews)
	checkCampPath(&diags, "paths.pipelines", cfg.Paths.Pipelines)
	checkCampPath(&diags, "paths.design", cfg.Paths.Design)
	checkCampPath(&diags, "paths.dungeon", cfg.Paths.Dungeon)
	for name, shortcut := range cfg.Shortcuts {
		checkCampPath(&diags, "shortcuts."+name+".path", shortcut.Path)
		checkCampPath(&diags, "shortcuts."+name+".workdir", shortcut.WorkDir)
	}
	values := map[string]any{}
	if err := yaml.Unmarshal(source, &values); err != nil {
		return SourceValidation{Diagnostics: yamlDiags(err)}
	}
	return finishValues(values, diags)
}

func validateFresh(source []byte) SourceValidation {
	if _, err := inspectYAML(source); err != nil {
		return SourceValidation{Diagnostics: []Diagnostic{{Severity: SeverityError, Message: err.Error()}}}
	}
	var cfg config.FreshConfig
	if err := yaml.Unmarshal(source, &cfg); err != nil {
		return SourceValidation{Diagnostics: yamlDiags(err)}
	}
	var diags []Diagnostic
	switch cfg.CompletedRuns {
	case "", "prompt", "sweep", "report", "off":
	default:
		diags = append(diags, Diagnostic{Severity: SeverityError, Path: "completed_runs", Message: "unknown completed_runs value"})
	}
	switch cfg.MergedWorkitems {
	case "", "prompt", "report", "off":
	default:
		diags = append(diags, Diagnostic{Severity: SeverityError, Path: "merged_workitems", Message: "unknown merged_workitems value"})
	}
	for i, step := range cfg.FollowUp {
		if err := step.Validate(); err != nil {
			diags = append(diags, Diagnostic{Severity: SeverityError, Path: "follow_up." + strconv.Itoa(i), Message: err.Error()})
		}
	}
	names := make([]string, 0, len(cfg.Projects))
	for name := range cfg.Projects {
		names = append(names, name)
	}
	for _, name := range sortedStrings(names) {
		for i, step := range cfg.Projects[name].FollowUp {
			if err := step.Validate(); err != nil {
				diags = append(diags, Diagnostic{
					Severity: SeverityError,
					Path:     "projects." + name + ".follow_up." + strconv.Itoa(i),
					Message:  err.Error(),
				})
			}
		}
	}
	values := map[string]any{}
	if err := yaml.Unmarshal(source, &values); err != nil {
		return SourceValidation{Diagnostics: yamlDiags(err)}
	}
	return finishValues(values, diags)
}

func validateLocal(source []byte) SourceValidation {
	var cfg config.LocalSettings
	if err := json.Unmarshal(source, &cfg); err != nil {
		return SourceValidation{Diagnostics: jsonDiags(err, source)}
	}
	var diags []Diagnostic
	if err := config.ValidateLocalSettings(&cfg); err != nil {
		diags = append(diags, Diagnostic{Severity: SeverityError, Message: err.Error()})
	}
	values, err := decodeJSONValue(source)
	if err != nil {
		return SourceValidation{Diagnostics: jsonDiags(err, source)}
	}
	return finishValues(values, diags)
}

func validateAllowlist(source []byte) SourceValidation {
	var cfg config.Allowlist
	if err := json.Unmarshal(source, &cfg); err != nil {
		return SourceValidation{Diagnostics: jsonDiags(err, source)}
	}
	var diags []Diagnostic
	if cfg.Version != 0 && cfg.Version != config.AllowlistVersion {
		diags = append(diags, Diagnostic{
			Severity: SeverityError,
			Path:     "version",
			Message:  "unknown allowlist schema version " + strconv.Itoa(cfg.Version),
		})
	}
	values, err := decodeJSONValue(source)
	if err != nil {
		return SourceValidation{Diagnostics: jsonDiags(err, source)}
	}
	return finishValues(values, diags)
}

func validateArtifacts(source []byte) SourceValidation {
	if _, err := inspectYAML(source); err != nil {
		return SourceValidation{Diagnostics: []Diagnostic{{Severity: SeverityError, Message: err.Error()}}}
	}
	var file artifacts.File
	if err := yaml.Unmarshal(source, &file); err != nil {
		return SourceValidation{Diagnostics: yamlDiags(err)}
	}
	var diags []Diagnostic
	if err := file.Validate(); err != nil {
		diags = append(diags, Diagnostic{Severity: SeverityError, Message: err.Error()})
	}
	values := map[string]any{}
	if err := yaml.Unmarshal(source, &values); err != nil {
		return SourceValidation{Diagnostics: yamlDiags(err)}
	}
	return finishValues(values, diags)
}

func validateNotices(source []byte) SourceValidation {
	if _, err := inspectYAML(source); err != nil {
		return SourceValidation{Diagnostics: []Diagnostic{{Severity: SeverityError, Message: err.Error()}}}
	}
	var file notice.DismissalFile
	if err := yaml.Unmarshal(source, &file); err != nil {
		return SourceValidation{Diagnostics: yamlDiags(err)}
	}
	var diags []Diagnostic
	if file.Version != 0 && file.Version != 1 {
		diags = append(diags, Diagnostic{
			Severity: SeverityError,
			Path:     "version",
			Message:  "unsupported notices.yaml version " + strconv.Itoa(file.Version),
		})
	}
	values := map[string]any{}
	if err := yaml.Unmarshal(source, &values); err != nil {
		return SourceValidation{Diagnostics: yamlDiags(err)}
	}
	return finishValues(values, diags)
}

func validateLeverage(source []byte) SourceValidation {
	var cfg leverage.LeverageConfig
	if err := json.Unmarshal(source, &cfg); err != nil {
		return SourceValidation{Diagnostics: jsonDiags(err, source)}
	}
	var diags []Diagnostic
	switch cfg.COCOMOProjectType {
	case "", leverage.COCOMOOrganic, "semi-detached", "embedded":
	default:
		diags = append(diags, Diagnostic{
			Severity: SeverityError,
			Path:     "cocomo_project_type",
			Message:  "invalid COCOMO type " + strconv.Quote(cfg.COCOMOProjectType) + ": must be organic, semi-detached, or embedded",
		})
	}
	if cfg.ActualPeople < 0 {
		diags = append(diags, Diagnostic{
			Severity: SeverityError,
			Path:     "actual_people",
			Message:  "people must be >= 0",
		})
	}
	names := make([]string, 0, len(cfg.Projects))
	for name := range cfg.Projects {
		names = append(names, name)
	}
	for _, name := range sortedStrings(names) {
		entry := cfg.Projects[name]
		if entry.Include && entry.Path == "" {
			diags = append(diags, Diagnostic{
				Severity: SeverityError,
				Path:     "projects." + name + ".path",
				Message:  "project " + strconv.Quote(name) + ": path is required",
			})
		}
	}
	values, err := decodeJSONValue(source)
	if err != nil {
		return SourceValidation{Diagnostics: jsonDiags(err, source)}
	}
	return finishValues(values, diags)
}

func projectLifecycleDiagnostics(previous, proposed []config.ProjectConfig) []Diagnostic {
	if len(previous) != len(proposed) {
		return []Diagnostic{{
			Severity: SeverityError,
			Path:     "projects",
			Message:  "project registry is owned by camp project",
		}}
	}
	var diags []Diagnostic
	for i := range previous {
		old := previous[i]
		next := proposed[i]
		prefix := "projects." + strconv.Itoa(i)
		if old.Name != next.Name {
			diags = append(diags, Diagnostic{Severity: SeverityError, Path: prefix + ".name", Message: "project name is immutable"})
		}
		if old.Path != next.Path {
			diags = append(diags, Diagnostic{Severity: SeverityError, Path: prefix + ".path", Message: "project path is immutable"})
		}
		if old.URL != next.URL {
			diags = append(diags, Diagnostic{Severity: SeverityError, Path: prefix + ".url", Message: "project url is immutable"})
		}
		if old.Branch != next.Branch {
			diags = append(diags, Diagnostic{Severity: SeverityError, Path: prefix + ".branch", Message: "project branch is immutable"})
		}
	}
	return diags
}

func previousManifestDiagnostics(previous []byte, proposed config.CampaignConfig) []Diagnostic {
	mapping, ok := yamlMapping(previous)
	if !ok {
		return []Diagnostic{{Severity: SeverityWarning, Message: "existing manifest could not be parsed; identity was not compared"}}
	}
	var diags []Diagnostic
	if id, idOK := decodeStringKey(mapping, "id"); idOK {
		if id != proposed.ID {
			diags = append(diags, Diagnostic{Severity: SeverityError, Path: "id", Message: "id is immutable"})
		}
	} else {
		diags = append(diags, Diagnostic{Severity: SeverityWarning, Message: "existing manifest could not be parsed; identity was not compared"})
	}
	if created, createdOK := decodeTimeKey(mapping, "created_at"); createdOK {
		if !created.Equal(proposed.CreatedAt) {
			diags = append(diags, Diagnostic{Severity: SeverityError, Path: "created_at", Message: "created_at is immutable"})
		}
	} else {
		diags = append(diags, Diagnostic{Severity: SeverityWarning, Message: "existing manifest could not be parsed; identity was not compared"})
	}
	if projects, projectsOK := decodeProjects(mapping); projectsOK {
		diags = append(diags, projectLifecycleDiagnostics(projects, proposed.Projects)...)
	} else {
		diags = append(diags, Diagnostic{Severity: SeverityWarning, Message: "existing project registry could not be parsed; lifecycle was not compared"})
	}
	return diags
}

func validateGeneric(rel string, source, previous []byte) SourceValidation {
	clean := strings.TrimPrefix(rel, "/")
	if isIntentDocument(clean) {
		return validateIntentSource(source, previous)
	}
	if isQuestDocument(clean) {
		return validateQuestSource(source, previous)
	}
	format := formatFromName(clean)
	var diags []Diagnostic
	var values any
	switch format {
	case DocumentYAML:
		if _, err := inspectYAML(source); err != nil {
			return SourceValidation{Diagnostics: []Diagnostic{{Severity: SeverityError, Message: err.Error()}}}
		}
		if err := yaml.Unmarshal(source, &values); err != nil {
			return SourceValidation{Diagnostics: yamlDiags(err)}
		}
	case DocumentJSON:
		decoded, err := decodeJSONValue(source)
		if err != nil {
			return SourceValidation{Diagnostics: jsonDiags(err, source)}
		}
		values = decoded
	case DocumentMarkdown, DocumentText:
		values = string(source)
	default:
		return SourceValidation{Diagnostics: []Diagnostic{{
			Severity: SeverityError,
			Message:  "this format is not accepted as document text",
		}}}
	}
	return finishValues(values, diags)
}

func finishValues(values any, diags []Diagnostic) SourceValidation {
	safe, err := JSONSafe(values)
	if err != nil {
		diags = append(diags, Diagnostic{Severity: SeverityError, Message: err.Error()})
		return SourceValidation{Diagnostics: diags}
	}
	return SourceValidation{Values: safe, Diagnostics: diags}
}

func validateIntentSource(source, previous []byte) SourceValidation {
	parsed, err := intent.ParseIntent(source)
	if err != nil {
		return SourceValidation{Diagnostics: []Diagnostic{{Severity: SeverityError, Message: err.Error()}}}
	}
	var diags []Diagnostic
	for _, verr := range parsed.Validate() {
		diags = append(diags, Diagnostic{Severity: SeverityError, Message: verr.Error()})
	}
	if len(bytes.TrimSpace(previous)) > 0 {
		old, oerr := intent.ParseIntent(previous)
		if oerr != nil {
			diags = append(diags, previousIntentDiagnostics(previous, parsed)...)
		} else {
			diags = append(diags, compareStable(old.ID, parsed.ID, old.CreatedAt, parsed.CreatedAt, string(old.Status), string(parsed.Status))...)
		}
	}
	return SourceValidation{Values: string(source), Diagnostics: diags}
}

func validateQuestSource(source, previous []byte) SourceValidation {
	if _, err := inspectYAML(source); err != nil {
		return SourceValidation{Diagnostics: []Diagnostic{{Severity: SeverityError, Message: err.Error()}}}
	}
	var parsed quest.Quest
	if err := yaml.Unmarshal(source, &parsed); err != nil {
		return SourceValidation{Diagnostics: yamlDiags(err)}
	}
	var diags []Diagnostic
	if err := parsed.Validate(); err != nil {
		diags = append(diags, Diagnostic{Severity: SeverityError, Message: err.Error()})
	}
	if len(bytes.TrimSpace(previous)) > 0 {
		var old quest.Quest
		if err := yaml.Unmarshal(previous, &old); err != nil {
			diags = append(diags, previousQuestDiagnostics(previous, parsed)...)
		} else {
			diags = append(diags, compareStable(old.ID, parsed.ID, old.CreatedAt, parsed.CreatedAt, string(old.Status), string(parsed.Status))...)
		}
	}
	values := map[string]any{}
	if err := yaml.Unmarshal(source, &values); err != nil {
		return SourceValidation{Diagnostics: yamlDiags(err)}
	}
	return finishValues(values, diags)
}

func previousIntentDiagnostics(previous []byte, parsed *intent.Intent) []Diagnostic {
	const warning = "existing idea metadata could not be parsed; identity was not compared"
	front, ok := intentFrontmatter(previous)
	if !ok {
		return []Diagnostic{{Severity: SeverityWarning, Message: warning}}
	}
	mapping, ok := yamlMapping(front)
	if !ok {
		return []Diagnostic{{Severity: SeverityWarning, Message: warning}}
	}
	return compareParsedIdentity(mapping, parsed.ID, parsed.CreatedAt, string(parsed.Status), warning)
}

func previousQuestDiagnostics(previous []byte, parsed quest.Quest) []Diagnostic {
	const warning = "existing quest metadata could not be parsed; identity was not compared"
	mapping, ok := yamlMapping(previous)
	if !ok {
		return []Diagnostic{{Severity: SeverityWarning, Message: warning}}
	}
	return compareParsedIdentity(mapping, parsed.ID, parsed.CreatedAt, string(parsed.Status), warning)
}

func compareParsedIdentity(mapping *yaml.Node, nextID string, nextCreated time.Time, nextStatus, warning string) []Diagnostic {
	id, idOK := decodeStringKey(mapping, "id")
	created, createdOK := decodeTimeKey(mapping, "created_at")
	status, statusOK := decodeStringKey(mapping, "status")
	if !idOK && !createdOK && !statusOK {
		return []Diagnostic{{Severity: SeverityWarning, Message: warning}}
	}
	var diags []Diagnostic
	if !idOK || !createdOK || !statusOK {
		diags = append(diags, Diagnostic{Severity: SeverityWarning, Message: warning})
	}
	if idOK && id != nextID {
		diags = append(diags, Diagnostic{Severity: SeverityError, Path: "id", Message: "id is immutable"})
	}
	if createdOK && !created.Equal(nextCreated) {
		diags = append(diags, Diagnostic{Severity: SeverityError, Path: "created_at", Message: "created_at is immutable"})
	}
	if statusOK && status != nextStatus {
		diags = append(diags, Diagnostic{Severity: SeverityError, Path: "status", Message: "status is owned by the lifecycle command"})
	}
	return diags
}

func intentFrontmatter(source []byte) ([]byte, bool) {
	rest, ok := bytes.CutPrefix(source, []byte("---\n"))
	if !ok {
		return nil, false
	}
	front, _, ok := bytes.Cut(rest, []byte("\n---"))
	if !ok {
		return nil, false
	}
	return bytes.TrimSpace(front), true
}

func yamlMapping(source []byte) (*yaml.Node, bool) {
	var doc yaml.Node
	if err := yaml.Unmarshal(source, &doc); err != nil {
		return nil, false
	}
	node := &doc
	if doc.Kind == yaml.DocumentNode {
		if len(doc.Content) != 1 {
			return nil, false
		}
		node = doc.Content[0]
	}
	if node.Kind != yaml.MappingNode {
		return nil, false
	}
	return node, true
}

func mappingValue(mapping *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			return mapping.Content[i+1]
		}
	}
	return nil
}

func decodeStringKey(mapping *yaml.Node, key string) (string, bool) {
	node := mappingValue(mapping, key)
	if node == nil {
		return "", true
	}
	var value string
	if err := node.Decode(&value); err != nil {
		return "", false
	}
	return value, true
}

func decodeTimeKey(mapping *yaml.Node, key string) (time.Time, bool) {
	node := mappingValue(mapping, key)
	if node == nil {
		return time.Time{}, true
	}
	var value time.Time
	if err := node.Decode(&value); err != nil {
		return time.Time{}, false
	}
	return value, true
}

func decodeProjects(mapping *yaml.Node) ([]config.ProjectConfig, bool) {
	node := mappingValue(mapping, "projects")
	if node == nil || node.Tag == "!!null" {
		return nil, true
	}
	if node.Kind != yaml.SequenceNode {
		return nil, false
	}
	projects := make([]config.ProjectConfig, len(node.Content))
	for i, item := range node.Content {
		ident, ok := decodeProjectIdentity(item)
		if !ok {
			return nil, false
		}
		projects[i] = ident
	}
	return projects, true
}

func decodeProjectIdentity(item *yaml.Node) (config.ProjectConfig, bool) {
	if item == nil || item.Kind != yaml.MappingNode {
		return config.ProjectConfig{}, false
	}
	name, nameOK := decodeStringKey(item, "name")
	path, pathOK := decodeStringKey(item, "path")
	url, urlOK := decodeStringKey(item, "url")
	branch, branchOK := decodeStringKey(item, "branch")
	if !nameOK || !pathOK || !urlOK || !branchOK {
		return config.ProjectConfig{}, false
	}
	return config.ProjectConfig{Name: name, Path: path, URL: url, Branch: branch}, true
}

func compareStable(oldID, nextID string, oldCreated, nextCreated time.Time, oldStatus, nextStatus string) []Diagnostic {
	var diags []Diagnostic
	if oldID != nextID {
		diags = append(diags, Diagnostic{Severity: SeverityError, Path: "id", Message: "id is immutable"})
	}
	if !oldCreated.Equal(nextCreated) {
		diags = append(diags, Diagnostic{Severity: SeverityError, Path: "created_at", Message: "created_at is immutable"})
	}
	if oldStatus != nextStatus {
		diags = append(diags, Diagnostic{Severity: SeverityError, Path: "status", Message: "status is owned by the lifecycle command"})
	}
	return diags
}

func badCampPath(value string) bool {
	if strings.ContainsRune(value, 0) || strings.Contains(value, `\`) || strings.HasPrefix(value, "/") {
		return true
	}
	for seg := range strings.SplitSeq(value, "/") {
		if seg == ".." {
			return true
		}
	}
	return false
}

func checkCampPath(diags *[]Diagnostic, field, value string) {
	if value == "" {
		return
	}
	if badCampPath(value) {
		*diags = append(*diags, Diagnostic{
			Severity: SeverityError,
			Path:     field,
			Message:  "must be a relative camp path",
		})
	}
}

func yamlDiags(err error) []Diagnostic {
	if err == nil {
		return nil
	}
	var typeErr *yaml.TypeError
	if camperrors.As(err, &typeErr) {
		out := make([]Diagnostic, 0, len(typeErr.Errors))
		for _, msg := range typeErr.Errors {
			out = append(out, yamlMessageDiag(msg))
		}
		if len(out) > 0 {
			return out
		}
	}
	return []Diagnostic{yamlMessageDiag(err.Error())}
}

func yamlMessageDiag(msg string) Diagnostic {
	diag := Diagnostic{Severity: SeverityError, Message: msg}
	if line, ok := lineNumber(msg); ok {
		diag.Line = line
	}
	return diag
}

func lineNumber(msg string) (int, bool) {
	const marker = "line "
	i := strings.Index(msg, marker)
	if i < 0 {
		return 0, false
	}
	rest := msg[i+len(marker):]
	n := 0
	digits := false
	for _, r := range rest {
		if r < '0' || r > '9' {
			break
		}
		digits = true
		n = n*10 + int(r-'0')
	}
	return n, digits
}

func jsonDiags(err error, source []byte) []Diagnostic {
	if err == nil {
		return nil
	}
	diag := Diagnostic{Severity: SeverityError, Message: err.Error()}
	var syntax *json.SyntaxError
	if camperrors.As(err, &syntax) {
		diag.Line, diag.Column = lineColumn(source, int(syntax.Offset))
	}
	var typeErr *json.UnmarshalTypeError
	if camperrors.As(err, &typeErr) {
		diag.Path = typeErr.Field
		diag.Line, diag.Column = lineColumn(source, int(typeErr.Offset))
	}
	return []Diagnostic{diag}
}

func lineColumn(source []byte, offset int) (line, column int) {
	if offset < 0 {
		return 0, 0
	}
	if offset > len(source) {
		offset = len(source)
	}
	line = 1
	column = 1
	for i := 0; i < offset; i++ {
		if source[i] == '\n' {
			line++
			column = 1
			continue
		}
		column++
	}
	return line, column
}

func decodeJSONValue(source []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(source))
	dec.UseNumber()
	value, err := decodeJSONToken(dec)
	if err != nil {
		return nil, err
	}
	if dec.More() {
		return nil, errors.New("trailing JSON value")
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, errors.New("trailing JSON value")
		}
		return nil, err
	}
	return value, nil
}

func decodeJSONToken(dec *json.Decoder) (any, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	delim, ok := tok.(json.Delim)
	if !ok {
		return tok, nil
	}
	switch delim {
	case '{':
		obj := map[string]any{}
		for dec.More() {
			keyTok, err := dec.Token()
			if err != nil {
				return nil, err
			}
			key, ok := keyTok.(string)
			if !ok {
				return nil, errors.New("object key must be a string")
			}
			if _, exists := obj[key]; exists {
				return nil, errors.New("duplicate JSON key " + key)
			}
			val, err := decodeJSONToken(dec)
			if err != nil {
				return nil, err
			}
			obj[key] = val
		}
		if _, err := dec.Token(); err != nil {
			return nil, err
		}
		return obj, nil
	case '[':
		arr := []any{}
		for dec.More() {
			val, err := decodeJSONToken(dec)
			if err != nil {
				return nil, err
			}
			arr = append(arr, val)
		}
		if _, err := dec.Token(); err != nil {
			return nil, err
		}
		return arr, nil
	default:
		return nil, errors.New("unexpected JSON delimiter")
	}
}

// JSONSafe walks a decoded YAML or JSON value into types encoding/json accepts.
// Map keys that are not strings are an error so Get can report them instead of
// failing later during response encoding.
func JSONSafe(value any) (any, error) {
	switch typed := value.(type) {
	case nil, string, bool, json.Number, float32, float64,
		int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64:
		return typed, nil
	case time.Time:
		return typed.UTC().Format(time.RFC3339Nano), nil
	case []any:
		out := make([]any, len(typed))
		for i, item := range typed {
			safe, err := JSONSafe(item)
			if err != nil {
				return nil, err
			}
			out[i] = safe
		}
		return out, nil
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, item := range typed {
			safe, err := JSONSafe(item)
			if err != nil {
				return nil, err
			}
			out[key] = safe
		}
		return out, nil
	case map[any]any:
		out := make(map[string]any, len(typed))
		for key, item := range typed {
			text, ok := key.(string)
			if !ok {
				return nil, camperrors.NewValidation("values", "YAML map key is not a string", nil)
			}
			safe, err := JSONSafe(item)
			if err != nil {
				return nil, err
			}
			out[text] = safe
		}
		return out, nil
	default:
		return nil, camperrors.NewValidation("values", "value cannot be represented as JSON", nil)
	}
}

// InterpretManifestIdentity separates camp identity from typed field validation.
// A parseable id that is missing or different from registryID is rejected.
// Syntax that prevents reading id returns registryID with broken true so the
// source can still be read and repaired; later writes must use that id.
func InterpretManifestIdentity(source []byte, registryID string) (string, bool, error) {
	if len(bytes.TrimSpace(source)) == 0 {
		if registryID != "" {
			return "", false, identityMismatch()
		}
		return "", false, nil
	}
	if _, err := inspectYAML(source); err != nil {
		return brokenIdentity(registryID)
	}
	var probe struct {
		ID string `yaml:"id"`
	}
	if err := yaml.Unmarshal(source, &probe); err != nil {
		return brokenIdentity(registryID)
	}
	if registryID != "" && probe.ID != registryID {
		return probe.ID, false, identityMismatch()
	}
	return probe.ID, false, nil
}

func brokenIdentity(registryID string) (string, bool, error) {
	if registryID != "" {
		return registryID, true, nil
	}
	return "", true, nil
}

func identityMismatch() error {
	return &OpError{Code: CodeIdentityMismatch, Message: IdentityMismatchMessage}
}

func sortedStrings(in []string) []string {
	out := slices.Clone(in)
	slices.Sort(out)
	return out
}
