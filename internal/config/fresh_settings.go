package config

import (
	"context"
	"strconv"

	"gopkg.in/yaml.v3"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// SetFreshBranch writes the working branch `camp fresh` creates after syncing.
// An empty projectName targets the global key; otherwise the per-project
// override. A nil branch clears the key, restoring inheritance: a project falls
// back to the global branch, and the global falls back to creating no branch.
//
// A non-nil pointer to "" is meaningful only in a project scope, where it
// records "create no branch for this project" explicitly, so the project keeps
// that behavior if a global branch is configured later. The global key has no
// such distinction, since an absent key and an empty one both mean no branch.
func SetFreshBranch(ctx context.Context, campaignRoot, projectName string, branch *string) error {
	return withFreshConfigLock(ctx, campaignRoot, func(mapping *yaml.Node) error {
		if branch == nil || (projectName == "" && *branch == "") {
			return clearFreshKey(mapping, projectName, "branch")
		}
		return setFreshScalar(mapping, projectName, "branch", *branch, "!!str")
	})
}

// SetFreshPushUpstream writes whether a newly created working branch is pushed
// with --set-upstream. An empty projectName targets the global key; otherwise
// the per-project override. A nil value clears the key, so a project inherits
// the global setting and the global falls back to the built-in default (true).
func SetFreshPushUpstream(ctx context.Context, campaignRoot, projectName string, value *bool) error {
	return withFreshConfigLock(ctx, campaignRoot, func(mapping *yaml.Node) error {
		return setFreshBool(mapping, projectName, "push_upstream", value)
	})
}

// SetFreshPrune writes whether merged branches are pruned. Global only: there
// is no projectName parameter because FreshProjectConfig has no prune field.
// A nil value clears the key so the built-in default (true) applies. Callers
// that need to keep a project scope honest (and refuse a project write) live
// in the configure TUI, which redirects those edits to Global defaults.
func SetFreshPrune(ctx context.Context, campaignRoot string, value *bool) error {
	return withFreshConfigLock(ctx, campaignRoot, func(mapping *yaml.Node) error {
		return setFreshBool(mapping, "", "prune", value)
	})
}

// SetFreshPruneRemote writes whether stale remote tracking refs are pruned.
// Global only, same contract as SetFreshPrune: no projectName parameter, nil
// clears the key back to the built-in default (true).
func SetFreshPruneRemote(ctx context.Context, campaignRoot string, value *bool) error {
	return withFreshConfigLock(ctx, campaignRoot, func(mapping *yaml.Node) error {
		return setFreshBool(mapping, "", "prune_remote", value)
	})
}

// ProjectOverrideKeys counts the fresh.yaml keys a project sets for itself.
// It is what distinguishes a project that deviates from the global defaults
// from one that merely exists, and counts keys rather than follow-up steps so
// a project whose only override is a branch is not reported as unconfigured.
func ProjectOverrideKeys(pc FreshProjectConfig) int {
	count := 0
	if pc.Branch != nil {
		count++
	}
	if pc.PushUpstream != nil {
		count++
	}
	if pc.FollowUp != nil {
		count++
	}
	return count
}

func setFreshBool(mapping *yaml.Node, projectName, key string, value *bool) error {
	if value == nil {
		return clearFreshKey(mapping, projectName, key)
	}
	return setFreshScalar(mapping, projectName, key, strconv.FormatBool(*value), "!!bool")
}

// setFreshScalar writes key: value into the requested scope, creating the
// projects.<name> wrapper when needed and replacing any existing value in
// place so surrounding keys and their comments keep their positions.
func setFreshScalar(mapping *yaml.Node, projectName, key, value, tag string) error {
	scope, err := freshScopeMapping(mapping, projectName, true)
	if err != nil {
		return err
	}
	for i := 0; i+1 < len(scope.Content); i += 2 {
		k := scope.Content[i]
		if k.Kind == yaml.ScalarNode && k.Value == key {
			existing := scope.Content[i+1]
			if existing.Kind != yaml.ScalarNode {
				return camperrors.NewValidation(key, "has an unexpected shape in fresh.yaml", nil)
			}
			existing.Tag = tag
			existing.Value = value
			existing.Style = 0
			return nil
		}
	}
	scope.Content = append(scope.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
		&yaml.Node{Kind: yaml.ScalarNode, Tag: tag, Value: value},
	)
	return nil
}

// clearFreshKey removes key from the requested scope and cascades the cleanup
// upward, so dropping a project's last override does not leave an empty
// projects.<name> mapping behind.
func clearFreshKey(mapping *yaml.Node, projectName, key string) error {
	scope, err := freshScopeMapping(mapping, projectName, false)
	if err != nil || scope == nil {
		return err
	}
	removeMappingKey(scope, key)
	pruneEmptyProjectScope(mapping, projectName)
	return nil
}

// freshScopeMapping returns the mapping node a scope's keys live under: the
// document root for the global scope, or projects.<projectName> for a project.
func freshScopeMapping(mapping *yaml.Node, projectName string, create bool) (*yaml.Node, error) {
	if projectName == "" {
		return mapping, nil
	}
	projectsNode, err := mappingChild(mapping, "projects", yaml.MappingNode, "!!map", create)
	if err != nil || projectsNode == nil {
		return nil, err
	}
	return mappingChild(projectsNode, projectName, yaml.MappingNode, "!!map", create)
}

// pruneEmptyProjectScope drops an emptied projects.<projectName> entry and, in
// turn, an emptied projects mapping. The global scope is the document root and
// is never pruned.
func pruneEmptyProjectScope(mapping *yaml.Node, projectName string) {
	if projectName == "" {
		return
	}
	projectsNode, _ := mappingChild(mapping, "projects", yaml.MappingNode, "!!map", false)
	if projectsNode == nil {
		return
	}
	projectNode, _ := mappingChild(projectsNode, projectName, yaml.MappingNode, "!!map", false)
	if projectNode != nil && len(projectNode.Content) == 0 {
		removeMappingKey(projectsNode, projectName)
	}
	if len(projectsNode.Content) == 0 {
		removeMappingKey(mapping, "projects")
	}
}
