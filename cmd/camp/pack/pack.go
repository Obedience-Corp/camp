// Package pack implements camp pack / camp unbundle for Festival Bundles.
package pack

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/obey-shared/festivalbundle"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// Cmd is the pack command family root (also usable as pack directly via PackCmd).
var Cmd = &cobra.Command{
	Use:     "pack",
	Short:   "Pack a directory into a portable .festival bundle",
	Long:    "Pack a work-unit directory into a compressed .festival archive using the Festival Bundle format.",
	GroupID: "planning",
	Args:    cobra.ExactArgs(1),
	RunE:    runPack,
}

var (
	packOutput  string
	packKind    string
	packName    string
	packStrict  bool
	packNoSent  bool
	packJSON    bool
	packCreator string
)

func init() {
	Cmd.Flags().StringVarP(&packOutput, "output", "o", "", "output .festival path (required)")
	Cmd.Flags().StringVar(&packKind, "kind", "", "bundle kind (explore, design, intent, note, …); inferred from path when empty")
	Cmd.Flags().StringVar(&packName, "name", "", "human-readable bundle name (default: directory name)")
	Cmd.Flags().BoolVar(&packStrict, "strict", false, "fail if out-of-root linked files are missing")
	Cmd.Flags().BoolVar(&packNoSent, "no-sent-record", false, "do not write .bundles/sent on the source tree")
	Cmd.Flags().BoolVar(&packJSON, "json", false, "emit info.json as JSON on stdout")
	Cmd.Flags().StringVar(&packCreator, "creator", "camp", "bundle.creator identity")
	_ = Cmd.MarkFlagRequired("output")
}

func runPack(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	source, err := filepath.Abs(args[0])
	if err != nil {
		return camperrors.NewCommand(cmd.CommandPath(), 1, "resolve source", err)
	}
	if st, err := os.Stat(source); err != nil || !st.IsDir() {
		return camperrors.NewCommand(cmd.CommandPath(), 1, "source", fmt.Errorf("not a directory: %s", source))
	}

	kind := packKind
	if kind == "" {
		kind = inferKind(source)
	}
	name := packName
	if name == "" {
		name = filepath.Base(source)
	}

	opts := festivalbundle.PackOptions{
		Kind:            kind,
		Name:            name,
		Creator:         packCreator,
		Strict:          packStrict,
		WriteSentRecord: !packNoSent,
	}

	// Campaign metadata when available
	if cfg, root, err := config.LoadCampaignConfigFromCwd(ctx); err == nil && cfg != nil {
		rel := ""
		if r, err := filepath.Rel(root, source); err == nil && !strings.HasPrefix(r, "..") {
			rel = filepath.ToSlash(r)
		}
		opts.From = &festivalbundle.FromMeta{
			CampaignID:   cfg.ID,
			CampaignName: cfg.Name,
			RelativePath: rel,
		}
	}

	if sub := loadWorkitemSubject(source); sub != nil {
		opts.Subject = sub
	}

	info, err := festivalbundle.Pack(ctx, source, packOutput, opts)
	if err != nil {
		return camperrors.NewCommand(cmd.CommandPath(), 1, "pack", err)
	}

	if packJSON {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(info)
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "wrote %s\n", packOutput)
	fmt.Fprintf(cmd.OutOrStdout(), "kind=%s id=%s name=%q\n", info.Kind, info.Bundle.ID, info.Bundle.Name)
	return nil
}

func inferKind(source string) string {
	// Walk path parts for known workflow segments
	parts := strings.Split(filepath.ToSlash(source), "/")
	for i, p := range parts {
		switch p {
		case "explore":
			return festivalbundle.KindExplore
		case "design":
			return festivalbundle.KindDesign
		case "intent", "intents":
			return festivalbundle.KindIntent
		case "ritual":
			return festivalbundle.KindRitual
		case "festivals":
			if i+1 < len(parts) && parts[i+1] == "ritual" {
				return festivalbundle.KindRitual
			}
			return festivalbundle.KindFestival
		case "workflow":
			// continue
		}
	}
	if _, err := os.Stat(filepath.Join(source, ".workitem")); err == nil {
		return festivalbundle.KindWorkitem
	}
	if _, err := os.Stat(filepath.Join(source, "fest.yaml")); err == nil {
		return festivalbundle.KindFestival
	}
	return festivalbundle.KindNote
}

type workitemMarker struct {
	ID    string `yaml:"id"`
	Ref   string `yaml:"ref"`
	Type  string `yaml:"type"`
	Title string `yaml:"title"`
}

func loadWorkitemSubject(source string) *festivalbundle.SubjectMeta {
	path := filepath.Join(source, ".workitem")
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var m workitemMarker
	if err := yaml.Unmarshal(raw, &m); err != nil {
		return nil
	}
	if m.ID == "" && m.Ref == "" && m.Title == "" {
		return nil
	}
	return &festivalbundle.SubjectMeta{
		ID:    m.ID,
		Ref:   m.Ref,
		Type:  m.Type,
		Title: m.Title,
	}
}
