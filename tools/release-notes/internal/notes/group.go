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
		leaves = append(leaves, groupLeaves(group)...)
	}
	return leaves
}

// groupLeaves returns the changes a group contributes to summary counts. A
// headline only counts when no child restates its category, so grouped PRs
// are counted by their constituents without losing a headline whose category
// no child covers.
func groupLeaves(group Group) []Change {
	if len(group.Children) == 0 {
		return []Change{group.Change}
	}
	leaves := make([]Change, 0, len(group.Children)+1)
	if !hasCategory(group.Children, group.Change.Category) {
		leaves = append(leaves, group.Change)
	}
	return append(leaves, group.Children...)
}

func hasCategory(changes []Change, category ChangeCategory) bool {
	for _, change := range changes {
		if change.Category == category {
			return true
		}
	}
	return false
}

func headlineChanges(groups []Group) []Change {
	changes := make([]Change, 0, len(groups))
	for _, group := range groups {
		changes = append(changes, group.Change)
	}
	return changes
}

// sectionGroups projects groups into one category's section: children of the
// category render nested under their headline (repeated per section as
// grouping context), and a headline appears bare only when it is itself a
// counted leaf of the category. Section contents therefore always agree with
// the LeafChanges counts.
func sectionGroups(groups []Group, category ChangeCategory) []Group {
	var out []Group
	for _, group := range groups {
		children := filterChanges(group.Children, category)
		switch {
		case len(children) > 0:
			out = append(out, Group{Change: group.Change, Children: children})
		case group.Change.Category == category:
			out = append(out, Group{Change: group.Change})
		}
	}
	return out
}

func filterChanges(changes []Change, category ChangeCategory) []Change {
	var out []Change
	for _, change := range changes {
		if change.Category == category {
			out = append(out, change)
		}
	}
	return out
}
