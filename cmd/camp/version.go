package main

import (
	"encoding/json"
	"fmt"

	"github.com/obediencecorp/camp/internal/version"
	"github.com/spf13/cobra"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Show version information",
	Long: `Show camp version, build information, and runtime details.

Examples:
  camp version           Show full version info
  camp version --short   Show only version number
  camp version --json    Output as JSON`,
	Run: func(cmd *cobra.Command, args []string) {
		short, _ := cmd.Flags().GetBool("short")
		jsonOut, _ := cmd.Flags().GetBool("json")

		info := version.Get()

		if short {
			fmt.Println(info.Version)
			return
		}

		if jsonOut {
			out, err := json.MarshalIndent(info, "", "  ")
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "Error: %v\n", err)
				return
			}
			fmt.Println(string(out))
			return
		}

		fmt.Printf("camp %s\n", info.Version)
		fmt.Printf("commit: %s\n", info.Commit)
		fmt.Printf("built: %s\n", info.BuildDate)
		fmt.Printf("go: %s\n", info.GoVersion)
		fmt.Printf("platform: %s\n", info.Platform)
	},
}

func init() {
	versionCmd.Flags().BoolP("short", "s", false, "show only version number")
	versionCmd.Flags().Bool("json", false, "output as JSON")
	rootCmd.AddCommand(versionCmd)
}
