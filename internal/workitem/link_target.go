package workitem

// SourceDeclaredID returns the single-segment id a workitem type declares inside
// its own source document, or "" for types that declare none.
//
// Festivals declare it in fest.yaml (e.g. SC0001) and intents in their markdown
// frontmatter (e.g. add-dark-mode-toggle-20260119-153412); discovery lands both
// on SourceID. Types whose identity instead comes from an adopted .workitem
// marker are absent here because that id is StableID.
//
// This is the shared rule behind "which single-segment string names this
// workitem": LinkWorkitemID stores it and the selector resolves it, so an id
// that can be written to a link can always be read back.
func SourceDeclaredID(wi *WorkItem) string {
	if wi == nil {
		return ""
	}
	switch wi.WorkflowType {
	case WorkflowTypeFestival, WorkflowTypeIntent:
		return wi.SourceID
	default:
		return ""
	}
}

// LinkWorkitemID returns the identifier to store as a link's workitem_id for
// wi. It is the single-segment id the selector can resolve back to wi:
//   - an adopted workitem's stable .workitem id, when present;
//   - the source-declared id for types that carry one (see SourceDeclaredID),
//     which is how a festival becomes a first-class link target after
//     `camp workitem promote --target festival` and how an intent becomes one
//     without being adopted;
//   - otherwise the workitem key.
//
// The links validator requires workitem_id to be a single path segment, so a
// slash-bearing key ("festival:festivals/...", "intent:.campaign/intents/...")
// is never a valid id; the source-declared id is. The key form belongs in
// workitem_key.
func LinkWorkitemID(wi *WorkItem) string {
	if wi == nil {
		return ""
	}
	if wi.StableID != "" {
		return wi.StableID
	}
	if id := SourceDeclaredID(wi); id != "" {
		return id
	}
	return wi.Key
}

// LinkMatchesWorkitem reports whether a link's stored workitem_id/workitem_key
// pair addresses wi.
//
// It accepts the id LinkWorkitemID mints today plus the forms an earlier camp
// may have written, so recognizing an existing link never requires a registry
// migration: a row saved under the path-derived key still matches the workitem
// it names, on either field.
func LinkMatchesWorkitem(wi *WorkItem, workitemID, workitemKey string) bool {
	if wi == nil {
		return false
	}
	if workitemID != "" {
		if workitemID == LinkWorkitemID(wi) || workitemID == wi.StableID || workitemID == wi.Key {
			return true
		}
	}
	return workitemKey != "" && workitemKey == wi.Key
}
