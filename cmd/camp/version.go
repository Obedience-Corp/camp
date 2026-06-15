package main

import (
	"encoding/json"
	"fmt"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/version"
	"github.com/spf13/cobra"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Show version information",
	Long: `Show camp version, build information, and runtime details.

When both --short and --json are provided, --json wins.

Examples:
  camp version           Show full version info
  camp version --short   Show only version number
  camp version --json    Output as JSON`,
	RunE: runVersion,
}

func runVersion(cmd *cobra.Command, args []string) error {
	short, _ := cmd.Flags().GetBool("short")
	jsonOut, _ := cmd.Flags().GetBool("json")

	info := version.Get()

	if jsonOut {
		out, err := json.MarshalIndent(info, "", "  ")
		if err != nil {
			return camperrors.Wrap(err, "marshal version info")
		}
		_, err = fmt.Fprintln(cmd.OutOrStdout(), string(out))
		return err
	}

	if short {
		_, err := fmt.Fprintln(cmd.OutOrStdout(), info.Version)
		return err
	}

	out := cmd.OutOrStdout()
	if _, err := fmt.Fprintf(out, "camp %s\n", info.Version); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "commit: %s\n", info.Commit); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "built: %s\n", info.BuildDate); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "go: %s\n", info.GoVersion); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "platform: %s\n", info.Platform); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "profile: %s\n", info.Profile); err != nil {
		return err
	}
	return nil
}

func init() {
	versionCmd.Flags().BoolP("short", "s", false, "show only version number")
	versionCmd.Flags().Bool("json", false, "output as JSON")
	rootCmd.AddCommand(versionCmd)
	versionCmd.GroupID = "system"
}
