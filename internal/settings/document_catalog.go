package settings

import (
	"bytes"
	"path"
	"strings"
	"unicode/utf8"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// DocumentKind is how a path is present on disk. KindMissing describes a known
// document that the catalog still lists.
type DocumentKind int

const (
	KindMissing DocumentKind = iota
	KindFile
	KindDirectory
	KindSymlink
	KindSpecial
)

// DocumentState is the filesystem fact the classifier needs. Revision and
// CampID are copied into supported action commands; both may be empty.
type DocumentState struct {
	Kind       DocumentKind
	LinkTarget string
	Revision   string
	CampID     string
}

type knownDocument struct {
	id      string
	aliases []string
	path    string
	title   string
	format  string
	owner   string
	form    bool
	managed bool
}

// KnownPaths is the stable document list. Schemas are attached for authored
// configs even when the file is absent.
func KnownPaths() []string {
	docs := knownDocuments()
	out := make([]string, len(docs))
	for i, doc := range docs {
		out[i] = doc.path
	}
	return out
}

func knownDocuments() []knownDocument {
	return []knownDocument{
		{id: "campaign_manifest", aliases: []string{"campaign-metadata"}, path: "campaign.yaml", title: "Camp manifest", format: DocumentYAML, owner: "camp", form: true},
		{id: "settings-jumps", aliases: []string{"settings_jumps"}, path: "settings/jumps.yaml", title: "Navigation", format: DocumentYAML, owner: "camp", form: true},
		{id: "fresh", path: "settings/fresh.yaml", title: "Fresh", format: DocumentYAML, owner: "camp", form: true},
		{id: "local_settings", path: "settings/local.json", title: "Local settings", format: DocumentJSON, owner: "camp", form: true},
		{id: "allowlist", aliases: []string{"settings-allowlist"}, path: "settings/allowlist.json", title: "Daemon command allowlist", format: DocumentJSON, owner: "camp", form: true},
		{id: "festival-pins", aliases: []string{"pins"}, path: "settings/pins.json", title: "Pins", format: DocumentJSON, owner: "camp", managed: true},
		{id: "workitems", path: "settings/workitems.json", title: "Workitem settings", format: DocumentJSON, owner: "camp", managed: true},
		{id: "workitem-links", path: "workitems/links.yaml", title: "Workitem links", format: DocumentYAML, owner: "camp", managed: true},
		{id: "artifacts", path: "artifacts.yaml", title: "Artifacts", format: DocumentYAML, owner: "camp", form: true},
		{id: "notices", path: "notices.yaml", title: "Notices", format: DocumentYAML, owner: "camp", form: true},
		{id: "leverage", path: "leverage/config.json", title: "Leverage", format: DocumentJSON, owner: "leverage", form: true},
	}
}

// ResolveRef accepts a catalog id, an alias, or a relative .campaign/ path.
func ResolveRef(ref string) (string, error) {
	for _, doc := range knownDocuments() {
		if ref == doc.id || ref == doc.path {
			return doc.path, nil
		}
		for _, alias := range doc.aliases {
			if ref == alias {
				return doc.path, nil
			}
		}
	}
	return CleanRelative(ref)
}

// CleanRelative rejects absolute paths, backslashes, NUL, and traversal.
func CleanRelative(p string) (string, error) {
	if p == "" {
		return "", camperrors.NewValidation("path", "document path is required", nil)
	}
	if strings.ContainsRune(p, 0) {
		return "", camperrors.NewValidation("path", "document path contains a null byte", nil)
	}
	if strings.Contains(p, `\`) {
		return "", camperrors.NewValidation("path", "document path must use slash separators", nil)
	}
	if strings.HasPrefix(p, "/") || path.IsAbs(p) {
		return "", camperrors.NewValidation("path", "document path must be relative to .campaign", nil)
	}
	for seg := range strings.SplitSeq(p, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return "", camperrors.NewValidation("path", "document path must not contain empty, current, or parent segments", nil)
		}
	}
	return path.Clean(p), nil
}

// DocumentActionCommand is the supported owner-action invocation.
func DocumentActionCommand(rel, action, revision, campID string) []string {
	if revision == "" {
		revision = RevisionAbsent
	}
	cmd := []string{
		"camp", "settings", "document", "action", rel, action,
		"--stdin", "--expected-revision", revision,
	}
	if campID != "" {
		cmd = append(cmd, "--campaign", campID)
	}
	return append(cmd, "--json")
}

// Classify describes one relative path. Known authored configs keep their
// schema when Kind is KindMissing.
func Classify(rel string, state DocumentState) (DocumentEntry, error) {
	clean, err := CleanRelative(rel)
	if err != nil {
		return DocumentEntry{}, err
	}
	doc, known := lookupKnown(clean)
	entry := DocumentEntry{
		ID:     clean,
		Path:   clean,
		Title:  titleFromPath(clean),
		Scope:  ScopeCamp,
		Format: formatFor(clean, state.Kind),
		Owner:  "camp",
		Exists: state.Kind != KindMissing,
		Capabilities: Capabilities{
			Read:    true,
			Actions: EmptyActions(),
		},
	}
	if known {
		entry.ID = doc.id
		entry.Title = doc.title
		entry.Format = doc.format
		entry.Owner = doc.owner
		if doc.form {
			entry.Fields = SchemaFor(doc.path)
			entry.Capabilities.Form = true
		}
	}
	if state.Kind == KindSymlink {
		entry.Format = DocumentSymlink
		entry.LinkTarget = state.LinkTarget
		entry.Fields = nil
		entry.Capabilities.Form = false
	}
	if state.Kind == KindDirectory {
		entry.Format = DocumentDirectory
		entry.Fields = nil
		entry.Capabilities.Form = false
	}
	applyPolicy(&entry, clean, doc, known, state)
	if entry.Capabilities.Actions == nil {
		entry.Capabilities.Actions = EmptyActions()
	}
	return entry, nil
}

func lookupKnown(rel string) (knownDocument, bool) {
	for _, doc := range knownDocuments() {
		if doc.path == rel {
			return doc, true
		}
	}
	return knownDocument{}, false
}

func applyPolicy(entry *DocumentEntry, rel string, doc knownDocument, known bool, state DocumentState) {
	switch state.Kind {
	case KindSymlink:
		entry.Capabilities.Reason = "Symlinks stay in the catalog as links. Ordinary edits do not follow them."
		return
	case KindDirectory:
		entry.Capabilities.Reason = "Directories are visible in the catalog and are not written as file content."
		return
	case KindSpecial:
		entry.Format = DocumentBinary
		entry.Capabilities.Read = false
		entry.Capabilities.Reason = "Special files are visible and are not opened."
		return
	}

	switch {
	case known && doc.path == "settings/pins.json":
		denyManaged(entry, "Pin changes go through pin and unpin.")
		entry.Capabilities.Actions = []OwnerAction{
			supportedAction(rel, "pin", "Pin a directory", state),
			supportedAction(rel, "unpin", "Unpin a directory", state),
		}
	case known && doc.path == "settings/workitems.json":
		denyManaged(entry, "Priority and group changes go through the workitem owner.")
		entry.Capabilities.Actions = []OwnerAction{
			supportedAction(rel, "workitem-priority", "Set workitem priority", state),
			supportedAction(rel, "workitem-group", "Set workitem group", state),
		}
	case known && doc.path == "workitems/links.yaml":
		denyManaged(entry, "Workitem links go through camp workitem link and camp workitem unlink.")
		entry.Capabilities.Actions = []OwnerAction{
			unsupported("link", "Link a workitem", []string{"camp", "workitem", "link"}, "Workitem links use camp workitem link."),
			unsupported("unlink", "Unlink a workitem", []string{"camp", "workitem", "unlink"}, "Workitem links use camp workitem unlink."),
		}
	case isGenerated(rel):
		denyManaged(entry, generatedReason(rel))
		entry.Owner = generatedOwner(rel)
		entry.Capabilities.Actions = generatedActions(rel)
	case isIntentDocument(rel):
		entry.Capabilities.SourceEdit = state.Kind == KindFile
		entry.Capabilities.Reason = "Idea identity and lifecycle stay with camp idea. Source edits keep id, created_at, and status."
		entry.Capabilities.Actions = []OwnerAction{
			unsupported("idea-edit", "Edit an idea", []string{"camp", "idea", "edit"}, "Idea edits use camp idea edit."),
			unsupported("idea-move", "Move an idea", []string{"camp", "idea", "move"}, "Idea lifecycle uses camp idea move."),
		}
	case isQuestDocument(rel):
		entry.Capabilities.SourceEdit = state.Kind == KindFile
		entry.Capabilities.Reason = "Quest identity and lifecycle stay with camp quest. Source edits keep id, created_at, and status."
		entry.Capabilities.Actions = []OwnerAction{
			unsupported("quest", "Manage a quest", []string{"camp", "quest"}, "Quest lifecycle uses camp quest."),
		}
	case known && doc.form:
		entry.Capabilities.SourceEdit = state.Kind == KindFile
		entry.Capabilities.Create = state.Kind == KindMissing
		if doc.path == "campaign.yaml" {
			entry.Capabilities.Actions = []OwnerAction{
				unsupported("project-add", "Add a project", []string{"camp", "project", "add"}, "Project lifecycle uses camp project add."),
				unsupported("project-remove", "Remove a project", []string{"camp", "project", "remove"}, "Project lifecycle uses camp project remove."),
			}
		}
	case entry.Format == DocumentBinary:
		entry.Capabilities.Reason = "Binary files are visible and are not edited as text."
	default:
		editable := state.Kind == KindFile || state.Kind == KindMissing
		entry.Capabilities.SourceEdit = state.Kind == KindFile
		entry.Capabilities.Create = state.Kind == KindMissing && editable
		entry.Capabilities.Rename = state.Kind == KindFile
		entry.Capabilities.Delete = state.Kind == KindFile
	}
}

func supportedAction(rel, id, label string, state DocumentState) OwnerAction {
	revision := state.Revision
	if revision == "" {
		revision = "<revision>"
	}
	return OwnerAction{
		ID:        id,
		Label:     label,
		Command:   DocumentActionCommand(rel, id, revision, state.CampID),
		Supported: true,
	}
}

func unsupported(id, label string, command []string, reason string) OwnerAction {
	return OwnerAction{ID: id, Label: label, Command: command, Supported: false, Reason: reason}
}

func denyManaged(entry *DocumentEntry, reason string) {
	entry.Capabilities.SourceEdit = false
	entry.Capabilities.Create = false
	entry.Capabilities.Rename = false
	entry.Capabilities.Delete = false
	entry.Capabilities.Form = false
	entry.Capabilities.Reason = reason
	entry.Fields = nil
}

func isGenerated(rel string) bool {
	if isQuestWorkspaceState(rel) {
		return true
	}
	switch rel {
	case "watchers.yaml", "state.yaml", "graph.db", "workitems/current.yaml", "workitems/links.yaml",
		"intents/.intents.jsonl", "intents/crawl.jsonl", "workitems/.workitems.jsonl":
		return true
	}
	switch {
	case strings.HasPrefix(rel, "fest/"):
		return true
	case strings.HasPrefix(rel, "cache/"):
		return true
	case strings.HasPrefix(rel, "events/"):
		return true
	case strings.HasPrefix(rel, "artifacts/manifests/"):
		return true
	case strings.HasPrefix(rel, "leverage/snapshots/"):
		return true
	case strings.HasPrefix(rel, "graphs/"):
		return true
	case strings.HasSuffix(rel, ".lock"):
		return true
	default:
		return false
	}
}

func generatedReason(rel string) string {
	switch {
	case isQuestWorkspaceState(rel):
		return "Quest workspace state is owned by camp quest workspace."
	case rel == "watchers.yaml":
		return "watchers.yaml is maintained by the watcher contract."
	case rel == "state.yaml":
		return "state.yaml is generated camp state."
	case rel == "graph.db" || strings.HasPrefix(rel, "graphs/"):
		return "Graph and index data is generated."
	case strings.HasPrefix(rel, "fest/"):
		return "fest owns this path."
	case rel == "workitems/current.yaml" || rel == "workitems/links.yaml":
		return "Workitem records are not raw-editable."
	case isOwnerAuditLog(rel):
		return "This file is an append-only owner audit or history log."
	default:
		return "This path is generated state and is not an authored document."
	}
}

func generatedOwner(rel string) string {
	if strings.HasPrefix(rel, "fest/") {
		return "fest"
	}
	return "camp"
}

func isOwnerAuditLog(rel string) bool {
	switch rel {
	case "intents/.intents.jsonl", "intents/crawl.jsonl", "workitems/.workitems.jsonl":
		return true
	default:
		return false
	}
}

func generatedActions(rel string) []OwnerAction {
	if isQuestWorkspaceState(rel) {
		return []OwnerAction{
			unsupported("workspace", "Quest workspace", []string{"camp", "quest", "workspace"}, "Quest workspace state uses camp quest workspace."),
		}
	}
	switch rel {
	case "intents/.intents.jsonl", "intents/crawl.jsonl":
		return []OwnerAction{
			unsupported("idea", "Idea history", []string{"camp", "idea"}, "Idea audit and crawl history use camp idea."),
		}
	case "workitems/.workitems.jsonl":
		return []OwnerAction{
			unsupported("workitem", "Workitem history", []string{"camp", "workitem"}, "Workitem audit history uses camp workitem."),
		}
	}
	if strings.HasPrefix(rel, "workitems/") {
		return []OwnerAction{
			unsupported("link", "Link a workitem", []string{"camp", "workitem", "link"}, "Workitem links use camp workitem link."),
			unsupported("unlink", "Unlink a workitem", []string{"camp", "workitem", "unlink"}, "Workitem links use camp workitem unlink."),
		}
	}
	return EmptyActions()
}

func formatFor(rel string, kind DocumentKind) string {
	switch kind {
	case KindDirectory:
		return DocumentDirectory
	case KindSymlink:
		return DocumentSymlink
	case KindSpecial:
		return DocumentBinary
	}
	return formatFromName(rel)
}

func formatFromName(rel string) string {
	base := path.Base(rel)
	if base == ".gitignore" {
		return DocumentText
	}
	switch strings.ToLower(path.Ext(base)) {
	case ".yaml", ".yml":
		return DocumentYAML
	case ".json":
		return DocumentJSON
	case ".md":
		return DocumentMarkdown
	case ".txt":
		return DocumentText
	case ".jsonl", ".lock", "":
		return DocumentText
	case ".db":
		if base == "graph.db" {
			return DocumentBinary
		}
		return DocumentText
	default:
		return DocumentText
	}
}

// ApplyContentFormat uses a bounded content prefix to tell authored text from
// binary. Callers pass bytes that were not read through a symlink or special file.
func ApplyContentFormat(entry DocumentEntry, content []byte) DocumentEntry {
	if entry.Format == DocumentDirectory || entry.Format == DocumentSymlink || !entry.Exists {
		return entry
	}
	if !looksBinary(content) {
		return entry
	}
	return markBinaryText(entry)
}

// markBinaryText clears authored edits. Catalog prefix sniffing uses looksBinary,
// which still allows a cut trailing rune. A complete body must call this when
// utf8.Valid is false even if that prefix rule would accept it.
func markBinaryText(entry DocumentEntry) DocumentEntry {
	entry.Format = DocumentBinary
	entry.Capabilities.SourceEdit = false
	entry.Capabilities.Create = false
	entry.Capabilities.Rename = false
	entry.Capabilities.Delete = false
	entry.Capabilities.Form = false
	entry.Fields = nil
	if entry.Capabilities.Reason == "" {
		entry.Capabilities.Reason = "The file is not valid UTF-8 text."
	}
	return entry
}

// looksBinary reports NUL or bytes that are not valid UTF-8.
// A trailing incomplete rune is text so a bounded prefix can split a character.
// Any finished invalid sequence, including 0xff, is binary.
func looksBinary(content []byte) bool {
	if bytes.IndexByte(content, 0) >= 0 {
		return true
	}
	for i := 0; i < len(content); {
		if !utf8.FullRune(content[i:]) {
			return false
		}
		r, size := utf8.DecodeRune(content[i:])
		if r == utf8.RuneError && size == 1 {
			return true
		}
		if size <= 0 {
			return true
		}
		i += size
	}
	return false
}

func titleFromPath(rel string) string {
	base := path.Base(rel)
	if base == "." || base == "/" {
		return rel
	}
	return base
}
