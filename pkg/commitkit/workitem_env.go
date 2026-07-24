package commitkit

import (
	"path/filepath"

	wkitem "github.com/Obedience-Corp/camp/internal/workitem"
)

const commitAmendEnv = "CAMP_COMMIT_AMEND=1"

// WithCommitAmendEnv adds the Camp-to-writer amend signal when amend is true.
// Writers use this explicit contract instead of inferring amend mode from an
// empty staged index.
func WithCommitAmendEnv(env []string, amend bool) []string {
	if !amend {
		return env
	}
	return append(env, commitAmendEnv)
}

// WorkitemEnv builds the CAMP_WORKITEM_* environment variables that the
// auto-write commit-message hook receives. Returns nil when wi is nil so the
// caller's "no workitem context" branch is just `append(env, nil...)`.
//
// The CAMP_WORKITEM_PATH value is campaign-relative when campaignRoot is
// non-empty and the workitem is inside it; otherwise it falls back to the
// raw path.
//
// CAMP_WORKITEM_QUEST_ID is emitted only when the workitem carries a
// non-empty quest_id. It is absent when unset.
func WorkitemEnv(wi *wkitem.WorkItem, campaignRoot string) []string {
	if wi == nil {
		return nil
	}
	path := wi.RelativePath
	if campaignRoot != "" {
		if rel, err := filepath.Rel(campaignRoot, filepath.Join(campaignRoot, wi.RelativePath)); err == nil {
			path = filepath.ToSlash(rel)
		}
	}
	env := []string{
		"CAMP_WORKITEM_ID=" + wi.StableID,
		"CAMP_WORKITEM_REF=" + sourceMetaString(wi, "ref"),
		"CAMP_WORKITEM_TYPE=" + string(wi.WorkflowType),
		"CAMP_WORKITEM_TITLE=" + wi.Title,
		"CAMP_WORKITEM_PATH=" + path,
	}
	if questID := sourceMetaString(wi, "quest_id"); questID != "" {
		env = append(env, "CAMP_WORKITEM_QUEST_ID="+questID)
	}
	return env
}

func sourceMetaString(wi *wkitem.WorkItem, key string) string {
	if wi == nil || wi.SourceMetadata == nil {
		return ""
	}
	if v, ok := wi.SourceMetadata[key].(string); ok {
		return v
	}
	return ""
}
