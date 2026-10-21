package settings

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/Obedience-Corp/camp/internal/campaign"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/fsutil"
	"github.com/Obedience-Corp/camp/internal/pins"
	"github.com/Obedience-Corp/camp/internal/workitem/priority"
	"github.com/Obedience-Corp/camp/internal/workitem/selector"
)

const (
	pinsDocument      = "settings/pins.json"
	workitemsDocument = "settings/workitems.json"
	settingsDirName   = "settings"

	codeUnsupportedAction = "unsupported_action"
	codeInvalidBody       = "invalid_body"
	codeInvalidRevision   = "invalid_revision"
	codeAlreadyExists     = "already_exists"
	codeMalformedDocument = "malformed_document"
	codeAmbiguousSelector = "ambiguous_selector"
)

var errMalformedDocument = errors.New("malformed document")

type actionKind int

const (
	kindPin actionKind = iota
	kindUnpin
	kindPriority
	kindGroup
)

type preparedAction struct {
	kind          actionKind
	name          string
	pin           pins.Pin
	key           string
	level         priority.ManualPriority
	clearPriority bool
	group         string
	clearGroup    bool
}

// Action applies one owner action and returns the persisted snapshot.
// Invalid actions and bodies are rejected before a lock is taken. The settings
// directory must already be an ordinary directory; this does not create it.
// Applied is true on the persisted snapshot. A failed readback after a
// successful write is readback_failed with Applied true.
func (s Service) Action(ctx context.Context, ref, action, expected string, body []byte) (Snapshot, error) {
	if err := opCancelled(ctx, s.Root, s.CampID); err != nil {
		return Snapshot{}, err
	}
	rel, err := ResolveRef(ref)
	if err != nil {
		return Snapshot{}, opInvalid(err, s.Root, s.CampID, ref)
	}
	if err := validExpectedRevision(expected); err != nil {
		return Snapshot{}, s.fail(codeInvalidRevision, rel, err.Error())
	}
	kind, err := kindFor(rel, action)
	if err != nil {
		return Snapshot{}, s.fail(codeUnsupportedAction, rel, err.Error(), err)
	}
	if s.Limits.MaxDocumentBytes >= 0 && len(body) > s.Limits.MaxDocumentBytes {
		return Snapshot{}, s.fail(CodeOversized, rel, "document exceeds the size limit")
	}
	prepared, err := s.prepareAction(ctx, rel, kind, body)
	if err != nil {
		return Snapshot{}, err
	}

	root, err := s.openDocumentRoot(ctx)
	if err != nil {
		return Snapshot{}, s.wrapRootErr(err, rel)
	}
	defer func() { _ = root.Close() }()
	s, err = s.gateRoot(ctx, root)
	if err != nil {
		return Snapshot{}, err
	}
	if err := opCancelled(ctx, s.Root, s.CampID); err != nil {
		return Snapshot{}, err
	}
	if err := requireSettingsDir(root); err != nil {
		return Snapshot{}, s.wrapRootErr(err, rel)
	}
	if err := rejectLockPath(rel); err != nil {
		return Snapshot{}, s.wrapRootErr(err, rel)
	}
	if err := ordinaryLockFile(root, rel+".lock"); err != nil {
		return Snapshot{}, s.wrapRootErr(err, rel)
	}

	release, err := fsutil.AcquireRootFileLock(ctx, root, rel+".lock")
	if err != nil {
		return Snapshot{}, s.wrapRootErr(err, rel)
	}
	defer release()
	s, err = s.gateRoot(ctx, root)
	if err != nil {
		return Snapshot{}, err
	}

	data, exists, err := readActionFile(root, rel, s.Limits.MaxDocumentBytes)
	if err != nil {
		return Snapshot{}, s.wrapRootErr(err, rel)
	}
	rev := RevisionAbsent
	if exists {
		rev = RevisionOf(data)
	}
	if rev != expected {
		return Snapshot{}, s.revisionConflict(rel)
	}
	next, deleteFile, err := applyOwnerAction(kind, data, exists, prepared)
	if err != nil {
		return Snapshot{}, s.mapApplyErr(rel, err)
	}
	if err := opCancelled(ctx, s.Root, s.CampID); err != nil {
		return Snapshot{}, err
	}
	if !deleteFile && s.Limits.MaxDocumentBytes >= 0 && len(next) > s.Limits.MaxDocumentBytes {
		return Snapshot{}, s.fail(CodeOversized, rel, "document exceeds the size limit")
	}
	if err := persistAction(root, rel, next, deleteFile); err != nil {
		return Snapshot{}, s.wrapRootErr(err, rel)
	}
	return s.readback(ctx, rel)
}

func kindFor(rel, action string) (actionKind, error) {
	switch rel {
	case pinsDocument:
		switch action {
		case "pin":
			return kindPin, nil
		case "unpin":
			return kindUnpin, nil
		}
	case workitemsDocument:
		switch action {
		case "workitem-priority":
			return kindPriority, nil
		case "workitem-group":
			return kindGroup, nil
		}
	}
	return 0, camperrors.Newf("unsupported action %q for %s", action, rel)
}

func validExpectedRevision(expected string) error {
	if expected == RevisionAbsent {
		return nil
	}
	hexPart, ok := strings.CutPrefix(expected, "sha256:")
	if ok && len(hexPart) == 64 {
		valid := true
		for _, c := range hexPart {
			if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
				valid = false
				break
			}
		}
		if valid {
			return nil
		}
	}
	return camperrors.Newf("expected revision must be absent or sha256:<lowercase-hex>")
}

func (s Service) prepareAction(ctx context.Context, rel string, kind actionKind, body []byte) (preparedAction, error) {
	if err := opCancelled(ctx, s.Root, s.CampID); err != nil {
		return preparedAction{}, err
	}
	obj, err := actionObject(body)
	if err != nil {
		return preparedAction{}, s.fail(codeInvalidBody, rel, err.Error(), err)
	}
	switch kind {
	case kindPin:
		if err := exactFields(obj, "name", "path"); err != nil {
			return preparedAction{}, s.fail(codeInvalidBody, rel, err.Error(), err)
		}
		name, err := requiredString(obj, "name")
		if err != nil {
			return preparedAction{}, s.fail(codeInvalidBody, rel, err.Error(), err)
		}
		path, err := requiredString(obj, "path")
		if err != nil {
			return preparedAction{}, s.fail(codeInvalidBody, rel, err.Error(), err)
		}
		pin, err := s.resolvePinTarget(ctx, path)
		if err != nil {
			if cancelled := s.cancelledErr(ctx, err); cancelled != nil {
				return preparedAction{}, cancelled
			}
			return preparedAction{}, s.fail(codeInvalidBody, rel, err.Error(), err)
		}
		pin.Name = name
		return preparedAction{kind: kind, name: name, pin: pin}, nil
	case kindUnpin:
		if err := exactFields(obj, "name"); err != nil {
			return preparedAction{}, s.fail(codeInvalidBody, rel, err.Error(), err)
		}
		name, err := requiredString(obj, "name")
		if err != nil {
			return preparedAction{}, s.fail(codeInvalidBody, rel, err.Error(), err)
		}
		return preparedAction{kind: kind, name: name}, nil
	case kindPriority:
		if err := exactFields(obj, "selector", "priority"); err != nil {
			return preparedAction{}, s.fail(codeInvalidBody, rel, err.Error(), err)
		}
		query, err := requiredString(obj, "selector")
		if err != nil {
			return preparedAction{}, s.fail(codeInvalidBody, rel, err.Error(), err)
		}
		rawLevel, err := requiredString(obj, "priority")
		if err != nil {
			return preparedAction{}, s.fail(codeInvalidBody, rel, err.Error(), err)
		}
		level, clear, err := parseActionPriority(rawLevel)
		if err != nil {
			return preparedAction{}, s.fail(codeInvalidBody, rel, err.Error(), err)
		}
		key, err := s.resolveWorkitemKey(ctx, rel, query)
		if err != nil {
			return preparedAction{}, err
		}
		return preparedAction{kind: kind, key: key, level: level, clearPriority: clear}, nil
	case kindGroup:
		if err := exactFields(obj, "selector", "group"); err != nil {
			return preparedAction{}, s.fail(codeInvalidBody, rel, err.Error(), err)
		}
		query, err := requiredString(obj, "selector")
		if err != nil {
			return preparedAction{}, s.fail(codeInvalidBody, rel, err.Error(), err)
		}
		group, present, err := fieldString(obj, "group")
		if err != nil || !present {
			if err == nil {
				err = camperrors.NewValidation("group", "group is required", nil)
			}
			return preparedAction{}, s.fail(codeInvalidBody, rel, err.Error(), err)
		}
		clear := group == ""
		if !clear && !priority.ValidGroup(group) {
			return preparedAction{}, s.fail(codeInvalidBody, rel, fmt.Sprintf("invalid group %q (use lowercase letters, numbers, dash, or underscore; no leading dash or dot; max 80 chars)", group))
		}
		item, err := selector.Resolve(ctx, s.Root, query, selector.ResolveOptions{ReadOnly: true})
		if err != nil {
			return preparedAction{}, s.mapSelectorErr(ctx, rel, err)
		}
		if !priority.EligibleForAttention(*item) {
			return preparedAction{}, s.fail(codeInvalidBody, rel, "group is only supported for directory-backed workflow workitems")
		}
		return preparedAction{kind: kind, key: item.Key, group: group, clearGroup: clear}, nil
	default:
		return preparedAction{}, s.fail(codeUnsupportedAction, rel, "unsupported action")
	}
}

func (s Service) resolveWorkitemKey(ctx context.Context, rel, query string) (string, error) {
	item, err := selector.Resolve(ctx, s.Root, query, selector.ResolveOptions{ReadOnly: true})
	if err != nil {
		return "", s.mapSelectorErr(ctx, rel, err)
	}
	return item.Key, nil
}

func (s Service) resolvePinTarget(ctx context.Context, rawPath string) (pins.Pin, error) {
	if err := ctx.Err(); err != nil {
		return pins.Pin{}, err
	}
	if strings.TrimSpace(rawPath) == "" {
		return pins.Pin{}, camperrors.NewValidation("path", "path is required", nil)
	}
	target := rawPath
	if !filepath.IsAbs(rawPath) {
		target = filepath.Join(s.Root, rawPath)
	}
	logicalPath, err := filepath.Abs(target)
	if err != nil {
		return pins.Pin{}, camperrors.Wrap(err, "resolve path")
	}
	info, err := os.Stat(logicalPath)
	if err != nil {
		return pins.Pin{}, camperrors.Wrapf(err, "path %q does not exist", logicalPath)
	}
	if !info.IsDir() {
		return pins.Pin{}, camperrors.Newf("path %q is not a directory", logicalPath)
	}
	relPath, relErr := filepath.Rel(s.Root, logicalPath)
	inside := relErr == nil && relPath != ".." && !strings.HasPrefix(relPath, ".."+string(filepath.Separator))
	if inside {
		return pins.Pin{Path: relPath}, nil
	}
	absPath, err := filepath.EvalSymlinks(logicalPath)
	if err != nil {
		return pins.Pin{}, camperrors.Wrapf(err, "resolve symlinks for %q", rawPath)
	}
	relPath, err = filepath.Rel(s.Root, absPath)
	if err != nil {
		return pins.Pin{}, camperrors.Wrapf(err, "compute relative path for %q", absPath)
	}
	if relPath != ".." && !strings.HasPrefix(relPath, "../") {
		return pins.Pin{Path: relPath}, nil
	}
	attached, err := markerResolvesToCampaign(ctx, absPath, s.Root)
	if err != nil {
		return pins.Pin{}, err
	}
	if !attached {
		return pins.Pin{}, camperrors.Newf("pin path %q is outside the camp root (run 'camp attach %s' first to bind this directory to the camp)", absPath, absPath)
	}
	return pins.Pin{AbsPath: absPath}, nil
}

func markerResolvesToCampaign(ctx context.Context, path, campaignRoot string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	detectCtx, cancel := context.WithTimeout(ctx, campaign.DefaultDetectTimeout)
	defer cancel()
	root, err := campaign.Detect(detectCtx, path)
	if err != nil {
		if ctx.Err() != nil {
			return false, ctx.Err()
		}
		return false, nil
	}
	resolvedRoot, err := filepath.EvalSymlinks(campaignRoot)
	if err != nil {
		resolvedRoot = campaignRoot
	}
	return root == resolvedRoot, nil
}

func actionObject(body []byte) (map[string]any, error) {
	value, err := decodeJSONValue(body)
	if err != nil {
		return nil, camperrors.NewValidation("body", "action body must be a JSON object", err)
	}
	obj, ok := value.(map[string]any)
	if !ok || obj == nil {
		return nil, camperrors.NewValidation("body", "action body must be a JSON object", nil)
	}
	return obj, nil
}

func exactFields(obj map[string]any, allowed ...string) error {
	ok := make(map[string]struct{}, len(allowed))
	for _, key := range allowed {
		ok[key] = struct{}{}
	}
	for key := range obj {
		if _, allowed := ok[key]; !allowed {
			return camperrors.NewValidation(key, "unknown field "+key, nil)
		}
	}
	return nil
}

func requiredString(obj map[string]any, key string) (string, error) {
	value, present, err := fieldString(obj, key)
	if err != nil {
		return "", err
	}
	if !present || value == "" {
		return "", camperrors.NewValidation(key, key+" is required", nil)
	}
	return value, nil
}

func fieldString(obj map[string]any, key string) (string, bool, error) {
	value, ok := obj[key]
	if !ok {
		return "", false, nil
	}
	text, ok := value.(string)
	if !ok {
		return "", true, camperrors.NewValidation(key, key+" must be a string", nil)
	}
	return text, true, nil
}

func parseActionPriority(raw string) (priority.ManualPriority, bool, error) {
	switch raw {
	case string(priority.High):
		return priority.High, false, nil
	case string(priority.Medium):
		return priority.Medium, false, nil
	case string(priority.Low):
		return priority.Low, false, nil
	case "clear":
		return priority.None, true, nil
	default:
		return priority.None, false, camperrors.NewValidation("priority", fmt.Sprintf("unknown priority %q (valid: high, medium, low, clear)", raw), nil)
	}
}

func requireSettingsDir(root *os.Root) error {
	info, err := lstatContained(root, settingsDirName)
	if err != nil {
		if os.IsNotExist(err) {
			return camperrors.Newf("settings directory is missing")
		}
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return camperrors.NewValidation("path", "settings must be an ordinary directory", nil)
	}
	return nil
}

func rejectLockPath(rel string) error {
	if strings.HasSuffix(rel, ".lock") {
		return camperrors.NewValidation("path", "lock path is not a document", nil)
	}
	return nil
}

func ordinaryLockFile(root *os.Root, lockRel string) error {
	info, err := lstatContained(root, lockRel)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Size() != 0 {
		return camperrors.NewValidation("path", "lock path is not an ordinary file", nil)
	}
	return nil
}

func readActionFile(root *os.Root, rel string, limit int) ([]byte, bool, error) {
	info, err := lstatContained(root, rel)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, true, camperrors.NewValidation("path", "document is not a regular file inside .campaign", nil)
	}
	if limit >= 0 && info.Size() > int64(limit) {
		return nil, true, errOversized()
	}
	file, err := openRegular(root, rel)
	if err != nil {
		return nil, true, err
	}
	defer func() { _ = file.Close() }()
	data, err := readFileLimited(root, rel, limit)
	if err != nil {
		return nil, true, err
	}
	return data, true, nil
}

func persistAction(root *os.Root, rel string, content []byte, deleteFile bool) error {
	info, err := lstatContained(root, rel)
	exists := true
	if err != nil {
		if os.IsNotExist(err) {
			exists = false
		} else {
			return err
		}
	} else if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return camperrors.NewValidation("path", "document is not a regular file inside .campaign", nil)
	}
	if deleteFile {
		if !exists {
			return nil
		}
		if err := root.Remove(rel); err != nil && !os.IsNotExist(err) {
			return err
		}
		if err := fsutil.SyncRootDir(root, path.Dir(rel)); err != nil {
			return &fsutil.RootWriteError{Err: err, Applied: true}
		}
		return nil
	}
	return fsutil.WriteRootFileAtomically(root, rel, content, 0o644)
}

func applyOwnerAction(kind actionKind, data []byte, exists bool, in preparedAction) ([]byte, bool, error) {
	switch kind {
	case kindPin, kindUnpin:
		next, err := applyPins(data, exists, in)
		return next, false, err
	case kindPriority, kindGroup:
		return applyWorkitems(data, exists, in)
	default:
		return nil, false, camperrors.New("unsupported action")
	}
}

func applyPins(data []byte, exists bool, in preparedAction) ([]byte, error) {
	original := []byte(nil)
	if exists {
		original = data
	}
	if len(bytes.TrimSpace(original)) > 0 {
		if err := strictStoreJSON(original); err != nil {
			return nil, err
		}
	}
	store := pins.NewStore("")
	if err := store.Decode(original); err != nil {
		return nil, malformedErr(err, "parse pins")
	}
	switch in.kind {
	case kindPin:
		if err := store.AddPin(in.pin); err != nil {
			return nil, err
		}
	case kindUnpin:
		if err := store.Remove(in.name); err != nil {
			return nil, err
		}
	}
	return mergePinDocument(original, store)
}

func mergePinDocument(original []byte, store *pins.Store) ([]byte, error) {
	list := store.List()
	if len(bytes.TrimSpace(original)) == 0 {
		return store.Encode()
	}
	var raws []json.RawMessage
	if err := json.Unmarshal(original, &raws); err != nil {
		return nil, malformedErr(err, "parse pins")
	}
	if raws == nil {
		return store.Encode()
	}
	type saved struct {
		name string
		raw  json.RawMessage
		used bool
	}
	savedRows := make([]saved, len(raws))
	for i, raw := range raws {
		trimmed := bytes.TrimSpace(raw)
		if len(trimmed) == 0 || trimmed[0] != '{' {
			return nil, malformedErr(nil, "pin must be an object")
		}
		var pin pins.Pin
		if err := json.Unmarshal(trimmed, &pin); err != nil {
			return nil, malformedErr(err, "parse pins")
		}
		savedRows[i] = saved{name: pin.Name, raw: append(json.RawMessage(nil), raw...)}
	}
	encoded, err := store.Encode()
	if err != nil {
		return nil, err
	}
	var fresh []json.RawMessage
	if err := json.Unmarshal(encoded, &fresh); err != nil || len(fresh) != len(list) {
		return nil, malformedErr(err, "encoded pins")
	}
	out := make([]json.RawMessage, len(list))
	for i, pin := range list {
		kept := false
		for j := range savedRows {
			if savedRows[j].used || savedRows[j].name != pin.Name {
				continue
			}
			savedRows[j].used = true
			out[i] = append(json.RawMessage(nil), savedRows[j].raw...)
			kept = true
			break
		}
		if kept {
			continue
		}
		out[i] = append(json.RawMessage(nil), fresh[i]...)
	}
	buf, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return nil, camperrors.Wrap(err, "marshal pins")
	}
	return buf, nil
}

func applyWorkitems(data []byte, exists bool, in preparedAction) ([]byte, bool, error) {
	store := priority.NewStore()
	var doc map[string]json.RawMessage
	if exists {
		if len(bytes.TrimSpace(data)) == 0 {
			return nil, false, malformedErr(nil, "parse document")
		}
		if err := strictStoreJSON(data); err != nil {
			return nil, false, err
		}
		if err := json.Unmarshal(data, &doc); err != nil || doc == nil {
			if err == nil {
				err = camperrors.New("document must be a JSON object")
			}
			return nil, false, malformedErr(err, "parse document")
		}
		if err := json.Unmarshal(data, store); err != nil {
			return nil, false, malformedErr(err, "parse document")
		}
		if store.ManualPriorities == nil {
			store.ManualPriorities = map[string]priority.PriorityEntry{}
		}
		if store.Attention == nil {
			store.Attention = map[string]priority.AttentionEntry{}
		}
		if store.Version == 0 {
			store.Version = 1
		}
	}
	beforePri := maps.Clone(store.ManualPriorities)
	beforeAtt := maps.Clone(store.Attention)
	switch in.kind {
	case kindPriority:
		if in.clearPriority {
			priority.Clear(store, in.key)
		} else {
			priority.Set(store, in.key, in.level)
		}
	case kindGroup:
		if in.clearGroup {
			priority.ClearGroup(store, in.key)
		} else {
			priority.SetGroup(store, in.key, in.group)
		}
	}
	if priority.IsEmpty(store) && !hasUnknownWorkitemKeys(doc) {
		return nil, true, nil
	}
	next, err := mergeWorkitems(doc, beforePri, beforeAtt, store)
	if err != nil {
		return nil, false, err
	}
	return next, false, nil
}

func hasUnknownWorkitemKeys(doc map[string]json.RawMessage) bool {
	for key := range doc {
		switch key {
		case "version", "manual_priorities", "attention":
		default:
			return true
		}
	}
	return false
}

func mergeWorkitems(doc map[string]json.RawMessage, beforePri map[string]priority.PriorityEntry, beforeAtt map[string]priority.AttentionEntry, store *priority.Store) ([]byte, error) {
	out := maps.Clone(doc)
	if out == nil {
		out = map[string]json.RawMessage{}
	}
	version, err := json.Marshal(store.Version)
	if err != nil {
		return nil, err
	}
	out["version"] = version
	priorities, err := mergePriorityMap(doc["manual_priorities"], beforePri, store.ManualPriorities)
	if err != nil {
		return nil, err
	}
	out["manual_priorities"] = priorities
	if len(store.Attention) == 0 {
		delete(out, "attention")
	} else {
		attention, err := mergeAttentionMap(doc["attention"], beforeAtt, store.Attention)
		if err != nil {
			return nil, err
		}
		out["attention"] = attention
	}
	buf, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return nil, camperrors.Wrap(err, "marshal workitems")
	}
	return append(buf, '\n'), nil
}

func mergePriorityMap(raw json.RawMessage, before, after map[string]priority.PriorityEntry) (json.RawMessage, error) {
	original, err := objectMap(raw)
	if err != nil {
		return nil, err
	}
	out := map[string]json.RawMessage{}
	for key, entry := range after {
		if old, ok := before[key]; ok && old == entry {
			if row, ok := original[key]; ok {
				out[key] = row
				continue
			}
		}
		obj := map[string]json.RawMessage{}
		if row, ok := original[key]; ok {
			if err := json.Unmarshal(row, &obj); err != nil {
				return nil, malformedErr(err, "parse document")
			}
		}
		encodedPriority, err := json.Marshal(string(entry.Priority))
		if err != nil {
			return nil, err
		}
		encodedAt, err := json.Marshal(entry.UpdatedAt)
		if err != nil {
			return nil, err
		}
		obj["priority"] = encodedPriority
		obj["updated_at"] = encodedAt
		merged, err := json.Marshal(obj)
		if err != nil {
			return nil, err
		}
		out[key] = merged
	}
	return json.Marshal(out)
}

func mergeAttentionMap(raw json.RawMessage, before, after map[string]priority.AttentionEntry) (json.RawMessage, error) {
	original, err := objectMap(raw)
	if err != nil {
		return nil, err
	}
	out := map[string]json.RawMessage{}
	for key, entry := range after {
		if old, ok := before[key]; ok && old == entry {
			if row, ok := original[key]; ok {
				out[key] = row
				continue
			}
		}
		obj := map[string]json.RawMessage{}
		if row, ok := original[key]; ok {
			if err := json.Unmarshal(row, &obj); err != nil {
				return nil, malformedErr(err, "parse document")
			}
		}
		if entry.Stage == priority.AttentionNone {
			delete(obj, "stage")
		} else {
			encoded, err := json.Marshal(string(entry.Stage))
			if err != nil {
				return nil, err
			}
			obj["stage"] = encoded
		}
		if entry.Group == "" {
			delete(obj, "group")
		} else {
			encoded, err := json.Marshal(entry.Group)
			if err != nil {
				return nil, err
			}
			obj["group"] = encoded
		}
		encodedAt, err := json.Marshal(entry.UpdatedAt)
		if err != nil {
			return nil, err
		}
		obj["updated_at"] = encodedAt
		merged, err := json.Marshal(obj)
		if err != nil {
			return nil, err
		}
		out[key] = merged
	}
	return json.Marshal(out)
}

func objectMap(raw json.RawMessage) (map[string]json.RawMessage, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return map[string]json.RawMessage{}, nil
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil || obj == nil {
		if err == nil {
			err = camperrors.New("document must be a JSON object")
		}
		return nil, malformedErr(err, "parse document")
	}
	return obj, nil
}

func strictStoreJSON(data []byte) error {
	if _, err := decodeJSONValue(data); err != nil {
		return malformedErr(err, "parse document")
	}
	return nil
}

func malformedErr(err error, msg string) error {
	if msg == "" {
		msg = "parse document"
	}
	return camperrors.WrapJoin(errMalformedDocument, err, msg)
}

func (s Service) readback(ctx context.Context, rel string) (Snapshot, error) {
	snap, err := s.Get(ctx, rel)
	if err != nil {
		return Snapshot{}, &OpError{
			Code:     ReadbackFailedCode,
			Message:  ReadbackFailedMessage,
			Path:     rel,
			CampID:   s.CampID,
			CampRoot: s.Root,
			Applied:  true,
			Err:      err,
		}
	}
	snap.Applied = true
	return snap, nil
}

func (s Service) revisionConflict(rel string) error {
	return &OpError{
		Code:     RevisionConflictCode,
		Message:  RevisionConflictMessage,
		Path:     rel,
		CampID:   s.CampID,
		CampRoot: s.Root,
	}
}

func (s Service) mapApplyErr(rel string, err error) error {
	if err == nil {
		return nil
	}
	var op *OpError
	if errors.As(err, &op) {
		return s.fillOp(rel, op)
	}
	if errors.Is(err, errMalformedDocument) {
		return s.fail(codeMalformedDocument, rel, "document is malformed and was not changed", err)
	}
	if errors.Is(err, camperrors.ErrAlreadyExists) {
		return s.fail(codeAlreadyExists, rel, err.Error(), err)
	}
	if errors.Is(err, camperrors.ErrNotFound) {
		return s.fail(CodeNotFound, rel, err.Error(), err)
	}
	return s.fail(CodeIO, rel, err.Error(), err)
}

func (s Service) mapSelectorErr(ctx context.Context, rel string, err error) error {
	if cancelled := s.cancelledErr(ctx, err); cancelled != nil {
		return cancelled
	}
	if errors.Is(err, selector.ErrSelectorNotFound) {
		return s.fail(CodeNotFound, rel, err.Error(), err)
	}
	if errors.Is(err, selector.ErrSelectorAmbiguous) {
		return s.fail(codeAmbiguousSelector, rel, err.Error(), err)
	}
	var validation *camperrors.ValidationError
	if errors.As(err, &validation) && validation.Message != "" {
		return s.fail(codeInvalidBody, rel, validation.Message, err)
	}
	return s.fail(CodeIO, rel, err.Error(), err)
}

func (s Service) wrapRootErr(err error, rel string) error {
	if err == nil {
		return nil
	}
	if writeWasApplied(err) {
		return s.appliedPersistErr(rel, err)
	}
	if cancelled := s.cancelledErr(context.Background(), err); cancelled != nil {
		return cancelled
	}
	if os.IsNotExist(err) {
		return s.fail(CodeNotFound, rel, "settings directory is missing", err)
	}
	return rootCallErr(err, s.Root, s.CampID, rel)
}

func (s Service) appliedPersistErr(rel string, err error) error {
	return s.appliedMutationError(rel, err)
}

func (s Service) cancelledErr(ctx context.Context, err error) error {
	if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		return nil
	}
	if ctx != nil && ctx.Err() != nil {
		return opCancelled(ctx, s.Root, s.CampID)
	}
	return &OpError{
		Code:     CodeCancelled,
		Message:  "operation cancelled",
		CampID:   s.CampID,
		CampRoot: s.Root,
		Err:      err,
	}
}

func writeWasApplied(err error) bool {
	var applied *fsutil.RootWriteError
	return errors.As(err, &applied) && applied != nil && applied.Applied
}

func (s Service) fillOp(rel string, op *OpError) error {
	if op.Path == "" {
		op.Path = rel
	}
	if op.CampID == "" {
		op.CampID = s.CampID
	}
	if op.CampRoot == "" {
		op.CampRoot = s.Root
	}
	return op
}

func (s Service) fail(code, path, message string, cause ...error) error {
	var err error
	if len(cause) > 0 {
		err = cause[0]
	}
	return &OpError{
		Code:     code,
		Message:  message,
		Path:     path,
		CampID:   s.CampID,
		CampRoot: s.Root,
		Err:      err,
	}
}
