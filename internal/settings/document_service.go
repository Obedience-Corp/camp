package settings

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// Service inventories, reads, validates, and mutates .campaign documents.
// Root is the canonical camp directory. RegistryID is the expected registered
// camp id; empty means identity is taken from a parseable manifest.
// When the manifest cannot be parsed, CampID is RegistryID and BrokenIdentity
// is true so source can be read and repaired. Later writes must use RegistryID.
type Service struct {
	Root           string
	RegistryID     string
	CampID         string
	BrokenIdentity bool
	Limits         Limits
}

// NewService canonicalizes root and requires .campaign to be a real directory.
// It does not load jumps or call config.LoadCampaignConfig.
func NewService(ctx context.Context, root, registryID string) (Service, error) {
	if err := ctx.Err(); err != nil {
		return Service{}, opCancelled(ctx, "", "")
	}
	if strings.TrimSpace(root) == "" || strings.ContainsRune(root, 0) {
		return Service{}, &OpError{Code: CodeInvalidPath, Message: "camp root is required"}
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return Service{}, opInvalid(err, root, "", "")
	}
	canon, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return Service{}, opIO(err, abs, "", "")
	}
	svc := Service{
		Root:       canon,
		RegistryID: registryID,
		Limits:     DefaultLimits(),
	}
	return svc.gate(ctx)
}

// Catalog lists every discovered path that fits the limits, plus known schema
// documents that are absent. It does not follow symlinks or open special files.
func (s Service) Catalog(ctx context.Context) (Catalog, error) {
	doc, err := s.openDocumentRoot(ctx)
	if err != nil {
		return Catalog{}, err
	}
	defer func() { _ = doc.Close() }()
	s, err = s.gateRoot(ctx, doc)
	if err != nil {
		return Catalog{}, err
	}
	byPath, err := s.seedKnown(doc)
	if err != nil {
		return Catalog{}, err
	}
	truncated := false
	if err := s.walk(ctx, doc, "", 0, byPath, &truncated); err != nil {
		return Catalog{}, err
	}
	docs := make([]DocumentEntry, 0, len(byPath))
	for _, rel := range slices.Sorted(maps.Keys(byPath)) {
		docs = append(docs, byPath[rel])
	}
	diags := EmptyDiagnostics()
	if truncated {
		diags = append(diags, Diagnostic{
			Severity: SeverityWarning,
			Message:  "Catalog stopped at the depth or entry limit.",
		})
	}
	return Catalog{
		SchemaVersion: DocumentSchemaVersion,
		CampID:        s.CampID,
		CampRoot:      s.Root,
		Documents:     docs,
		Limits:        s.Limits,
		Truncated:     truncated,
		Diagnostics:   diags,
	}, nil
}

// Get returns one document. Invalid UTF-8 or schema content is returned with
// diagnostics so it can be repaired. Missing files use revision absent and a
// null content pointer. Nothing is created.
func (s Service) Get(ctx context.Context, ref string) (Snapshot, error) {
	rel, err := ResolveRef(ref)
	if err != nil {
		return Snapshot{}, opInvalid(err, s.Root, s.CampID, ref)
	}
	doc, err := s.openDocumentRoot(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	defer func() { _ = doc.Close() }()
	s, err = s.gateRoot(ctx, doc)
	if err != nil {
		return Snapshot{}, err
	}
	info, err := lstatContained(doc, rel)
	if err != nil {
		if os.IsNotExist(err) {
			return s.missingSnapshot(rel), nil
		}
		return Snapshot{}, rootCallErr(err, s.Root, s.CampID, rel)
	}
	state := DocumentState{Kind: kindOf(info), CampID: s.CampID}
	if state.Kind == KindSymlink {
		target, rerr := doc.Readlink(rel)
		if rerr != nil {
			return Snapshot{}, rootCallErr(rerr, s.Root, s.CampID, rel)
		}
		state.LinkTarget = target
	}
	entry, err := Classify(rel, state)
	if err != nil {
		return Snapshot{}, opInvalid(err, s.Root, s.CampID, rel)
	}
	snap := s.snapshotFrom(entry)
	if state.Kind != KindFile {
		return snap, nil
	}
	if s.Limits.MaxDocumentBytes >= 0 && info.Size() > int64(s.Limits.MaxDocumentBytes) {
		return Snapshot{}, &OpError{
			Code: CodeOversized, Message: "document exceeds the size limit",
			Path: rel, CampID: s.CampID, CampRoot: s.Root,
		}
	}
	body, err := readFileLimited(doc, rel, s.Limits.MaxDocumentBytes)
	if err != nil {
		return Snapshot{}, rootCallErr(err, s.Root, s.CampID, rel)
	}
	if !utf8.Valid(body) {
		entry = markBinaryText(entry)
	} else {
		entry = ApplyContentFormat(entry, body)
	}
	snap = s.snapshotFrom(entry)
	snap.Revision = RevisionOf(body)
	snap.Capabilities.Actions = actionsWithRevision(entry.Capabilities.Actions, rel, snap.Revision, s.CampID)
	if entry.Format == DocumentBinary {
		snap.Content = nil
		snap.Diagnostics = []Diagnostic{{
			Severity: SeverityError,
			Message:  "The file is not valid UTF-8 text.",
		}}
		return snap, nil
	}
	text := string(body)
	snap.Content = &text
	checked, err := ValidateSource(rel, body, nil)
	if err != nil {
		return Snapshot{}, opInvalid(err, s.Root, s.CampID, rel)
	}
	snap.Diagnostics = checked.Diagnostics
	if checked.Values != nil {
		if _, merr := json.Marshal(checked.Values); merr != nil {
			snap.Diagnostics = append(snap.Diagnostics, Diagnostic{
				Severity: SeverityError,
				Message:  "values were omitted because they are not JSON",
			})
		} else {
			snap.Values = checked.Values
		}
	}
	if rel == "settings/local.json" && checked.Valid() {
		if err := s.attachLocalEffective(ctx, body, &snap); err != nil {
			return Snapshot{}, err
		}
	}
	return snap, nil
}

// Validate checks candidate bytes without writing. An invalid candidate returns
// Valid false. Transport, path, and cancellation failures are errors.
func (s Service) Validate(ctx context.Context, ref string, source []byte) (Validation, error) {
	rel, err := ResolveRef(ref)
	if err != nil {
		return Validation{}, opInvalid(err, s.Root, s.CampID, ref)
	}
	doc, err := s.openDocumentRoot(ctx)
	if err != nil {
		return Validation{}, err
	}
	defer func() { _ = doc.Close() }()
	s, err = s.gateRoot(ctx, doc)
	if err != nil {
		return Validation{}, err
	}
	if s.Limits.MaxDocumentBytes >= 0 && len(source) > s.Limits.MaxDocumentBytes {
		return Validation{
			SchemaVersion: DocumentSchemaVersion,
			CampID:        s.CampID,
			CampRoot:      s.Root,
			Path:          rel,
			Valid:         false,
			Diagnostics: []Diagnostic{{
				Severity: SeverityError,
				Message:  "document exceeds the size limit",
			}},
		}, nil
	}
	entry, missing, err := s.inspect(doc, rel)
	if err != nil {
		if os.IsNotExist(err) {
			missing = true
			entry, err = Classify(rel, DocumentState{Kind: KindMissing, CampID: s.CampID})
			if err != nil {
				return Validation{}, opInvalid(err, s.Root, s.CampID, rel)
			}
		} else {
			return Validation{}, rootCallErr(err, s.Root, s.CampID, rel)
		}
	}
	if !acceptsCandidate(entry, missing) {
		message := entry.Capabilities.Reason
		if message == "" {
			message = "this document cannot be edited through the document API"
		}
		return Validation{
			SchemaVersion: DocumentSchemaVersion,
			CampID:        s.CampID,
			CampRoot:      s.Root,
			Path:          rel,
			Valid:         false,
			Diagnostics:   []Diagnostic{{Severity: SeverityError, Message: message}},
		}, nil
	}
	previous, warning, err := s.previousBytes(ctx, doc, rel, missing)
	if err != nil {
		return Validation{}, err
	}
	checked, err := ValidateSource(rel, source, previous)
	if err != nil {
		return Validation{}, opInvalid(err, s.Root, s.CampID, rel)
	}
	if warning != "" {
		checked.Diagnostics = append(checked.Diagnostics, Diagnostic{Severity: SeverityWarning, Message: warning})
	}
	if rel == "campaign.yaml" && s.RegistryID != "" {
		if _, _, ierr := InterpretManifestIdentity(source, s.RegistryID); ierr != nil {
			checked.Diagnostics = append(checked.Diagnostics, Diagnostic{
				Severity: SeverityError,
				Path:     "id",
				Message:  IdentityMismatchMessage,
			})
		}
	}
	if checked.Diagnostics == nil {
		checked.Diagnostics = EmptyDiagnostics()
	}
	return Validation{
		SchemaVersion: DocumentSchemaVersion,
		CampID:        s.CampID,
		CampRoot:      s.CampRoot(),
		Path:          rel,
		Valid:         checked.Valid(),
		Diagnostics:   checked.Diagnostics,
	}, nil
}

// openDocumentRoot opens the camp root, rejects a symlink .campaign, and
// returns a caller-owned root anchored at the real .campaign directory.
// The camp handle is closed before return. The caller must Close the result.
func (s Service) openDocumentRoot(ctx context.Context) (*os.Root, error) {
	if err := opCancelled(ctx, s.Root, s.CampID); err != nil {
		return nil, err
	}
	if strings.TrimSpace(s.Root) == "" {
		return nil, &OpError{Code: CodeInvalidPath, Message: "camp root is required", CampID: s.CampID}
	}
	camp, err := os.OpenRoot(s.Root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, &OpError{Code: CodeNotFound, Message: ".campaign directory is missing", CampRoot: s.Root, CampID: s.CampID, Err: err}
		}
		if isNotDirectory(err) {
			return nil, &OpError{Code: CodeInvalidPath, Message: "camp root must be a directory", CampRoot: s.Root, CampID: s.CampID, Err: err}
		}
		return nil, opIO(err, s.Root, s.CampID, "")
	}
	defer func() { _ = camp.Close() }()
	info, err := camp.Lstat(".campaign")
	if err != nil {
		if os.IsNotExist(err) {
			return nil, &OpError{Code: CodeNotFound, Message: ".campaign directory is missing", CampRoot: s.Root, CampID: s.CampID, Err: err}
		}
		return nil, rootCallErr(err, s.Root, s.CampID, ".campaign")
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return nil, &OpError{Code: CodeInvalidPath, Message: ".campaign must be a real directory", CampRoot: s.Root, CampID: s.CampID}
	}
	doc, err := camp.OpenRoot(".campaign")
	if err != nil {
		return nil, rootCallErr(err, s.Root, s.CampID, ".campaign")
	}
	opened, err := doc.Lstat(".")
	if err != nil || !os.SameFile(info, opened) {
		_ = doc.Close()
		if err != nil {
			return nil, rootCallErr(err, s.Root, s.CampID, ".campaign")
		}
		return nil, &OpError{Code: CodeInvalidPath, Message: ".campaign must be a real directory", CampRoot: s.Root, CampID: s.CampID}
	}
	return doc, nil
}

// CampRoot returns the canonical camp directory.
func (s Service) CampRoot() string { return s.Root }

func (s Service) gate(ctx context.Context) (Service, error) {
	doc, err := s.openDocumentRoot(ctx)
	if err != nil {
		return Service{}, err
	}
	defer func() { _ = doc.Close() }()
	return s.gateRoot(ctx, doc)
}

func (s Service) gateRoot(ctx context.Context, root *os.Root) (Service, error) {
	if err := opCancelled(ctx, s.Root, s.CampID); err != nil {
		return Service{}, err
	}
	source, unreadable, err := readManifestBytes(root, s.Limits.MaxDocumentBytes)
	if err != nil {
		return Service{}, rootCallErr(err, s.Root, s.RegistryID, "campaign.yaml")
	}
	if unreadable {
		id, broken, _ := brokenIdentity(s.RegistryID)
		s.CampID = id
		s.BrokenIdentity = broken
		return s, nil
	}
	id, broken, ierr := InterpretManifestIdentity(source, s.RegistryID)
	if ierr != nil {
		var op *OpError
		if errors.As(ierr, &op) {
			op.CampRoot = s.Root
			op.CampID = id
			op.Path = "campaign.yaml"
			return Service{}, op
		}
		return Service{}, ierr
	}
	s.CampID = id
	s.BrokenIdentity = broken
	return s, nil
}

func (s Service) walk(ctx context.Context, root *os.Root, rel string, depth int, byPath map[string]DocumentEntry, truncated *bool) error {
	if err := opCancelled(ctx, s.Root, s.CampID); err != nil {
		return err
	}
	dir, err := openDirectory(root, rel)
	if err != nil {
		return rootCallErr(err, s.Root, s.CampID, rel)
	}
	defer func() { _ = dir.Close() }()
	for {
		if err := opCancelled(ctx, s.Root, s.CampID); err != nil {
			return err
		}
		batch, err := dir.ReadDir(dirChunk)
		if len(batch) == 0 {
			if err == nil || errors.Is(err, io.EOF) {
				return nil
			}
			return opIO(err, s.Root, s.CampID, rel)
		}
		for _, ent := range batch {
			if err := opCancelled(ctx, s.Root, s.CampID); err != nil {
				return err
			}
			name := ent.Name()
			if name == "." || name == ".." {
				continue
			}
			childRel := name
			if rel != "" {
				childRel = rel + "/" + name
			}
			info, lerr := root.Lstat(childRel)
			if lerr != nil {
				return rootCallErr(lerr, s.Root, s.CampID, childRel)
			}
			added, err := s.addCatalogEntry(root, childRel, info, byPath, truncated)
			if err != nil {
				return err
			}
			if !added {
				return nil
			}
			if kindOf(info) != KindDirectory {
				continue
			}
			next := depth + 1
			if next >= s.Limits.MaxDepth {
				has, herr := directoryHasEntry(root, childRel)
				if herr != nil {
					return rootCallErr(herr, s.Root, s.CampID, childRel)
				}
				if has {
					*truncated = true
				}
				continue
			}
			if err := s.walk(ctx, root, childRel, next, byPath, truncated); err != nil {
				return err
			}
			if *truncated && len(byPath) >= s.Limits.MaxEntries {
				return nil
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return opIO(err, s.Root, s.CampID, rel)
		}
	}
}

func (s Service) addCatalogEntry(root *os.Root, rel string, info fs.FileInfo, byPath map[string]DocumentEntry, truncated *bool) (bool, error) {
	if _, known := byPath[rel]; !known && len(byPath) >= s.Limits.MaxEntries {
		*truncated = true
		return false, nil
	}
	entry, err := s.classifyLive(root, rel, info)
	if err != nil {
		return false, err
	}
	byPath[rel] = entry
	return true, nil
}

func (s Service) seedKnown(root *os.Root) (map[string]DocumentEntry, error) {
	paths := KnownPaths()
	byPath := make(map[string]DocumentEntry, len(paths))
	for _, rel := range paths {
		info, err := lstatContained(root, rel)
		if err != nil {
			if os.IsNotExist(err) {
				entry, cerr := Classify(rel, DocumentState{Kind: KindMissing, Revision: RevisionAbsent, CampID: s.CampID})
				if cerr != nil {
					return nil, opInvalid(cerr, s.Root, s.CampID, rel)
				}
				byPath[rel] = entry
				continue
			}
			return nil, rootCallErr(err, s.Root, s.CampID, rel)
		}
		entry, err := s.classifyLive(root, rel, info)
		if err != nil {
			return nil, err
		}
		byPath[rel] = entry
	}
	return byPath, nil
}

func (s Service) classifyLive(root *os.Root, rel string, info fs.FileInfo) (DocumentEntry, error) {
	state := DocumentState{Kind: kindOf(info), CampID: s.CampID}
	var prefix []byte
	switch state.Kind {
	case KindSymlink:
		target, err := root.Readlink(rel)
		if err != nil {
			return DocumentEntry{}, rootCallErr(err, s.Root, s.CampID, rel)
		}
		state.LinkTarget = target
	case KindFile:
		var err error
		prefix, err = readPrefix(root, rel, 512)
		if err != nil {
			return DocumentEntry{}, rootCallErr(err, s.Root, s.CampID, rel)
		}
	}
	entry, err := Classify(rel, state)
	if err != nil {
		return DocumentEntry{}, opInvalid(err, s.Root, s.CampID, rel)
	}
	if state.Kind == KindFile {
		entry = ApplyContentFormat(entry, prefix)
		if info.Size() > int64(s.documentLimit()) {
			entry = markOversized(entry)
		}
	}
	return entry, nil
}

func actionsWithRevision(actions []OwnerAction, rel, revision, campID string) []OwnerAction {
	if len(actions) == 0 {
		return actions
	}
	out := slices.Clone(actions)
	for i := range out {
		if !out[i].Supported {
			continue
		}
		out[i].Command = DocumentActionCommand(rel, out[i].ID, revision, campID)
	}
	return out
}

func (s Service) inspect(root *os.Root, rel string) (DocumentEntry, bool, error) {
	info, err := lstatContained(root, rel)
	if err != nil {
		return DocumentEntry{}, false, err
	}
	state := DocumentState{Kind: kindOf(info), CampID: s.CampID}
	if state.Kind == KindSymlink {
		target, rerr := root.Readlink(rel)
		if rerr != nil {
			return DocumentEntry{}, false, rerr
		}
		state.LinkTarget = target
	}
	entry, err := Classify(rel, state)
	if err != nil {
		return DocumentEntry{}, false, err
	}
	if state.Kind == KindFile {
		if s.Limits.MaxDocumentBytes >= 0 && info.Size() > int64(s.Limits.MaxDocumentBytes) {
			return markOversized(entry), false, nil
		}
		prefix, rerr := readPrefix(root, rel, 512)
		if rerr != nil {
			return DocumentEntry{}, false, rerr
		}
		entry = ApplyContentFormat(entry, prefix)
	}
	return entry, false, nil
}

func markOversized(entry DocumentEntry) DocumentEntry {
	entry.Capabilities = Capabilities{Actions: EmptyActions(), Reason: "Document exceeds the configured byte limit; use its owning CLI or a local editor."}
	entry.Fields = nil
	return entry
}

func acceptsCandidate(entry DocumentEntry, missing bool) bool {
	if missing {
		return entry.Capabilities.Create
	}
	if !entry.Capabilities.Read || entry.Format == DocumentSymlink || entry.Format == DocumentDirectory {
		return false
	}
	return entry.Capabilities.SourceEdit
}

func (s Service) missingSnapshot(rel string) Snapshot {
	entry, err := Classify(rel, DocumentState{Kind: KindMissing, CampID: s.CampID})
	if err != nil {
		entry = DocumentEntry{ID: rel, Path: rel, Scope: ScopeCamp, Capabilities: Capabilities{Read: true, Actions: EmptyActions()}}
	}
	snap := s.snapshotFrom(entry)
	snap.Exists = false
	snap.Revision = RevisionAbsent
	snap.Content = nil
	return snap
}

func (s Service) snapshotFrom(entry DocumentEntry) Snapshot {
	if entry.Capabilities.Actions == nil {
		entry.Capabilities.Actions = EmptyActions()
	}
	return Snapshot{
		SchemaVersion: DocumentSchemaVersion,
		CampID:        s.CampID,
		CampRoot:      s.Root,
		ID:            entry.ID,
		Path:          entry.Path,
		Title:         entry.Title,
		Scope:         entry.Scope,
		Format:        entry.Format,
		Owner:         entry.Owner,
		Exists:        entry.Exists,
		Capabilities:  entry.Capabilities,
		Fields:        entry.Fields,
		LinkTarget:    entry.LinkTarget,
		Revision:      RevisionAbsent,
		Diagnostics:   EmptyDiagnostics(),
		Applied:       false,
	}
}

func (s Service) previousBytes(ctx context.Context, root *os.Root, rel string, missing bool) ([]byte, string, error) {
	if missing {
		return nil, "", nil
	}
	if err := opCancelled(ctx, s.Root, s.CampID); err != nil {
		return nil, "", err
	}
	info, err := lstatContained(root, rel)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, "", nil
		}
		return nil, "", rootCallErr(err, s.Root, s.CampID, rel)
	}
	if kindOf(info) != KindFile {
		return nil, "", nil
	}
	if s.Limits.MaxDocumentBytes >= 0 && info.Size() > int64(s.Limits.MaxDocumentBytes) {
		return nil, "existing document exceeds the size limit; identity was not compared", nil
	}
	body, err := readFileLimited(root, rel, s.Limits.MaxDocumentBytes)
	if err != nil {
		var op *OpError
		if errors.As(err, &op) && op.Code == CodeOversized {
			return nil, "existing document exceeds the size limit; identity was not compared", nil
		}
		return nil, "", rootCallErr(err, s.Root, s.CampID, rel)
	}
	return body, "", nil
}

func (s Service) attachLocalEffective(ctx context.Context, body []byte, snap *Snapshot) error {
	if err := opCancelled(ctx, s.Root, s.CampID); err != nil {
		return err
	}
	global, err := config.ReadGlobalConfig(ctx)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return opCancelled(ctx, s.Root, s.CampID)
		}
		snap.Diagnostics = append(snap.Diagnostics, Diagnostic{
			Severity: SeverityWarning,
			Message:  "global settings were not read; effective values were omitted",
		})
		return nil
	}
	var local config.LocalSettings
	if err := json.Unmarshal(body, &local); err != nil {
		snap.Diagnostics = append(snap.Diagnostics, Diagnostic{
			Severity: SeverityWarning,
			Message:  "local settings were not decoded; effective values were omitted",
		})
		return nil
	}
	effective, inheritance := localOverlay(global, &local)
	snap.EffectiveValues = effective
	snap.Inheritance = inheritance
	return nil
}

// localOverlay resolves theme and commit prefs without reading the filesystem.
// A non-nil local commit block replaces the global block even when both flags
// are false.
func localOverlay(global *config.GlobalConfig, local *config.LocalSettings) (any, any) {
	var globalCommit config.CommitPrefs
	globalTheme := ""
	if global != nil {
		globalCommit = global.Commit
		globalTheme = global.TUI.Theme
	}
	theme := config.EffectiveThemeFrom(global, local)
	merged := config.MergeCommitPrefs(globalCommit, local)
	var localCommit any
	replaced := false
	if local != nil && local.Commit != nil {
		localCommit = commitValue(*local.Commit)
		replaced = true
	}
	localTheme := ""
	if local != nil {
		localTheme = local.ThemeOverride
	}
	effective := map[string]any{
		"theme_override": theme,
		"commit":         commitValue(merged),
	}
	inheritance := map[string]any{
		"theme_override": map[string]any{
			"global":    globalTheme,
			"local":     localTheme,
			"effective": theme,
		},
		"commit": map[string]any{
			"global":    commitValue(globalCommit),
			"local":     localCommit,
			"effective": commitValue(merged),
			"replaced":  replaced,
		},
	}
	return effective, inheritance
}

func commitValue(prefs config.CommitPrefs) map[string]any {
	return map[string]any{
		"sync_project_refs":   prefs.SyncProjectRefs,
		"disable_commit_tags": prefs.DisableCommitTags,
	}
}

func rootCallErr(err error, campRoot, campID, path string) error {
	if err == nil {
		return nil
	}
	var op *OpError
	if errors.As(err, &op) {
		if op.CampRoot == "" {
			op.CampRoot = campRoot
		}
		if op.CampID == "" {
			op.CampID = campID
		}
		if op.Path == "" {
			op.Path = path
		}
		return op
	}
	if isEscape(err) || isPathErr(err) {
		return opInvalid(err, campRoot, campID, path)
	}
	if errors.Is(err, fs.ErrNotExist) {
		return &OpError{Code: CodeNotFound, Message: "document or parent directory does not exist", Path: path, CampID: campID, CampRoot: campRoot, Err: err}
	}
	return opIO(err, campRoot, campID, path)
}

func isPathErr(err error) bool {
	var validation *camperrors.ValidationError
	return errors.As(err, &validation)
}
