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
	} else {
		fmt.Fprintf(w, "created %s\n", strings.TrimRight(plan.WorkflowRel, "/"))
	}
	fmt.Fprintf(w, "  shortcut: %s -> %s\n", plan.Shortcut.Key, plan.WorkflowRel)
	fmt.Fprintf(w, "  workitem type: %s\n", plan.Type)
	fmt.Fprintf(w, "  status dirs: %s\n", strings.Join(statusDirsForOutput(), ", "))
	fmt.Fprintf(w, "next: camp workitem create <slug> --type %s\n", plan.Type)
	if opts.DryRun {
		fmt.Fprintln(w, "dry run: nothing written. re-run without --dry-run to apply.")
	}
	return nil
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
		Applied:       !opts.DryRun,
	}
	if payload.Replaced == nil {
		payload.Replaced = []string{}
	}

	enc := json.NewEncoder(cmd.OutOrStdout())
	enc.SetIndent("", "  ")
	return enc.Encode(payload)
}

func statusDirsForOutput() []string {
	out := make([]string, len(statusDirs))
	for i, sub := range statusDirs {
		out[i] = sub + "/"
	}
	return out
}
