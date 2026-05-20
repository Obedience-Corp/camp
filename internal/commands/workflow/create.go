package workflow

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/nav"
	navindex "github.com/Obedience-Corp/camp/internal/nav/index"
	"github.com/Obedience-Corp/camp/internal/pathsafe"
)

func newCreateCommand() *cobra.Command {
	var shortcut, title string
	var replace bool

	cmd := &cobra.Command{
		Use:   "create <type>",
		Short: "Create a custom workflow collection",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCreate(cmd.Context(), cmd, createOptions{
				Type:     args[0],
				Shortcut: shortcut,
				Title:    title,
				Replace:  replace,
			})
		},
	}

	cmd.Flags().StringVar(&shortcut, "shortcut", "", "navigation shortcut for this workflow")
	cmd.Flags().StringVar(&title, "title", "", "human-readable workflow title")
	cmd.Flags().BoolVar(&replace, "replace", false, "replace an existing shortcut or concept with the same name")
	_ = cmd.MarkFlagRequired("shortcut")

	return cmd
}

type createOptions struct {
	Type     string
	Shortcut string
	Title    string
	Replace  bool
}

func runCreate(ctx context.Context, cmd *cobra.Command, opts createOptions) error {
	if err := validatePathSegment("type", opts.Type); err != nil {
		return err
	}
	if err := validatePathSegment("shortcut", opts.Shortcut); err != nil {
		return err
	}
	shortcutKey := nav.NormalizeNavigationName(opts.Shortcut)

	cfg, campaignRoot, err := config.LoadCampaignConfigFromCwd(ctx)
	if err != nil {
		return camperrors.Wrap(err, "not in a campaign directory")
	}

	workflowTitle := opts.Title
	if workflowTitle == "" {
		workflowTitle = opts.Type
	}

	relPath := path.Join("workflow", opts.Type) + "/"
	absPath := filepath.Join(campaignRoot, filepath.FromSlash(relPath))
	if err := os.MkdirAll(absPath, 0o755); err != nil {
		return camperrors.Wrapf(err, "create workflow directory %s", relPath)
	}

	if err := writeOBEYIfMissing(absPath, opts.Type, workflowTitle); err != nil {
		return err
	}

	if err := upsertShortcut(ctx, campaignRoot, cfg, shortcutKey, relPath, workflowTitle, opts.Replace); err != nil {
		return err
	}
	if err := upsertConcept(ctx, campaignRoot, cfg, opts.Type, relPath, workflowTitle, opts.Replace); err != nil {
		return err
	}
	invalidateNavigationCache(cmd, campaignRoot)

	fmt.Fprintf(cmd.OutOrStdout(),
		"created %s\n  shortcut: %s -> %s\n  workitem type: %s\nnext: camp workitem create <slug> --type %s\n",
		strings.TrimRight(relPath, "/"), shortcutKey, relPath, opts.Type, opts.Type)
	return nil
}

func validatePathSegment(field, value string) error {
	return pathsafe.ValidateSegment(field, value)
}

func writeOBEYIfMissing(absPath, workflowType, title string) error {
	obeyPath := filepath.Join(absPath, "OBEY.md")
	if _, err := os.Stat(obeyPath); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return camperrors.Wrap(err, "stat workflow OBEY.md")
	}

	content := fmt.Sprintf(`# %s

Custom workflow collection for %q workitems.

Create a workitem:

`+"```bash"+`
camp workitem create <slug> --type %s
`+"```"+`
`, title, workflowType, workflowType)

	if err := os.WriteFile(obeyPath, []byte(content), 0o644); err != nil {
		return camperrors.Wrap(err, "write workflow OBEY.md")
	}
	return nil
}

func upsertShortcut(ctx context.Context, campaignRoot string, cfg *config.CampaignConfig, shortcut, relPath, title string, replace bool) error {
	jumps := cfg.Jumps
	if jumps == nil {
		defaults := config.DefaultJumpsConfig()
		jumps = &defaults
	}
	if jumps.Shortcuts == nil {
		jumps.Shortcuts = make(map[string]config.ShortcutConfig)
	}

	shortcutKey := nav.NormalizeNavigationName(shortcut)
	matches := matchingShortcutKeys(jumps.Shortcuts, shortcutKey)
	for _, match := range matches {
		existing := jumps.Shortcuts[match]
		if existing.Path != relPath && !replace {
			return camperrors.NewValidation("shortcut",
				"shortcut "+shortcutKey+" already points to "+existing.Path+"; use --replace to update it", nil)
		}
	}
	for _, match := range matches {
		if match != shortcutKey {
			delete(jumps.Shortcuts, match)
		}
	}

	jumps.Shortcuts[shortcutKey] = config.ShortcutConfig{
		Path:        relPath,
		Description: title + " workflow",
		Source:      config.ShortcutSourceUser,
	}
	cfg.Jumps = jumps

	if err := config.SaveJumpsConfig(ctx, campaignRoot, jumps); err != nil {
		return err
	}
	return nil
}

func matchingShortcutKeys(shortcuts map[string]config.ShortcutConfig, shortcut string) []string {
	normalized := nav.NormalizeNavigationName(shortcut)
	var matches []string
	for key := range shortcuts {
		if nav.NormalizeNavigationName(key) == normalized {
			matches = append(matches, key)
		}
	}
	sort.Strings(matches)
	return matches
}

func upsertConcept(ctx context.Context, campaignRoot string, cfg *config.CampaignConfig, name, relPath, title string, replace bool) error {
	concepts := cfg.ConceptList
	if len(concepts) == 0 {
		concepts = cfg.Concepts()
	}

	for i, concept := range concepts {
		if strings.EqualFold(concept.Name, name) {
			if concept.Path == relPath {
				cfg.ConceptList = concepts
				return nil
			}
			if !replace {
				return camperrors.NewValidation("type",
					"concept "+name+" already points to "+concept.Path+"; use --replace to update it", nil)
			}
			concepts[i] = config.ConceptEntry{
				Name:        name,
				Path:        relPath,
				Description: title + " workflow",
			}
			cfg.ConceptList = concepts
			return config.SaveCampaignConfig(ctx, campaignRoot, cfg)
		}
	}

	concepts = append(concepts, config.ConceptEntry{
		Name:        name,
		Path:        relPath,
		Description: title + " workflow",
	})
	cfg.ConceptList = concepts
	return config.SaveCampaignConfig(ctx, campaignRoot, cfg)
}

func invalidateNavigationCache(cmd *cobra.Command, campaignRoot string) {
	if err := navindex.Delete(campaignRoot); err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "warning: failed to invalidate navigation cache: %v\n", err)
	}
}
