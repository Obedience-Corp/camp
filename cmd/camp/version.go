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

--check compares the Festival suite release this camp shipped in against the
latest stable release of github.com/Obedience-Corp/festival and names the
upgrade command. It reuses a result less than a day old from
~/.obey/update-check/suite.json, the cache camp and fest share; otherwise it
asks GitHub. A camp built outside a suite release has nothing to compare and
says so. --check takes precedence over --short.

Human commands at a terminal print a two-line notice on stderr, at most once
per new suite release, when this camp is behind it. Set
FESTIVAL_NO_UPDATE_CHECK=1 to turn off the notice and the check behind it.

Examples:
  camp version                  Show full version info
  camp version --short          Show only version number
  camp version --json           Output as JSON
  camp version --check          Check for a newer Festival suite release
  camp version --check --json   Output the suite check as JSON`,
	RunE: runVersion,
}

func runVersion(cmd *cobra.Command, args []string) error {
	short, _ := cmd.Flags().GetBool("short")
	jsonOut, _ := cmd.Flags().GetBool("json")
	if check, _ := cmd.Flags().GetBool("check"); check {
		return runVersionCheck(cmd, jsonOut)
	}

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
	if info.Bundle != "" {
		if _, err := fmt.Fprintf(out, "bundle: festival %s\n", info.Bundle); err != nil {
			return err
		}
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
	versionCmd.Flags().Bool("check", false, "check for a newer Festival suite release")
	rootCmd.AddCommand(versionCmd)
	versionCmd.GroupID = "system"
}
