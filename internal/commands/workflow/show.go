package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// errWorkflowNotFound triggers exit code 2 from cobra via RunE.
var errWorkflowNotFound = camperrors.NewValidation("type", "workflow not found", nil)

func newShowCommand() *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "show <type>",
		Short: "Show a workflow collection's config and recent workitems",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runShow(cmd.Context(), cmd, args[0], jsonOut)
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "emit a structured JSON result")
	return cmd
}

func runShow(ctx context.Context, cmd *cobra.Command, typeName string, jsonOut bool) error {
	if err := validatePathSegment("type", typeName); err != nil {
		return err
	}

	cfg, campaignRoot, err := config.LoadCampaignConfigFromCwd(ctx)
	if err != nil {
		return camperrors.Wrap(err, "not in a campaign directory")
	}

	entries, err := enumerateWorkflowEntries(campaignRoot, cfg)
	if err != nil {
		return err
	}

	var entry *workflowEntry
	for i := range entries {
		if strings.EqualFold(entries[i].Type, typeName) {
			entry = &entries[i]
			break
		}
	}
	if entry == nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "unknown workflow type: %s\n", typeName)
		return errWorkflowNotFound
	}

	recent, total, latest, err := recentWorkitems(campaignRoot, entry.Path, 5)
	if err != nil {
		return err
	}
	entry.WorkitemCount = total
	if !latest.IsZero() {
		entry.LastModified = latest
	}

	if jsonOut {
		return emitShowJSON(cmd.OutOrStdout(), *entry, recent)
	}
	return emitShowHuman(cmd.OutOrStdout(), *entry, recent)
}

type recentItem struct {
	Slug     string    `json:"slug"`
	Path     string    `json:"path"`
	Modified time.Time `json:"modified"`
}

// recentWorkitems returns up to limit items by mtime descending plus the total
// marker count and the newest mtime.
func recentWorkitems(campaignRoot, relPath string, limit int) ([]recentItem, int, time.Time, error) {
	absRoot := filepath.Join(campaignRoot, filepath.FromSlash(relPath))
	var (
		items  []recentItem
		latest time.Time
	)
	err := filepath.WalkDir(absRoot, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if errors.Is(walkErr, fs.ErrNotExist) {
				return fs.SkipAll
			}
			return walkErr
		}
		if d.IsDir() {
			name := d.Name()
			if p != absRoot && strings.HasPrefix(name, ".") {
				return fs.SkipDir
			}
			return nil
		}
		if d.Name() != ".workitem" {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		slugDir := filepath.Dir(p)
		rel, err := filepath.Rel(campaignRoot, slugDir)
		if err != nil {
			return err
		}
		items = append(items, recentItem{
			Slug:     filepath.Base(slugDir),
			Path:     filepath.ToSlash(rel),
			Modified: info.ModTime(),
		})
		if info.ModTime().After(latest) {
			latest = info.ModTime()
		}
		return nil
	})
	if err != nil {
		return nil, 0, time.Time{}, camperrors.Wrapf(err, "walk %s", absRoot)
	}

	sort.Slice(items, func(i, j int) bool { return items[i].Modified.After(items[j].Modified) })
	total := len(items)
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	return items, total, latest, nil
}

func emitShowHuman(w io.Writer, entry workflowEntry, recent []recentItem) error {
	fmt.Fprintf(w, "workflow: %s\n", entry.Type)
	if entry.Title != "" {
		fmt.Fprintf(w, "  title: %s\n", entry.Title)
	}
	fmt.Fprintf(w, "  path: %s\n", entry.Path)
	if entry.HasShortcut {
		fmt.Fprintf(w, "  shortcut: %s -> %s\n", entry.ShortcutKey, entry.ShortcutPath)
	} else {
		fmt.Fprintln(w, "  shortcut: (none — add with: camp workflow shortcut add <type> <key>)")
	}
	fmt.Fprintf(w, "  has_concept: %t\n", entry.HasConcept)
	fmt.Fprintf(w, "  has_dir: %t\n", entry.HasDir)
	fmt.Fprintf(w, "  workitems: %d\n", entry.WorkitemCount)
	if entry.WorkitemCount > 0 {
		fmt.Fprintln(w, "recent:")
		for _, r := range recent {
			fmt.Fprintf(w, "  %s  %s\n", r.Modified.Format(time.RFC3339), r.Path)
		}
	}
	return nil
}

func emitShowJSON(w io.Writer, entry workflowEntry, recent []recentItem) error {
	if recent == nil {
		recent = []recentItem{}
	}
	out := struct {
		SchemaVersion string       `json:"schema_version"`
		GeneratedAt   time.Time    `json:"generated_at"`
		Type          string       `json:"type"`
		Title         string       `json:"title,omitempty"`
		Path          string       `json:"path"`
		Shortcut      string       `json:"shortcut,omitempty"`
		ShortcutPath  string       `json:"shortcut_path,omitempty"`
		HasConcept    bool         `json:"has_concept"`
		HasDir        bool         `json:"has_dir"`
		HasShortcut   bool         `json:"has_shortcut"`
		WorkitemCount int          `json:"workitem_count"`
		Recent        []recentItem `json:"recent"`
	}{
		SchemaVersion: JSONSchemaVersion,
		GeneratedAt:   time.Now().UTC(),
		Type:          entry.Type,
		Title:         entry.Title,
		Path:          entry.Path,
		Shortcut:      entry.ShortcutKey,
		ShortcutPath:  entry.ShortcutPath,
		HasConcept:    entry.HasConcept,
		HasDir:        entry.HasDir,
		HasShortcut:   entry.HasShortcut,
		WorkitemCount: entry.WorkitemCount,
		Recent:        recent,
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

// Avoid unused-import warning when no .workitem markers exist.
var _ = os.ReadDir
