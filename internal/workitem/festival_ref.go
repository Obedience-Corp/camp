package workitem

import (
	"path/filepath"
	"strings"
)

// FestivalRefFromID extracts the festival ref token (the FE-<ref> payload) from
// a festival id or directory name: the trailing id segment, e.g. SC0001 from
// "sync-clone-transport-SC0001", from "festivals/planning/...-SC0001", or from
// a bare "SC0001". Returns "" when no ref-shaped token is present.
func FestivalRefFromID(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	value = filepath.Base(filepath.ToSlash(value))
	if isFestivalRefToken(value) {
		return value
	}
	if idx := strings.LastIndex(value, "-"); idx >= 0 {
		if tail := value[idx+1:]; isFestivalRefToken(tail) {
			return tail
		}
	}
	return ""
}

// FestivalRef returns the festival ref for a festival-typed workitem, derived
// from its fest.yaml id (SourceID) and falling back to its directory name.
// Returns "" for any non-festival workitem. This is the FE-<ref> segment a
// commit made in a festival-linked worktree should carry.
func FestivalRef(wi *WorkItem) string {
	if wi == nil || wi.WorkflowType != WorkflowTypeFestival {
		return ""
	}
	src := wi.SourceID
	if src == "" {
		src = wi.RelativePath
	}
	return FestivalRefFromID(src)
}

func isFestivalRefToken(value string) bool {
	if value == "" || len(value) > 32 {
		return false
	}
	for _, r := range value {
		switch {
		case r >= '0' && r <= '9', r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z':
		default:
			return false
		}
	}
	return true
}
