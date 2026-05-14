package workitem

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	wkitem "github.com/Obedience-Corp/camp/internal/workitem"
)

var slugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,79}$`)

func newCreateCommand() *cobra.Command {
	var typeFlag, title, idOverride, dirOverride string
	cmd := &cobra.Command{
		Use:   "create <slug>",
		Short: "Create a new workitem with v1 minimum metadata",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			return runCreate(ctx, cmd, args[0], typeFlag, title, idOverride, dirOverride)
		},
	}
	cmd.Flags().StringVar(&typeFlag, "type", "feature", "workitem type (feature, bug, chore, or custom)")
	cmd.Flags().StringVar(&title, "title", "", "human-readable title")
	cmd.Flags().StringVar(&idOverride, "id", "", "override the generated id")
	cmd.Flags().StringVar(&dirOverride, "dir", "", "parent dir override (default: workflow/<type>)")
	return cmd
}

func runCreate(ctx context.Context, cmd *cobra.Command, slug, typeFlag, title, idOverride, dirOverride string) error {
	if err := validateSlug(slug); err != nil {
		return err
	}
	if err := validateSlug(typeFlag); err != nil {
		return camperrors.NewValidation("type", "invalid type slug: "+err.Error(), nil)
	}

	_, campaignRoot, err := config.LoadCampaignConfigFromCwd(ctx)
	if err != nil {
		return camperrors.Wrap(err, "not in a campaign directory")
	}

	id, err := generateID(ctx, typeFlag, slug, idOverride, campaignRoot)
	if err != nil {
		return err
	}

	parent := dirOverride
	if parent == "" {
		parent = filepath.Join("workflow", typeFlag)
	}
	if err := validateParentPath(parent); err != nil {
		return err
	}

	target := filepath.Join(campaignRoot, parent, slug)
	if _, err := os.Stat(target); err == nil {
		return camperrors.NewValidation("path",
			"target directory already exists: "+target+" — use `camp workitem adopt` to attach metadata to an existing dir", nil)
	}
	if err := os.MkdirAll(target, 0o755); err != nil {
		return camperrors.Wrap(err, "create directory")
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
	if err := atomicWriteFile(filepath.Join(target, ".workitem"), buf, 0o644); err != nil {
		return err
	}

	rel := filepath.Join(parent, slug)
	fmt.Fprintf(cmd.OutOrStdout(),
		"created %s\n  id: %s\n  type: %s\nnext: cd %s && fest create workflow %s\n",
		rel, id, typeFlag, rel, slug)
	return nil
}

func validateSlug(slug string) error {
	if slug == "" {
		return camperrors.NewValidation("slug", "slug must not be empty", nil)
	}
	if !slugPattern.MatchString(slug) {
		return camperrors.NewValidation("slug",
			"invalid slug "+slug+": use lowercase letters, digits, '-', '_'; start with [a-z0-9]; max 80 chars", nil)
	}
	return nil
}

func validateParentPath(parent string) error {
	clean := filepath.Clean(parent)
	if filepath.IsAbs(clean) {
		return camperrors.NewValidation("dir", "parent dir must be relative to campaign root", nil)
	}
	if strings.HasPrefix(clean, "..") {
		return camperrors.NewValidation("dir", "parent dir must not escape campaign root", nil)
	}
	return nil
}

func generateID(ctx context.Context, typeStr, slug, override, campaignRoot string) (string, error) {
	if override != "" {
		if err := validateSlug(override); err != nil {
			return "", camperrors.NewValidation("id",
				"invalid id override "+override+": ids must be slug-safe (lowercase a-z0-9_-)", nil)
		}
		return override, nil
	}
	base := typeStr + "-" + slug + "-" + time.Now().UTC().Format("2006-01-02")
	if !idCollides(ctx, campaignRoot, base) {
		return base, nil
	}
	for i := 0; i < 32; i++ {
		var b [3]byte
		if _, err := rand.Read(b[:]); err != nil {
			return "", camperrors.Wrap(err, "generate id suffix")
		}
		candidate := base + "-" + hex.EncodeToString(b[:])
		if !idCollides(ctx, campaignRoot, candidate) {
			return candidate, nil
		}
	}
	return "", camperrors.NewValidation("id", "could not generate a unique id after 32 attempts", nil)
}

func idCollides(ctx context.Context, campaignRoot, id string) bool {
	if campaignRoot == "" {
		return false
	}
	root := filepath.Join(campaignRoot, "workflow")
	collision := false
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if d.IsDir() || filepath.Base(path) != ".workitem" {
			return nil
		}
		raw, rErr := os.ReadFile(path)
		if rErr != nil {
			return nil
		}
		var m wkitem.Metadata
		if uErr := yaml.Unmarshal(raw, &m); uErr != nil {
			return nil
		}
		if m.ID == id {
			collision = true
			return filepath.SkipAll
		}
		return nil
	})
	return collision
}

func atomicWriteFile(path string, data []byte, mode os.FileMode) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, mode); err != nil {
		return camperrors.Wrap(err, "write tmp file")
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return camperrors.Wrap(err, "rename tmp file")
	}
	return nil
}
