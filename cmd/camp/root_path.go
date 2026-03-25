package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"

	"github.com/Obedience-Corp/camp/internal/campaign"
	"github.com/spf13/cobra"
)

type campaignRootOutput struct {
	RelativeRoot string `json:"relative_root"`
	CWD          string `json:"cwd"`
	AbsoluteRoot string `json:"absolute_root"`
}

var campaignRootCmd = &cobra.Command{
	Use:          "root",
	Short:        "Print the current campaign root",
	Long:         "Print the current campaign root relative to the current working directory.",
	Example:      "  camp root\n  camp root --json",
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE:         runCampaignRoot,
}

func init() {
	campaignRootCmd.Flags().Bool("json", false, "output as JSON")
	rootCmd.AddCommand(campaignRootCmd)
	campaignRootCmd.GroupID = "navigation"
}

func runCampaignRoot(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	result, err := detectCampaignRootOutput(ctx)
	if err != nil {
		return err
	}

	jsonOut, err := cmd.Flags().GetBool("json")
	if err != nil {
		return err
	}

	if jsonOut {
		return writeCampaignRootJSON(cmd.OutOrStdout(), result)
	}

	_, err = fmt.Fprintln(cmd.OutOrStdout(), result.RelativeRoot)
	return err
}

func detectCampaignRootOutput(ctx context.Context) (campaignRootOutput, error) {
	cwd, err := resolveExistingPath(".")
	if err != nil {
		return campaignRootOutput{}, err
	}

	root, err := campaign.DetectCached(ctx)
	if err != nil {
		return campaignRootOutput{}, err
	}

	root, err = resolveExistingPath(root)
	if err != nil {
		return campaignRootOutput{}, err
	}

	return buildCampaignRootOutput(cwd, root)
}

func buildCampaignRootOutput(cwd, root string) (campaignRootOutput, error) {
	rel, err := filepath.Rel(cwd, root)
	if err != nil {
		return campaignRootOutput{}, err
	}

	return campaignRootOutput{
		RelativeRoot: rel,
		CWD:          cwd,
		AbsoluteRoot: root,
	}, nil
}

func resolveExistingPath(path string) (string, error) {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}

	return filepath.Abs(resolved)
}

func writeCampaignRootJSON(w io.Writer, output campaignRootOutput) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(output)
}
