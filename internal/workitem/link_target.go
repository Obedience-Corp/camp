package workitem

// LinkWorkitemID returns the identifier to store as a link's workitem_id for
// wi. It is the single-segment id the selector can resolve back to wi:
//   - an adopted workitem's stable .workitem id, when present;
//   - a festival's fest.yaml id (e.g. SC0001), which is how a festival becomes
//     a first-class link target after `camp workitem promote --target festival`;
//   - otherwise the workitem key.
//
// The links validator requires workitem_id to be a single path segment, so a
// festival's slash-bearing key ("festival:festivals/...") is never a valid id;
// its fest.yaml id is.
func LinkWorkitemID(wi *WorkItem) string {
	if wi == nil {
		return ""
	}
	if wi.StableID != "" {
		return wi.StableID
	}
	if wi.WorkflowType == WorkflowTypeFestival && wi.SourceID != "" {
		return wi.SourceID
	}
	return wi.Key
}
