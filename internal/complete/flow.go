package complete

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/Obedience-Corp/camp/internal/nav"
	"github.com/Obedience-Corp/camp/internal/workflow"
)

// CompleteFlow completes paths within a workflow directory.
// Pattern: flow/status/item or flow/status/ for status completion.
// The flow parameter should be the workflow directory name containing a .workflow.yaml.
func CompleteFlow(ctx context.Context, campaignRoot, partial string) ([]string, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	parts := strings.Split(partial, "/")

	switch len(parts) {
	case 1:
		// Complete flow names (directories with .workflow.yaml)
		return completeFlowNames(ctx, campaignRoot, parts[0])
	case 2:
		// Complete status directories from schema
		return completeFlowStatuses(ctx, campaignRoot, parts[0], parts[1])
	default:
		// 3+ parts - complete items within status (handles nested dungeon paths)
		return completeFlowItems(ctx, campaignRoot, parts)
	}
}

// IsFlowDirectory checks if a directory contains a .workflow.yaml file.
func IsFlowDirectory(ctx context.Context, campaignRoot, name string) bool {
	if ctx.Err() != nil {
		return false
	}

	// Search common workflow locations
	searchPaths := []string{
		filepath.Join(campaignRoot, "workflow", name),
		filepath.Join(campaignRoot, "workflow", "design", name),
		filepath.Join(campaignRoot, "workflow", "design", "active", name),
		filepath.Join(campaignRoot, name),
	}

	for _, dir := range searchPaths {
		schemaPath := filepath.Join(dir, workflow.SchemaFileName)
		if _, err := os.Stat(schemaPath); err == nil {
			return true
		}
	}
	return false
}

// FindFlowRoot locates the root directory of a flow by name.
// Returns the absolute path or empty string if not found.
func FindFlowRoot(ctx context.Context, campaignRoot, flowName string) string {
	if ctx.Err() != nil {
		return ""
	}

	// Search common workflow locations
	searchPaths := []string{
		filepath.Join(campaignRoot, "workflow", flowName),
		filepath.Join(campaignRoot, "workflow", "design", flowName),
		filepath.Join(campaignRoot, "workflow", "design", "active", flowName),
		filepath.Join(campaignRoot, flowName),
	}

	for _, dir := range searchPaths {
		schemaPath := filepath.Join(dir, workflow.SchemaFileName)
		if _, err := os.Stat(schemaPath); err == nil {
			return dir
		}
	}
	return ""
}

// completeFlowNames returns flow directory names that match the partial prefix.
func completeFlowNames(ctx context.Context, campaignRoot, partial string) ([]string, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	var candidates []string
	seen := make(map[string]bool)
	partialLower := strings.ToLower(partial)

	// Search common workflow locations
	searchDirs := []string{
		filepath.Join(campaignRoot, "workflow"),
		filepath.Join(campaignRoot, "workflow", "design"),
		filepath.Join(campaignRoot, "workflow", "design", "active"),
	}

	for _, searchDir := range searchDirs {
		entries, err := os.ReadDir(searchDir)
		if err != nil {
			continue
		}

		for _, entry := range entries {
			if ctx.Err() != nil {
				return candidates, ctx.Err()
			}
			if !entry.IsDir() {
				continue
			}
			name := entry.Name()
			if strings.HasPrefix(name, ".") {
				continue
			}
			// Check if this directory has a .workflow.yaml
			schemaPath := filepath.Join(searchDir, name, workflow.SchemaFileName)
			if _, err := os.Stat(schemaPath); err != nil {
				continue
			}
			// Match prefix
			if !seen[name] && strings.HasPrefix(strings.ToLower(name), partialLower) {
				seen[name] = true
				candidates = append(candidates, name+"/")
			}
		}
	}

	return candidates, nil
}

// completeFlowStatuses returns status directories for a flow that match the partial prefix.
func completeFlowStatuses(ctx context.Context, campaignRoot, flowName, partial string) ([]string, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	flowRoot := FindFlowRoot(ctx, campaignRoot, flowName)
	if flowRoot == "" {
		return nil, nil
	}

	// Load the workflow schema
	schemaPath := filepath.Join(flowRoot, workflow.SchemaFileName)
	schema, err := workflow.LoadSchema(ctx, schemaPath)
	if err != nil {
		return nil, nil
	}

	var candidates []string
	partialLower := strings.ToLower(partial)

	// Get all status directories from schema
	for name, dir := range schema.Directories {
		if strings.HasPrefix(strings.ToLower(name), partialLower) {
			if dir.Nested && len(dir.Children) > 0 {
				// Nested directory - show it with trailing slash to indicate more levels
				candidates = append(candidates, flowName+"/"+name+"/")
			} else {
				// Leaf directory
				candidates = append(candidates, flowName+"/"+name+"/")
			}
		}
	}

	return candidates, nil
}

// completeFlowItems returns items within a flow status directory.
// Handles both simple paths (flow/active/item) and nested paths (flow/dungeon/completed/item).
func completeFlowItems(ctx context.Context, campaignRoot string, parts []string) ([]string, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	if len(parts) < 3 {
		return nil, nil
	}

	flowName := parts[0]
	flowRoot := FindFlowRoot(ctx, campaignRoot, flowName)
	if flowRoot == "" {
		return nil, nil
	}

	// Load schema to validate status path
	schemaPath := filepath.Join(flowRoot, workflow.SchemaFileName)
	schema, err := workflow.LoadSchema(ctx, schemaPath)
	if err != nil {
		return nil, nil
	}

	// Determine status path - could be "active" or "dungeon/completed" etc.
	// Try to find the longest valid status prefix
	var statusPath string
	var remaining []string

	// Try two-level nested first (e.g., dungeon/completed)
	if len(parts) >= 4 {
		twoLevel := parts[1] + "/" + parts[2]
		if schema.HasDirectory(twoLevel) {
			statusPath = twoLevel
			remaining = parts[3:]
		}
	}

	// Fall back to single level
	if statusPath == "" {
		if schema.HasDirectory(parts[1]) {
			statusPath = parts[1]
			remaining = parts[2:]
		} else {
			return nil, nil
		}
	}

	if len(remaining) == 0 {
		return nil, nil
	}

	currentDir := filepath.Join(flowRoot, statusPath)
	prefixParts := []string{flowName, statusPath}

	for i := 0; i < len(remaining)-1; i++ {
		if remaining[i] == "" {
			continue
		}
		nextDir := filepath.Join(currentDir, remaining[i])
		info, err := os.Stat(nextDir)
		if err != nil || !info.IsDir() {
			return nil, nil
		}
		currentDir = nextDir
		prefixParts = append(prefixParts, remaining[i])
	}

	itemPartial := remaining[len(remaining)-1]
	entries, err := os.ReadDir(currentDir)
	if err != nil {
		return nil, nil
	}

	var candidates []string
	partialLower := strings.ToLower(itemPartial)

	for _, entry := range entries {
		if ctx.Err() != nil {
			return candidates, ctx.Err()
		}
		name := entry.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		// Skip system files
		if name == "OBEY.md" {
			continue
		}
		if strings.HasPrefix(strings.ToLower(name), partialLower) {
			fullPath := strings.Join(append(prefixParts, name), "/")
			candidates = append(candidates, fullPath)
		}
	}

	return candidates, nil
}

// CompleteFlowInCategory handles flow completion when the user is navigating
// within a category shortcut (like "de" for design).
// It detects if the query contains a flow path pattern and routes to flow completion.
func CompleteFlowInCategory(ctx context.Context, cat nav.Category, campaignRoot, query string) ([]string, bool, error) {
	if ctx.Err() != nil {
		return nil, false, ctx.Err()
	}

	// Only handle queries with "/" that might be flow paths
	if !strings.Contains(query, "/") {
		return nil, false, nil
	}

	parts := strings.Split(query, "/")
	if len(parts) < 2 {
		return nil, false, nil
	}

	// Check if first part is a flow directory
	if !IsFlowDirectory(ctx, campaignRoot, parts[0]) {
		return nil, false, nil
	}

	// It's a flow path - complete it
	candidates, err := CompleteFlow(ctx, campaignRoot, query)
	return candidates, true, err
}
