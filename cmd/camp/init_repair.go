package main

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/Obedience-Corp/camp/internal/git/commit"
	"github.com/Obedience-Corp/camp/internal/scaffold"
	"github.com/Obedience-Corp/camp/internal/ui"
)

// commitRepairChanges creates a git commit after a successful repair.
func commitRepairChanges(ctx context.Context, initResult *scaffold.InitResult, plan *scaffold.RepairPlan, migrationCount int, w initWriters) {
	hasChanges := len(initResult.DirsCreated) > 0 || len(initResult.FilesCreated) > 0 || migrationCount > 0
	if plan != nil && len(plan.IntentMigrations) > 0 {
		hasChanges = true
	}
	if !hasChanges {
		return
	}

	cfg, err := config.LoadCampaignConfig(ctx, initResult.CampaignRoot)
	if err != nil {
		return
	}

	description := buildRepairCommitMessage(initResult, plan, migrationCount)
	files := buildRepairCommitFiles(initResult, plan)

	result := commit.Repair(ctx, commit.RepairOptions{
		Options: commit.Options{
			CampaignRoot:  initResult.CampaignRoot,
			CampaignID:    cfg.ID,
			Files:         files,
			SelectiveOnly: true,
		},
		Description: description,
	})

	if result.Committed {
		writef(w.humanOut, "\n%s %s\n", ui.SuccessIcon(), result.Message)
	} else if result.Message != "" {
		writef(w.humanOut, "\n%s %s\n", ui.InfoIcon(), result.Message)
	}
}

func buildRepairCommitFiles(initResult *scaffold.InitResult, plan *scaffold.RepairPlan) []string {
	files := make([]string, 0, len(initResult.FilesCreated)+len(initResult.DirsCreated))
	files = append(files, initResult.FilesCreated...)
	files = append(files, initResult.DirsCreated...)

	if plan != nil {
		for _, m := range plan.Migrations {
			for _, item := range m.Items {
				files = append(files,
					filepath.Join(m.Source, item),
					filepath.Join(m.Dest, item),
				)
			}
		}
		for _, m := range plan.IntentMigrations {
			for _, item := range m.Items {
				files = append(files,
					filepath.Join(m.Source, item),
					filepath.Join(m.Dest, item),
				)
			}
		}
	}

	return commit.NormalizeFiles(initResult.CampaignRoot, files...)
}

// buildRepairCommitMessage constructs a descriptive commit body for repair operations.
func buildRepairCommitMessage(initResult *scaffold.InitResult, plan *scaffold.RepairPlan, migrationCount int) string {
	var b strings.Builder

	if len(initResult.DirsCreated) > 0 {
		fmt.Fprintf(&b, "Directories created:\n")
		for _, d := range initResult.DirsCreated {
			fmt.Fprintf(&b, "  - %s\n", d)
		}
		b.WriteString("\n")
	}

	if len(initResult.FilesCreated) > 0 {
		fmt.Fprintf(&b, "Files created:\n")
		for _, f := range initResult.FilesCreated {
			fmt.Fprintf(&b, "  - %s\n", f)
		}
		b.WriteString("\n")
	}

	if plan != nil && migrationCount > 0 {
		fmt.Fprintf(&b, "Migrated %d item(s):\n", migrationCount)
		for _, m := range plan.Migrations {
			for _, item := range m.Items {
				fmt.Fprintf(&b, "  - %s → %s\n", filepath.Join(m.Source, item), m.Dest)
			}
		}
	}

	if plan != nil && len(plan.IntentMigrations) > 0 {
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "Migrated %d legacy intent item(s):\n", countMigrationItems(plan.IntentMigrations))
		for _, m := range plan.IntentMigrations {
			for _, item := range m.Items {
				fmt.Fprintf(&b, "  - %s → %s\n", filepath.Join(m.Source, item), m.Dest)
			}
		}
	}

	return strings.TrimRight(b.String(), "\n")
}

func countMigrationItems(migrations []scaffold.MigrationAction) int {
	total := 0
	for _, m := range migrations {
		total += len(m.Items)
	}
	return total
}

// printRepairDiff displays the proposed repair changes as a colored diff.
func printRepairDiff(plan *scaffold.RepairPlan, w initWriters) {
	writeLine(w.humanOut, ui.Subheader("Repair Preview"))
	writeLine(w.humanOut)

	for _, c := range plan.Changes {
		switch c.Type {
		case scaffold.RepairAdd:
			writef(w.humanOut, "  %s  %s  %s\n",
				ui.Success("+"),
				ui.Success(c.Key),
				ui.Dim("("+c.Description+")"),
			)
		case scaffold.RepairModify:
			writef(w.humanOut, "  %s  %s  %s\n",
				ui.Warning("~"),
				ui.Warning(c.Key),
				ui.Dim("("+c.Description+")"),
			)
		case scaffold.RepairPreserve:
			writef(w.humanOut, "  %s  %s  %s\n",
				ui.Dim("✓"),
				ui.Value(c.Key),
				ui.Dim("(user-defined, preserved)"),
			)
		case scaffold.RepairMigrate:
			writef(w.humanOut, "  %s  %s  %s\n",
				ui.Warning("→"),
				ui.Value(c.Key),
				ui.Dim(c.Description),
			)
		}
	}
}
