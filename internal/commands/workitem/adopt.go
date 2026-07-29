package workitem

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/fsutil"
	"github.com/Obedience-Corp/camp/internal/ledger"
	wkitem "github.com/Obedience-Corp/camp/internal/workitem"
	wkaudit "github.com/Obedience-Corp/camp/internal/workitem/audit"
	"github.com/Obedience-Corp/camp/pkg/ledgerkit"
)

func newAdoptCommand() *cobra.Command {
	var typeFlag, title, idOverride, questSelector, fileFlag string
	var tags []string
	var projects []string
	var force bool
	cmd := &cobra.Command{
		Use:     "adopt [dir]",
		Aliases: []string{"init"},
		Short:   "Adopt an existing directory or file as a workitem",
		Long: `Attach workitem metadata to an existing campaign directory or markdown file.

With a directory argument, writes a .workitem marker (the directory must exist
and must not already contain a .workitem). With --file <path.md>, stamps a
kind: workitem frontmatter block onto an existing markdown file without ever
rewriting its body: it prepends a block when the file has none, merges camp's
keys into a foreign block (refusing an ambiguous foreign tags: key without
--force), and updates tags/projects when the file is already a workitem. In all
cases it sets the selected type, title, generated or supplied id, optional quest
link, optional tags, and optional related projects.`,
		Args: cobra.MaximumNArgs(1),
		Annotations: map[string]string{
			"agent_allowed": "false",
			"agent_reason":  "Adopt mutates an existing directory or a file's frontmatter; requires a human decision",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			if fileFlag != "" {
				if len(args) > 0 {
					return camperrors.NewValidation("args", "provide either a directory argument or --file, not both", nil)
				}
				return runAdoptFile(ctx, cmd, fileFlag, typeFlag, title, idOverride, questSelector, tags, projects, force)
			}
			if len(args) != 1 {
				return camperrors.NewValidation("args", "adopt requires a directory argument or --file <path>", nil)
			}
			return runAdopt(ctx, cmd, args[0], typeFlag, title, idOverride, questSelector, tags, projects)
		},
	}
	cmd.Flags().StringVar(&typeFlag, "type", "feature", "workitem type (feature, bug, chore, or custom)")
	cmd.Flags().StringVar(&title, "title", "", "human-readable title")
	cmd.Flags().StringVar(&idOverride, "id", "", "override the generated id")
	cmd.Flags().StringVar(&questSelector, "quest", "", questFlagHelp())
	cmd.Flags().StringArrayVar(&tags, "tag", nil, "add a tag (repeatable, normalized to lowercase kebab-case)")
	cmd.Flags().StringArrayVar(&projects, "project", nil, "add a related project path (repeatable, e.g. projects/camp)")
	cmd.Flags().StringVar(&fileFlag, "file", "", "stamp kind: workitem frontmatter onto a markdown file instead of adopting a directory")
	cmd.Flags().BoolVar(&force, "force", false, "with --file, take ownership of an existing foreign tags: key (union conforming values, drop and report non-conforming ones)")
	return cmd
}

func runAdopt(ctx context.Context, cmd *cobra.Command, dir, typeFlag, title, idOverride, questSelector string, tags, projects []string) error {
	if err := validateSlug(typeFlag); err != nil {
		return camperrors.NewValidation("type", "invalid type slug: "+err.Error(), nil)
	}
	normalizedTags, err := normalizeTags(tags)
	if err != nil {
		return err
	}
	normalizedProjects, err := normalizeProjects(projects)
	if err != nil {
		return err
	}
	if err := wkitem.ValidateProjectPaths(normalizedProjects); err != nil {
		return err
	}

	cfg, campaignRoot, err := config.LoadCampaignConfigFromCwd(ctx)
	if err != nil {
		return camperrors.Wrap(err, "not in a campaign directory")
	}

	rel := dir
	if filepath.IsAbs(dir) {
		var relErr error
		rel, relErr = filepath.Rel(campaignRoot, dir)
		if relErr != nil {
			return camperrors.Wrap(relErr, "resolve dir relative to campaign root")
		}
	}
	if err := validateParentPath(rel); err != nil {
		return err
	}

	target := filepath.Join(campaignRoot, rel)
	info, err := os.Stat(target)
	if err != nil {
		return camperrors.Wrap(err, "stat target dir")
	}
	if !info.IsDir() {
		return camperrors.NewValidation("dir", "target must be a directory: "+target, nil)
	}

	markerPath := filepath.Join(target, ".workitem")
	if _, err := os.Stat(markerPath); err == nil {
		return camperrors.NewValidation("path",
			".workitem already exists at "+markerPath+" — directory is already adopted", nil)
	}

	slug := filepath.Base(rel)
	id, err := generateID(ctx, typeFlag, slug, idOverride, campaignRoot)
	if err != nil {
		return err
	}

	ref, err := deriveUniqueRef(ctx, campaignRoot, cfg, id)
	if err != nil {
		return err
	}
	questID := resolveQuestIDForCreate(ctx, cmd, campaignRoot, questSelector)

	meta := wkitem.Metadata{
		Version:  wkitem.WorkitemSchemaVersion,
		Kind:     "workitem",
		ID:       id,
		Type:     typeFlag,
		Title:    title,
		Ref:      ref,
		QuestID:  questID,
		Tags:     normalizedTags,
		Projects: normalizedProjects,
	}
	buf, err := yaml.Marshal(&meta)
	if err != nil {
		return camperrors.Wrap(err, "marshal metadata")
	}
	if err := fsutil.WriteFileAtomically(markerPath, buf, 0o644); err != nil {
		return err
	}
	// Adoption writes inside an existing directory, which may not update the
	// workflow/type parent mtime watched by passive cache staleness checks.
	invalidateNavigationCache(cmd, campaignRoot)
	appendWorkitemAuditEvent(ctx, cmd, campaignRoot, wkaudit.Event{
		Event: wkaudit.EventAdopt,
		ID:    id,
		Ref:   ref,
		Type:  typeFlag,
		Title: title,
		To:    filepath.ToSlash(rel),
	})

	ledger.NewFromRoot(ctx, campaignRoot, ledger.WarnTo(cmd.ErrOrStderr())).
		Emit(ctx, ledgerkit.KindCreated, ledgerkit.Scope{Workitem: ref, Quest: questID},
			ledger.WithWhy(title),
			ledger.WithPayload(map[string]any{"type": typeFlag, "title": title, "path": rel, "adopted": true}))

	questLine := ""
	if questID != "" {
		questLine = fmt.Sprintf("  quest: %s\n", questID)
	}
	fmt.Fprintf(cmd.OutOrStdout(),
		"adopted %s\n  id: %s\n  ref: %s\n  type: %s\n%s",
		rel, id, ref, typeFlag, questLine)
	return nil
}
