package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	initcmd "github.com/Obedience-Corp/camp/cmd/camp/init"
	"github.com/Obedience-Corp/camp/internal/starter"
	"github.com/spf13/cobra"
)

func init() {
	var asJSON bool
	cmd := &cobra.Command{
		Use: "setup", Short: "Create your first festival camp once",
		Long: "Create and register a camp named festival in your configured camps directory when you have no registered camps. Existing camps are preserved. Once setup succeeds, deleting the starter does not cause it to be recreated. Failed setup can be retried with the same command.",
		Args: cobra.NoArgs, GroupID: "setup",
		RunE: func(cmd *cobra.Command, _ []string) error {
			result, err := initcmd.EnsureStarter(cmd.Context(), cmd.ErrOrStderr())
			if err != nil {
				return err
			}
			if asJSON {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(result)
			}
			return renderStarterResult(cmd.OutOrStdout(), result)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Emit the setup result as JSON")
	rootCmd.AddCommand(cmd)
}

func renderStarterResult(out io.Writer, result starter.Result) error {
	var err error
	switch result.Action {
	case "created", "registered":
		_, err = fmt.Fprintf(out, "Your festival camp is ready at %s\nEnter it with: cd %s\n", result.Path, "'"+strings.ReplaceAll(result.Path, "'", "'\"'\"'")+"'")
	case "existing":
		_, err = fmt.Fprintln(out, "Your existing camps are ready; no starter was added.")
	case "complete":
		_, err = fmt.Fprintln(out, "Starter camp setup has already completed.")
	}
	if err == nil && result.Message != "" {
		_, err = fmt.Fprintln(out, result.Message)
	}
	return err
}
