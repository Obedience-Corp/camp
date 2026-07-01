package notes

import "regexp"

type RawEntry struct {
	Subject       string
	ChildSubjects []string
	IsMerge       bool
}

type Group struct {
	Change   Change
	Children []Change
}

var statusChangeRe = regexp.MustCompile(`(?i)^chore\(fest\):\s*status change`)

func BuildGroups(entries []RawEntry) []Group {
	var groups []Group
	for _, entry := range entries {
		if isNoiseSubject(entry.Subject) {
			continue
		}
		headline, ok := ParseCommitSubject(entry.Subject)
		if !ok {
			continue
		}
		group := Group{Change: headline}
		if entry.IsMerge {
			group.Children = buildChildren(entry.ChildSubjects, headline)
		}
		groups = append(groups, group)
	}
	return groups
}

func buildChildren(subjects []string, headline Change) []Change {
	var children []Change
	seen := map[string]struct{}{}
	headlineKey := normalizedChangeKey(headline)
	for _, subject := range subjects {
		if isNoiseSubject(subject) {
			continue
		}
		child, ok := ParseCommitSubject(subject)
		if !ok {
			continue
		}
		key := normalizedChangeKey(child)
		if key == headlineKey {
			continue
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		children = append(children, child)
	}
	return children
}

func isNoiseSubject(subject string) bool {
	return statusChangeRe.MatchString(stripBracketTags(subject))
}

func LeafChanges(groups []Group) []Change {
	var leaves []Change
	for _, group := range groups {
		if len(group.Children) == 0 {
			leaves = append(leaves, group.Change)
			continue
		}
		leaves = append(leaves, group.Children...)
	}
	return leaves
}

func headlineChanges(groups []Group) []Change {
	changes := make([]Change, 0, len(groups))
	for _, group := range groups {
		changes = append(changes, group.Change)
	}
	return changes
}

func filterGroups(groups []Group, category ChangeCategory) []Group {
	var filtered []Group
	for _, group := range groups {
		if group.Change.Category == category {
			filtered = append(filtered, group)
		}
	}
	return filtered
}
