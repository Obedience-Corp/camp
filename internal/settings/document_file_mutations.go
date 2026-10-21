package settings

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"path"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/Obedience-Corp/camp/internal/fsutil"
)

// lockedDocument anchors both identity and document operations to one directory.
type lockedDocument struct {
	root    *os.Root
	release func()
}

func (d lockedDocument) close() { d.release(); _ = d.root.Close() }

func (s Service) lockDocuments(ctx context.Context, paths ...string) (Service, lockedDocument, error) {
	root, err := s.openDocumentRoot(ctx)
	if err != nil {
		return s, lockedDocument{}, err
	}
	var releases []func()
	release := func() {
		for i := len(releases) - 1; i >= 0; i-- {
			releases[i]()
		}
	}
	fail := func(err error) (Service, lockedDocument, error) {
		release()
		_ = root.Close()
		return s, lockedDocument{}, err
	}
	paths = slices.Clone(paths)
	slices.Sort(paths)
	paths = slices.Compact(paths)
	for _, rel := range paths {
		// Resolve existing parent links before any lock file can be created.
		if _, err := lstatContained(root, rel); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fail(rootCallErr(err, s.Root, s.CampID, rel))
		}
		if err := rejectLockPath(rel); err != nil {
			return fail(rootCallErr(err, s.Root, s.CampID, rel))
		}
		if err := ordinaryLockFile(root, rel+".lock"); err != nil {
			return fail(rootCallErr(err, s.Root, s.CampID, rel))
		}
		unlock, err := fsutil.AcquireRootFileLock(ctx, root, rel+".lock")
		if err != nil {
			return fail(rootCallErr(err, s.Root, s.CampID, rel))
		}
		releases = append(releases, unlock)
	}
	s, err = s.gateRoot(ctx, root)
	if err != nil {
		return fail(err)
	}
	return s, lockedDocument{root: root, release: release}, nil
}

func (s Service) mutationRef(ref, expected string) (string, error) {
	rel, err := ResolveRef(ref)
	if err != nil {
		return "", opInvalid(err, s.Root, s.CampID, ref)
	}
	if validExpectedRevision(expected) != nil {
		return "", s.mutationError("invalid_revision", rel, "expected revision must be absent or a sha256 digest")
	}
	return rel, nil
}

func (s Service) mutationError(code, rel, message string) error {
	return &OpError{Code: code, Path: rel, Message: message, CampID: s.CampID, CampRoot: s.Root}
}

func (s Service) documentLimit() int {
	if s.Limits.MaxDocumentBytes <= 0 {
		return DefaultLimits().MaxDocumentBytes
	}
	return s.Limits.MaxDocumentBytes
}

func (s Service) validateMutation(rel string, next, previous []byte) error {
	if len(next) > s.documentLimit() {
		return s.mutationError(CodeOversized, rel, "document exceeds the size limit")
	}
	if !utf8.Valid(next) || bytes.IndexByte(next, 0) >= 0 {
		return s.mutationError("validation_error", rel, "document must contain UTF-8 text without null bytes")
	}
	checked, err := ValidateSource(rel, next, previous)
	if err != nil {
		return rootCallErr(err, s.Root, s.CampID, rel)
	}
	if !checked.Valid() {
		return &OpError{Code: "validation_error", Message: "document is invalid", Path: rel, CampID: s.CampID, CampRoot: s.Root, Diagnostics: checked.Diagnostics}
	}
	if rel == "campaign.yaml" {
		if _, _, err := InterpretManifestIdentity(next, s.RegistryID); err != nil {
			return rootCallErr(err, s.Root, s.CampID, rel)
		}
	}
	return nil
}

func (s Service) existingMutation(root *os.Root, rel, expected string) ([]byte, DocumentEntry, error) {
	info, err := lstatContained(root, rel)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) && expected != RevisionAbsent {
			return nil, DocumentEntry{}, s.revisionConflict(rel)
		}
		return nil, DocumentEntry{}, rootCallErr(err, s.Root, s.CampID, rel)
	}
	if !info.Mode().IsRegular() {
		return nil, DocumentEntry{}, s.mutationError(CodeInvalidPath, rel, "document must be an ordinary file")
	}
	data, err := readFileLimited(root, rel, s.documentLimit())
	if err != nil {
		return nil, DocumentEntry{}, rootCallErr(err, s.Root, s.CampID, rel)
	}
	if RevisionOf(data) != expected {
		return nil, DocumentEntry{}, s.revisionConflict(rel)
	}
	entry, err := Classify(rel, DocumentState{Kind: KindFile, CampID: s.CampID})
	if err != nil {
		return nil, entry, rootCallErr(err, s.Root, s.CampID, rel)
	}
	entry = ApplyContentFormat(entry, data)
	if !utf8.Valid(data) {
		entry = markBinaryText(entry)
	}
	return data, entry, nil
}

func (s Service) persistMutation(ctx context.Context, root *os.Root, rel string, source []byte) (Snapshot, error) {
	if err := opCancelled(ctx, s.Root, s.CampID); err != nil {
		return Snapshot{}, err
	}
	if err := fsutil.WriteRootFileAtomically(root, rel, source, 0o644); err != nil {
		var applied *fsutil.RootWriteError
		if errors.As(err, &applied) && applied.Applied {
			return Snapshot{}, s.appliedMutationError(rel, err)
		}
		return Snapshot{}, rootCallErr(err, s.Root, s.CampID, rel)
	}
	return s.readback(ctx, rel)
}

func (s Service) appliedMutationError(rel string, cause error) error {
	message := "The write succeeded, but a later filesystem operation failed; reload to inspect the persisted state"
	if cause != nil {
		message += ": " + cause.Error()
	}
	return &OpError{Code: CodeIO, Message: message, Path: rel, CampID: s.CampID, CampRoot: s.Root, Applied: true, Err: cause}
}

type createdParent struct {
	path string
	info fs.FileInfo
}

func createDocumentParents(root *os.Root, rel string) ([]createdParent, error) {
	var created []createdParent
	current := ""
	parent := path.Dir(rel)
	if parent == "." {
		return created, nil
	}
	for _, part := range strings.Split(parent, "/") {
		current = path.Join(current, part)
		info, err := root.Lstat(current)
		if errors.Is(err, fs.ErrNotExist) {
			if err := root.Mkdir(current, 0o755); err != nil {
				return created, err
			}
			info, err = root.Lstat(current)
			if err != nil {
				return created, err
			}
			created = append(created, createdParent{path: current, info: info})
		} else if err != nil {
			return created, err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return created, &os.PathError{Op: "mkdir", Path: current, Err: fs.ErrInvalid}
		}
	}
	return created, nil
}

func cleanupDocumentParents(root *os.Root, parents []createdParent) {
	for i := len(parents) - 1; i >= 0; i-- {
		info, err := root.Lstat(parents[i].path)
		if err == nil && os.SameFile(info, parents[i].info) {
			_ = root.Remove(parents[i].path)
		}
	}
}

// Create writes a new authored document without replacing an existing path.
func (s Service) Create(ctx context.Context, ref, expected string, source []byte) (Snapshot, error) {
	rel, err := s.mutationRef(ref, expected)
	if err != nil {
		return Snapshot{}, err
	}
	if expected != RevisionAbsent {
		return Snapshot{}, s.revisionConflict(rel)
	}
	if err := s.validateMutation(rel, source, nil); err != nil {
		return Snapshot{}, err
	}
	entry, err := Classify(rel, DocumentState{Kind: KindMissing, CampID: s.CampID})
	if err != nil {
		return Snapshot{}, rootCallErr(err, s.Root, s.CampID, rel)
	}
	entry = ApplyContentFormat(entry, source)
	if !entry.Capabilities.Create {
		return Snapshot{}, s.mutationError(CodeInvalidPath, rel, "document cannot be created through this API")
	}
	if len(strings.Split(rel, "/")) > DefaultLimits().MaxDepth {
		return Snapshot{}, s.mutationError(CodeInvalidPath, rel, "document path exceeds the depth limit")
	}
	root, err := s.openDocumentRoot(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	defer func() { _ = root.Close() }()
	s, err = s.gateRoot(ctx, root)
	if err != nil {
		return Snapshot{}, err
	}
	// Serialize parent scaffolding so cleanup cannot remove another create's
	// freshly prepared directory. Ordinary writers retain their document lock.
	const parentLock = ".document-create.lock"
	if err := ordinaryLockFile(root, parentLock); err != nil {
		return Snapshot{}, rootCallErr(err, s.Root, s.CampID, rel)
	}
	unlockParents, err := fsutil.AcquireRootFileLock(ctx, root, parentLock)
	if err != nil {
		return Snapshot{}, rootCallErr(err, s.Root, s.CampID, rel)
	}
	defer unlockParents()
	if _, err := lstatContained(root, rel); err == nil {
		return Snapshot{}, s.revisionConflict(rel)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return Snapshot{}, rootCallErr(err, s.Root, s.CampID, rel)
	}
	parents, err := createDocumentParents(root, rel)
	applied := false
	defer func() {
		if !applied {
			cleanupDocumentParents(root, parents)
		}
	}()
	if err != nil {
		return Snapshot{}, rootCallErr(err, s.Root, s.CampID, rel)
	}
	if err := ordinaryLockFile(root, rel+".lock"); err != nil {
		return Snapshot{}, rootCallErr(err, s.Root, s.CampID, rel)
	}
	unlock, err := fsutil.AcquireRootFileLock(ctx, root, rel+".lock")
	if err != nil {
		return Snapshot{}, rootCallErr(err, s.Root, s.CampID, rel)
	}
	defer unlock()
	s, err = s.gateRoot(ctx, root)
	if err != nil {
		return Snapshot{}, err
	}
	if _, err := lstatContained(root, rel); err == nil {
		return Snapshot{}, s.revisionConflict(rel)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return Snapshot{}, rootCallErr(err, s.Root, s.CampID, rel)
	}
	if err := opCancelled(ctx, s.Root, s.CampID); err != nil {
		return Snapshot{}, err
	}
	if err := fsutil.CreateRootFileAtomically(root, rel, source, 0o644); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return Snapshot{}, s.revisionConflict(rel)
		}
		var writeErr *fsutil.RootWriteError
		if errors.As(err, &writeErr) && writeErr.Applied {
			applied = true
			return Snapshot{}, s.appliedMutationError(rel, err)
		}
		return Snapshot{}, rootCallErr(err, s.Root, s.CampID, rel)
	}
	applied = true
	return s.readback(ctx, rel)
}

// Patch updates structured object fields while preserving unrelated content.
func (s Service) Patch(ctx context.Context, ref, expected string, patch []byte) (Snapshot, error) {
	rel, err := s.mutationRef(ref, expected)
	if err != nil {
		return Snapshot{}, err
	}
	if len(patch) > s.documentLimit() {
		return Snapshot{}, s.mutationError(CodeOversized, rel, "patch exceeds the size limit")
	}
	changes, err := ParsePatch(patch)
	if err != nil {
		return Snapshot{}, opInvalid(err, s.Root, s.CampID, rel)
	}
	s, locked, err := s.lockDocuments(ctx, rel)
	if err != nil {
		return Snapshot{}, err
	}
	defer locked.close()
	current, entry, err := s.existingMutation(locked.root, rel, expected)
	if err != nil {
		return Snapshot{}, err
	}
	if !entry.Capabilities.SourceEdit {
		return Snapshot{}, s.mutationError(CodeInvalidPath, rel, "document cannot be edited")
	}
	var next []byte
	switch entry.Format {
	case DocumentYAML:
		next, err = PatchYAML(current, changes)
	case DocumentJSON:
		next, err = PatchJSON(current, changes)
	default:
		return Snapshot{}, s.mutationError(CodeInvalidPath, rel, "field patches require YAML or JSON")
	}
	if err != nil {
		return Snapshot{}, opInvalid(err, s.Root, s.CampID, rel)
	}
	if err := s.validateMutation(rel, next, current); err != nil {
		return Snapshot{}, err
	}
	return s.persistMutation(ctx, locked.root, rel, next)
}

// Rename moves an authored file without overwriting an existing destination.
func (s Service) Rename(ctx context.Context, ref, destination, expected string) (Snapshot, error) {
	rel, err := s.mutationRef(ref, expected)
	if err != nil {
		return Snapshot{}, err
	}
	dest, err := ResolveRef(destination)
	if err != nil {
		return Snapshot{}, opInvalid(err, s.Root, s.CampID, destination)
	}
	if rel == dest {
		return Snapshot{}, s.mutationError(CodeInvalidPath, rel, "destination must differ from the source")
	}
	s, locked, err := s.lockDocuments(ctx, rel, dest)
	if err != nil {
		return Snapshot{}, err
	}
	defer locked.close()
	current, entry, err := s.existingMutation(locked.root, rel, expected)
	if err != nil {
		return Snapshot{}, err
	}
	if !entry.Capabilities.Rename {
		return Snapshot{}, s.mutationError(CodeInvalidPath, rel, "document cannot be renamed")
	}
	if _, err := lstatContained(locked.root, dest); err == nil {
		return Snapshot{}, s.mutationError("already_exists", dest, "destination already exists")
	} else if !errors.Is(err, fs.ErrNotExist) {
		return Snapshot{}, rootCallErr(err, s.Root, s.CampID, dest)
	}
	target, err := Classify(dest, DocumentState{Kind: KindMissing, CampID: s.CampID})
	if err != nil {
		return Snapshot{}, opInvalid(err, s.Root, s.CampID, dest)
	}
	target = ApplyContentFormat(target, current)
	if !target.Capabilities.Create {
		return Snapshot{}, s.mutationError(CodeInvalidPath, dest, "destination is managed by an owner")
	}
	if _, known := lookupKnown(dest); known {
		return Snapshot{}, s.mutationError(CodeInvalidPath, dest, "use document create for a known configuration")
	}
	if err := s.validateMutation(dest, current, nil); err != nil {
		return Snapshot{}, err
	}
	if err := opCancelled(ctx, s.Root, s.CampID); err != nil {
		return Snapshot{}, err
	}
	// Link publishes without clobbering, then remove the original. If removal
	// fails both names remain and Applied reports that partial result explicitly.
	if err := locked.root.Link(rel, dest); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return Snapshot{}, s.mutationError("already_exists", dest, "destination already exists")
		}
		return Snapshot{}, rootCallErr(err, s.Root, s.CampID, dest)
	}
	if err := fsutil.SyncRootDir(locked.root, path.Dir(dest)); err != nil {
		return Snapshot{}, s.appliedMutationError(dest, err)
	}
	if err := locked.root.Remove(rel); err != nil {
		return Snapshot{}, &OpError{Code: CodeIO, Message: "Destination " + dest + " was created, but source " + rel + " could not be removed; both names remain: " + err.Error(), Path: rel, CampID: s.CampID, CampRoot: s.Root, Applied: true, Err: err}
	}
	if err := fsutil.SyncRootDir(locked.root, path.Dir(rel)); err != nil {
		return Snapshot{}, s.appliedMutationError(dest, err)
	}
	return s.readback(ctx, dest)
}

// Delete removes an authored file at the revision the caller read.
func (s Service) Delete(ctx context.Context, ref, expected string) (Snapshot, error) {
	rel, err := s.mutationRef(ref, expected)
	if err != nil {
		return Snapshot{}, err
	}
	s, locked, err := s.lockDocuments(ctx, rel)
	if err != nil {
		return Snapshot{}, err
	}
	defer locked.close()
	_, entry, err := s.existingMutation(locked.root, rel, expected)
	if err != nil {
		return Snapshot{}, err
	}
	if !entry.Capabilities.Delete {
		return Snapshot{}, s.mutationError(CodeInvalidPath, rel, "document cannot be deleted")
	}
	if err := opCancelled(ctx, s.Root, s.CampID); err != nil {
		return Snapshot{}, err
	}
	if err := locked.root.Remove(rel); err != nil {
		return Snapshot{}, rootCallErr(err, s.Root, s.CampID, rel)
	}
	if err := fsutil.SyncRootDir(locked.root, path.Dir(rel)); err != nil {
		return Snapshot{}, s.appliedMutationError(rel, err)
	}
	snap, err := s.readback(ctx, rel)
	if err != nil {
		return Snapshot{}, err
	}
	if snap.Exists {
		return Snapshot{}, &OpError{Code: ReadbackFailedCode, Message: "The delete succeeded, but another file appeared before readback; reload to inspect it", Path: rel, CampID: s.CampID, CampRoot: s.Root, Applied: true}
	}
	return snap, nil
}
