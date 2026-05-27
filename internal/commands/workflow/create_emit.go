package workflow

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

func emitCreateResult(cmd *cobra.Command, plan *createPlan, opts createOptions) error {
	if opts.JSON {
		return emitCreateJSON(cmd, plan, opts)
	}
	return emitCreateHuman(cmd, plan, opts)
}

func emitCreateHuman(cmd *cobra.Command, plan *createPlan, opts createOptions) error {
	w := cmd.OutOrStdout()
	if plan.NoChanges {
		fmt.Fprintf(w, "no changes for workflow %s\n", plan.Type)
		return nil
	}

	if opts.DryRun {
		fmt.Fprintf(w, "plan: create %s\n", strings.TrimRight(plan.WorkflowRel, "/"))
		for _, line := range planActionLines(plan) {
			fmt.Fprintf(w, "  %s\n", line)
		}
		fmt.Fprintln(w, "dry run: nothing written. re-run without --dry-run to apply.")
		return nil
	}

	fmt.Fprintf(w, "created %s\n", strings.TrimRight(plan.WorkflowRel, "/"))
	fmt.Fprintf(w, "  shortcut: %s -> %s\n", plan.Shortcut.Key, plan.WorkflowRel)
	fmt.Fprintf(w, "  workitem type: %s\n", plan.Type)
	fmt.Fprintf(w, "  dungeon dirs: %s\n", strings.Join(statusDirsForOutput(), ", "))
	fmt.Fprintf(w, "next: camp workitem create <slug> --type %s\n", plan.Type)
	return nil
}

func planActionLines(plan *createPlan) []string {
	var lines []string
	rel := strings.TrimRight(plan.WorkflowRel, "/")

	if plan.WorkflowDirCreate {
		lines = append(lines, "create dir "+rel+"/")
	} else {
		lines = append(lines, "skip-exists dir "+rel+"/")
	}

	for _, sub := range terminalDungeonDirs {
		dir := rel + "/" + sub + "/"
		missing := false
		for _, m := range plan.MissingScaffoldDirs {
			if m == sub {
				missing = true
				break
			}
		}
		if missing {
			lines = append(lines, "create dir "+dir)
		} else {
			lines = append(lines, "skip-exists dir "+dir)
		}
		gitkeepMissing := false
		for _, m := range plan.MissingGitKeeps {
			if m == sub {
				gitkeepMissing = true
				break
			}
		}
		if gitkeepMissing {
			lines = append(lines, "create file "+dir+".gitkeep")
		}
	}

	if plan.OBEYWrite {
		lines = append(lines, "create file "+rel+"/OBEY.md")
	} else {
		lines = append(lines, "skip-exists file "+rel+"/OBEY.md")
	}

	switch {
	case plan.Shortcut.NoChange:
		lines = append(lines, "no-op shortcut "+plan.Shortcut.Key+" -> "+plan.Shortcut.Path)
	case plan.Shortcut.Replaced:
		lines = append(lines, "update shortcut "+plan.Shortcut.Key+" -> "+plan.Shortcut.Path+" (was "+plan.Shortcut.Existing+")")
	default:
		lines = append(lines, "create shortcut "+plan.Shortcut.Key+" -> "+plan.Shortcut.Path)
	}

	switch {
	case plan.Concept.NoChange:
		lines = append(lines, "no-op concept "+plan.Concept.Name+" -> "+plan.Concept.Path)
	case plan.Concept.Replaced:
		lines = append(lines, "update concept "+plan.Concept.Name+" -> "+plan.Concept.Path+" (was "+plan.Concept.Existing+")")
	default:
		lines = append(lines, "create concept "+plan.Concept.Name+" -> "+plan.Concept.Path)
	}

	for _, key := range plan.Replaced {
		lines = append(lines, "remove shortcut "+key+" (replaced under --replace)")
	}

	return lines
}

func emitCreateJSON(cmd *cobra.Command, plan *createPlan, opts createOptions) error {
	payload := struct {
		SchemaVersion string       `json:"schema_version"`
		GeneratedAt   time.Time    `json:"generated_at"`
		Type          string       `json:"type"`
		Title         string       `json:"title"`
		WorkflowDir   string       `json:"workflow_dir"`
		StatusDirs    []string     `json:"status_dirs"`
		OBEYWritten   bool         `json:"obey_written"`
		Shortcut      shortcutPlan `json:"shortcut"`
		Concept       conceptPlan  `json:"concept"`
		Replaced      []string     `json:"replaced"`
		NoChanges     bool         `json:"no_changes"`
		DryRun        bool         `json:"dry_run"`
		Applied       bool         `json:"applied"`
	}{
		SchemaVersion: JSONSchemaVersion,
		GeneratedAt:   time.Now().UTC(),
		Type:          plan.Type,
		Title:         plan.Title,
		WorkflowDir:   plan.WorkflowRel,
		StatusDirs:    statusDirsForOutput(),
		OBEYWritten:   plan.OBEYWrite && !opts.DryRun,
		Shortcut:      plan.Shortcut,
		Concept:       plan.Concept,
		Replaced:      append([]string(nil), plan.Replaced...),
		NoChanges:     plan.NoChanges,
		DryRun:        opts.DryRun,
		Applied:       !opts.DryRun && !plan.NoChanges,
	}
	if payload.Replaced == nil {
		payload.Replaced = []string{}
	}

	enc := json.NewEncoder(cmd.OutOrStdout())
	enc.SetIndent("", "  ")
	return enc.Encode(payload)
}

func statusDirsForOutput() []string {
	out := make([]string, len(terminalDungeonDirs))
	for i, sub := range terminalDungeonDirs {
		out[i] = sub + "/"
	}
	return out
}
