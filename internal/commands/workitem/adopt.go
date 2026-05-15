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
	wkitem "github.com/Obedience-Corp/camp/internal/workitem"
)

func newAdoptCommand() *cobra.Command {
	var typeFlag, title, idOverride string
	cmd := &cobra.Command{
		Use:   "adopt <dir>",
		Short: "Attach .workitem metadata to an existing directory",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			return runAdopt(ctx, cmd, args[0], typeFlag, title, idOverride)
		},
	}
	cmd.Flags().StringVar(&typeFlag, "type", "feature", "workitem type (feature, bug, chore, or custom)")
	cmd.Flags().StringVar(&title, "title", "", "human-readable title")
	cmd.Flags().StringVar(&idOverride, "id", "", "override the generated id")
	return cmd
}

func runAdopt(ctx context.Context, cmd *cobra.Command, dir, typeFlag, title, idOverride string) error {
	if err := validateSlug(typeFlag); err != nil {
		return camperrors.NewValidation("type", "invalid type slug: "+err.Error(), nil)
	}

	_, campaignRoot, err := config.LoadCampaignConfigFromCwd(ctx)
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

	meta := wkitem.Metadata{
		Version: wkitem.WorkitemSchemaVersion,
		Kind:    "workitem",
		ID:      id,
		Type:    typeFlag,
		Title:   title,
	}
	buf, err := yaml.Marshal(&meta)
	if err != nil {
		return camperrors.Wrap(err, "marshal metadata")
	}
	if err := atomicWriteFile(markerPath, buf, 0o644); err != nil {
		return err
	}

	fmt.Fprintf(cmd.OutOrStdout(),
		"adopted %s\n  id: %s\n  type: %s\n",
		rel, id, typeFlag)
	return nil
}
