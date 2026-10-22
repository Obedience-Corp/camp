package skills

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/Obedience-Corp/camp/internal/scaffold"
	intskills "github.com/Obedience-Corp/camp/internal/skills"
)

const bundleStatusTool = "bundle"

func bundleStatusEntries(ctx context.Context, root, skillsDir string) ([]skillStatusEntry, error) {
	shipped, err := scaffold.ShippedSkills(ctx, root)
	if err != nil {
		return nil, err
	}
	inv, err := intskills.Classify(ctx, os.DirFS(skillsDir), shipped)
	if err != nil {
		return nil, err
	}
	bundles := inv.Bundles()
	entries := make([]skillStatusEntry, 0, len(bundles))
	for _, b := range bundles {
		entry := skillStatusEntry{
			Tool:  bundleStatusTool,
			Path:  ".campaign/skills/" + b.Slug,
			State: string(b.State),
		}
		switch b.State {
		case intskills.ShippedOutdated, intskills.ShippedMissing:
			entry.Suggestion = fmt.Sprintf("run 'camp skills update %s' to refresh it", b.Slug)
		case intskills.ShippedModified:
			entry.Suggestion = fmt.Sprintf("local changes are preserved; run 'camp skills update --force %s' to replace them", b.Slug)
		case intskills.ShippedSymlink:
			entry.Suggestion = "a shipped file is reached through a symlink; camp never replaces symlinked skill files"
		case intskills.ShippedUnknown:
			entry.Suggestion = fmt.Sprintf("differs with no record of what camp shipped; run 'camp skills update --force %s' to replace it", b.Slug)
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

func printBundleStatus(out io.Writer, entries []skillStatusEntry) error {
	if len(entries) == 0 {
		return nil
	}
	if _, err := fmt.Fprintf(out, "\n%-52s %s\n%-52s %s\n", "Shipped bundle", "State", "--------------", "-----"); err != nil {
		return err
	}
	for _, e := range entries {
		if _, err := fmt.Fprintf(out, "%-52s %s\n", e.Path, e.State); err != nil {
			return err
		}
	}
	wrote := false
	for _, e := range entries {
		if e.Suggestion == "" {
			continue
		}
		if _, err := fmt.Fprintf(out, "\n  %s: %s", e.Path, e.Suggestion); err != nil {
			return err
		}
		wrote = true
	}
	if wrote {
		if _, err := fmt.Fprintln(out); err != nil {
			return err
		}
	}
	return nil
}
